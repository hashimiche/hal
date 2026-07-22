package vault

// softhsm.go implements the SoftHSM2-backed Vault managed-key PKI path,
// enabled via `hal vault pki enable --hsm`.
//
// Flow:
//   1. Build a custom hal-vault-softhsm:latest image (debian:12-slim + SoftHSM2
//      + Vault binary extracted from the official image) — skipped when the
//      image is already present unless --update was passed.
//   2. Restart the running hal-vault container using the new image.
//   3. Inside the container, run `softhsm2-util --init-token` and parse the
//      reassigned slot number from stdout.
//   4. Register a Vault managed key at sys/managed-keys/pkcs11/<name> using
//      the detected slot, library path, PIN, and key labels.
//   5. Generate the Root CA and Intermediate CA using the /kms generate path
//      so Vault delegates all private-key operations to the PKCS#11 token.
//
// The SoftHSM token data directory is stored in the hal-vault-data volume at
// /vault/softhsm/tokens so it survives a container restart.  The tokens
// directory ownership is fixed to vault (UID 100) by the helper container
// before the main container starts.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"hal/internal/global"

	vault "github.com/hashicorp/vault/api"
)

// softHSMLibPath is the path to libsofthsm2.so inside the custom image.
// On debian:12-slim the softhsm2 package installs the library here.
const softHSMLibPath = "/usr/lib/softhsm/libsofthsm2.so"

// softHSMTokensDir is the in-container path for SoftHSM token storage.
// It lives under the persistent hal-vault-data Raft volume (/vault/data) so
// tokens survive container recreation — if it were only in the image layer it
// would be lost every time the container is replaced.
const softHSMTokensDir = "/vault/data/softhsm/tokens"

// softHSMConf is the in-container path for the softhsm2 config file.
// Also placed under the Raft volume so it persists alongside the token store.
const softHSMConf = "/vault/data/softhsm/softhsm2.conf"

// slotRe matches the slot number printed by softhsm2-util --init-token:
//
//	"The token has been initialized and is reassigned to slot 1874907601"
var slotRe = regexp.MustCompile(`reassigned to slot\s+(\d+)`)

// showSlotsRe matches a slot number + label line pair from softhsm2-util --show-slots.
// The output looks like:
//
//	Slot 1783835404
//	    Slot info:
//	    ...
//	    Token info:
//	        Label:          hal-hsm-token
//
// We scan for a "Slot <number>" line and then confirm the label appears nearby.
var showSlotsSlotRe = regexp.MustCompile(`(?m)^Slot\s+(\d+)`)
var showSlotsLabelRe = regexp.MustCompile(`Label:\s+(\S+)`)

// --- flag variables (bound in pki.go init) ---

var (
	pkiHSM            bool
	softHSMBaseImage  string
	softHSMBaseTag    string
	softHSMVaultTag   string // tag for the hashicorp/vault-enterprise source image (must be *-ent.hsm)
	softHSMLabel      string
	softHSMPin        string
	softHSMSOPin      string
	softHSMManagedKey string
	softHSMKeyLabel   string
	softHSMHMACLabel  string
)

// hsmImageRef returns the fully-qualified source image reference for the
// HSM-enabled Vault binary.  It always uses hashicorp/vault-enterprise with
// the HSM-specific tag so the Dockerfile COPY pulls a PKCS#11-capable binary.
func hsmImageRef() string {
	return defaultVaultImageEnt + ":" + softHSMVaultTag
}

// isHSMTag reports whether a tag string is an HSM-enabled Vault Enterprise tag.
// HashiCorp publishes HSM builds exclusively with the "-ent.hsm" suffix, e.g.
// "2.0.3-ent.hsm".  A plain "-ent" tag does not include the PKCS#11 subsystem.
func isHSMTag(tag string) bool {
	return strings.HasSuffix(tag, ".hsm")
}

// buildSoftHSMImage builds the hal-vault-softhsm:latest image from a
// dynamically generated multi-stage Dockerfile.  The Vault binary is always
// pulled from the HSM-enabled Enterprise image (softHSMVaultTag, e.g.
// "2.0.3-ent.hsm") so the resulting container is guaranteed to include PKCS#11
// support; SoftHSM2 is installed from the Debian package repository.
func buildSoftHSMImage(engine string) error {
	arch := runtime.GOARCH
	var platform string
	switch arch {
	case "arm64":
		platform = "linux/arm64"
	default:
		platform = "linux/amd64"
	}

	imageRef := vaultSoftHSMRuntimeImage + ":" + vaultSoftHSMRuntimeTag
	baseRef := softHSMBaseImage + ":" + softHSMBaseTag
	srcRef := hsmImageRef()

	dockerfile := fmt.Sprintf(`FROM --platform=%s %s AS vault-src

FROM --platform=%s %s

RUN apt-get update && apt-get install -y --no-install-recommends \
    softhsm2 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=vault-src /bin/vault /bin/vault

RUN mkdir -p /vault/logs /vault/plugins /vault/file /vault/softhsm/tokens && \
    useradd -r -u 100 -g 0 vault 2>/dev/null || true && \
    chown -R 100:0 /vault /var/lib/softhsm

ENV VAULT_PLUGIN_DIR=/vault/plugins
ENV SOFTHSM2_CONF=%s

EXPOSE 8200

ENTRYPOINT ["/bin/vault"]
`, platform, srcRef, platform, baseRef, softHSMConf)

	tmpDir, err := os.MkdirTemp("", "hal-softhsm-build-*")
	if err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(tmpDir+"/Dockerfile", []byte(dockerfile), 0644); err != nil {
		return fmt.Errorf("write Dockerfile: %w", err)
	}

	buildCmd := exec.Command(engine, "build", "--platform", platform,
		"-t", imageRef, "-f", tmpDir+"/Dockerfile", tmpDir)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	return buildCmd.Run()
}

// vaultHSMProdConfigHCL renders the prod Raft server config with the
// kms_library "pkcs11" stanza required for Vault managed keys.
// The block tells the HSM-enabled Vault binary where to find libsofthsm2.so
// so that sys/managed-keys writes succeed at runtime.
func vaultHSMProdConfigHCL() string {
	base := vaultProdConfigHCL()
	hsmBlock := fmt.Sprintf(`
kms_library "pkcs11" {
  name    = "softhsm"
  library = %q
}
`, softHSMLibPath)
	return base + hsmBlock
}

// restartVaultWithSoftHSM stops the running hal-vault container and starts a
// fresh one using the hal-vault-softhsm image in production mode (server
// -config).  Dev mode (server -dev) does not load a config file at all, so
// the kms_library block would be silently ignored — prod mode is mandatory
// for managed-key PKI.
//
// The function reuses the TLS certs and state already present in
// ~/.hal/vault-prod/ (written by the original hal vault create --mode prod
// run), and overwrites vault.hcl with the HSM-augmented config.
func restartVaultWithSoftHSM(engine string) error {
	stateDir := global.VaultProdStateDir()
	if stateDir == "" {
		return fmt.Errorf("could not resolve ~/.hal/vault-prod state directory")
	}
	certDir := filepath.Join(stateDir, vaultProdCertsDirName)
	configPath := filepath.Join(stateDir, vaultProdConfigFileName)

	isPodman := strings.Contains(engine, "podman")

	// Overwrite vault.hcl with the HSM-augmented config.
	if err := os.WriteFile(configPath, []byte(vaultHSMProdConfigHCL()), 0o644); err != nil {
		return fmt.Errorf("write HSM vault.hcl: %w", err)
	}

	// Load the cached unseal keys produced by the original `hal vault create
	// --mode prod` run. We need them to unseal after the container restarts —
	// prod Vault always boots sealed.
	initData, err := global.LoadCachedVaultInit()
	if err != nil {
		return fmt.Errorf("could not load unseal keys from %s: %w\n   Run 'hal vault create --mode prod' first", global.VaultInitCachePath(), err)
	}

	license := extractVaultLicense(engine)
	_ = exec.Command(engine, "rm", "-f", vaultContainer).Run()

	runtimeRef := vaultSoftHSMRuntimeImage + ":" + vaultSoftHSMRuntimeTag
	mountOpts := "ro"
	if isPodman {
		mountOpts = "ro,Z"
	}

	vaultArgs := []string{
		"run", "-d",
		"--name", vaultContainer,
		"--network", global.HalNetName,
		"--cap-add", "IPC_LOCK",
		"-p", fmt.Sprintf("%d:%d", vaultHTTPPort, vaultHTTPPort),
		"-v", vaultDataVolume + ":" + vaultProdRaftMount,
		"-v", fmt.Sprintf("%s:%s:%s", configPath, vaultProdConfigMount, mountOpts),
		"-v", fmt.Sprintf("%s:%s:%s", certDir, vaultProdTLSMount, mountOpts),
		"-e", "SKIP_SETCAP=true",
		"-e", fmt.Sprintf("SOFTHSM2_CONF=%s", softHSMConf),
	}

	if license != "" {
		vaultArgs = append(vaultArgs, "-e", "VAULT_LICENSE="+license)
	}

	vaultArgs = append(vaultArgs,
		runtimeRef,
		"server", "-config="+vaultProdConfigMount,
	)

	out, err := exec.Command(engine, vaultArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}

	// Wait for the server process to start listening (sealed is fine at this point).
	if err := waitForVaultResponding(vaultProdHealthURL, 30); err != nil {
		return fmt.Errorf("vault did not start listening after restart: %w", err)
	}

	// Unseal using the cached keys from the original init.
	fmt.Println("🔓 Unsealing Vault (using cached unseal key)...")
	if err := vaultOperatorUnseal(engine, initData.UnsealKeysB64); err != nil {
		return fmt.Errorf("unseal failed: %w", err)
	}

	return nil
}

// initSoftHSMToken creates the token directory and softhsm2.conf inside the
// running container, then initialises the SoftHSM2 token if it does not already
// exist, returning the slot number in both cases.
func initSoftHSMToken(engine string) (string, error) {
	// 1. Ensure the token directory and softhsm2.conf exist.
	mkdirOut, err := exec.Command(engine, "exec", vaultContainer,
		"sh", "-c",
		fmt.Sprintf("mkdir -p %s && chown -R vault:root %s", softHSMTokensDir, softHSMTokensDir),
	).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("mkdir softhsm tokens dir: %v (%s)", err, strings.TrimSpace(string(mkdirOut)))
	}

	conf := fmt.Sprintf("directories.tokendir = %s\nobjectstore.backend = file\nlog.level = ERROR\n", softHSMTokensDir)
	confDir := strings.TrimSuffix(softHSMConf, "/softhsm2.conf")
	writeConf := fmt.Sprintf("mkdir -p %s && printf '%%s' '%s' > %s", confDir, conf, softHSMConf)
	confOut, err := exec.Command(engine, "exec", vaultContainer, "sh", "-c", writeConf).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("write softhsm2.conf: %v (%s)", err, strings.TrimSpace(string(confOut)))
	}

	// 2. Check whether the token already exists from a previous run.
	//    softhsm2-util --show-slots lists all slots including initialised tokens.
	//    If our label is already present we extract the slot and skip --init-token.
	if slot, found := findExistingSoftHSMSlot(engine); found {
		fmt.Printf("  ⚡ SoftHSM token '%s' already exists on slot %s — reusing.\n", softHSMLabel, slot)
		return slot, nil
	}

	// 3. Initialise a fresh token on the uninitialized placeholder slot (slot 0).
	initArgs := []string{
		"exec",
		"-e", fmt.Sprintf("SOFTHSM2_CONF=%s", softHSMConf),
		vaultContainer,
		"softhsm2-util",
		"--init-token",
		"--slot", "0",
		"--label", softHSMLabel,
		"--pin", softHSMPin,
		"--so-pin", softHSMSOPin,
	}
	out, err := exec.Command(engine, initArgs...).CombinedOutput()
	output := string(out)
	if err != nil {
		return "", fmt.Errorf("softhsm2-util --init-token failed: %v\n%s", err, strings.TrimSpace(output))
	}

	// 4. Parse the reassigned slot number from the init output.
	//    softhsm2-util prints: "The token has been initialized and is reassigned to slot 1874907601"
	m := slotRe.FindStringSubmatch(output)
	if len(m) < 2 {
		return "", fmt.Errorf("could not parse slot from softhsm2-util output:\n%s", strings.TrimSpace(output))
	}
	slot := m[1]
	fmt.Printf("  ✅ SoftHSM token '%s' initialised on slot %s.\n", softHSMLabel, slot)
	return slot, nil
}

// findExistingSoftHSMSlot runs softhsm2-util --show-slots inside the container
// and returns the slot number for the token whose label matches softHSMLabel.
// Returns ("", false) when no matching token is found.
func findExistingSoftHSMSlot(engine string) (string, bool) {
	out, err := exec.Command(engine, "exec",
		"-e", fmt.Sprintf("SOFTHSM2_CONF=%s", softHSMConf),
		vaultContainer,
		"softhsm2-util", "--show-slots",
	).Output()
	if err != nil {
		return "", false
	}

	// Walk the output block-by-block: each "Slot <N>" line starts a new block.
	// We track the most recently seen slot number and look for our label within
	// the same block.
	output := string(out)
	slotMatches := showSlotsSlotRe.FindAllStringSubmatchIndex(output, -1)
	for i, sm := range slotMatches {
		// The block for this slot runs from the current match to the start of the
		// next slot block (or end of output).
		blockStart := sm[0]
		blockEnd := len(output)
		if i+1 < len(slotMatches) {
			blockEnd = slotMatches[i+1][0]
		}
		block := output[blockStart:blockEnd]
		slotNum := output[sm[2]:sm[3]]

		labelMatches := showSlotsLabelRe.FindStringSubmatch(block)
		if len(labelMatches) >= 2 && strings.TrimSpace(labelMatches[1]) == softHSMLabel {
			return slotNum, true
		}
	}
	return "", false
}

// configureSoftHSMManagedKey registers the PKCS#11 managed key in Vault and
// returns (managedKeyName, error). Vault Enterprise is required for
// sys/managed-keys; an early gate check is performed before this is called.
func configureSoftHSMManagedKey(client *vault.Client, slot string) error {
	// `library` must be the `name` value from the kms_library HCL block:
	//   kms_library "pkcs11" { name = "softhsm" ... }  →  library = "softhsm"
	// NOT the .so path and NOT the block label — the "pkcs11" provider type is
	// carried by the API path (sys/managed-keys/pkcs11/<name>), it is not a
	// request parameter.
	// `mechanism` and `key_bits` are both mandatory here: with
	// allow_generate_key=true Vault defers key creation until the first
	// /root/generate/kms call, and without a mechanism that deferred
	// generation fails with "cannot generate a key for mechanism 0".
	// CKM_RSA_PKCS + 4096 mirrors the RSA-4096 chain built by the software
	// (non-HSM) PKI path.
	_, err := client.Logical().Write(
		"sys/managed-keys/pkcs11/"+softHSMManagedKey,
		map[string]interface{}{
			"library":            "softhsm",
			"slot":               slot,
			"pin":                softHSMPin,
			"key_label":          softHSMKeyLabel,
			"hmac_key_label":     softHSMHMACLabel,
			"mechanism":          softHSMMechanismRSAPKCS,
			"key_bits":           softHSMKeyBits,
			"allow_generate_key": "true",
			"allow_store_key":    "true",
		},
	)
	if err != nil {
		return fmt.Errorf("sys/managed-keys/pkcs11/%s: %w", softHSMManagedKey, err)
	}
	fmt.Printf("  ✅ Vault managed key '%s' registered (PKCS#11 slot %s).\n", softHSMManagedKey, slot)
	return nil
}

// runVaultPKIHSMSetup is the --hsm analogue of runVaultPKISetup. It generates
// the Root CA and Intermediate CA using the /kms path so all key operations are
// delegated to the SoftHSM PKCS#11 token.
func runVaultPKIHSMSetup(client *vault.Client, engine string, isUpdate bool) {
	// ---- Prod-mode gate ----
	// server -dev does not load a config file, so the kms_library block would
	// be silently ignored and every sys/managed-keys write would fail with
	// "Vault is not built with HSM support".  Prod mode is required.
	if !vaultProdActive() {
		fmt.Println("❌ --hsm requires Vault to be running in production mode.")
		fmt.Printf("   Deploy with: hal vault create --mode prod --vault-tag %s\n", defaultVaultEntHSMTag)
		fmt.Println("   (prod mode writes a vault.hcl config that includes the kms_library block)")
		return
	}

	// ---- Tag validation ----
	// Vault PKCS#11 support is compiled in only for the "-ent.hsm" image variant.
	// Catch a misconfigured --vault-hsm-tag early with a clear, actionable error
	// before we spend time building the image or restarting Vault.
	if !isHSMTag(softHSMVaultTag) {
		fmt.Printf("❌ --vault-hsm-tag %q does not look like an HSM-enabled tag.\n", softHSMVaultTag)
		fmt.Println("   HashiCorp publishes PKCS#11-capable builds with the \"-ent.hsm\" suffix.")
		fmt.Printf("   Example: --vault-hsm-tag %s\n", defaultVaultEntHSMTag)
		return
	}

	// ---- Enterprise + PKCS#11 gate ----
	// sys/license/status is Enterprise-only; a 200 confirms the binary is
	// Enterprise.  But a plain "-ent" binary accepts the call yet still rejects
	// sys/managed-keys writes with "Vault is not built with HSM support".
	// The tag check above is the primary guard; this API call is a belt-and-
	// suspenders reachability check that also surfaces a clear message when
	// Vault is not yet deployed.
	licResp, licErr := client.Logical().Read("sys/license/status")
	if licErr != nil || licResp == nil {
		fmt.Println("❌ --hsm requires Vault Enterprise (sys/managed-keys is an Enterprise-only API).")
		fmt.Printf("   Deploy with: hal vault create --mode prod --vault-tag %s\n", defaultVaultEntHSMTag)
		return
	}

	runtimeRef := vaultSoftHSMRuntimeImage + ":" + vaultSoftHSMRuntimeTag
	runtimeBuilt := exec.Command(engine, "image", "inspect", runtimeRef).Run() == nil

	if pkiUpdate || !runtimeBuilt {
		fmt.Printf("🔨 Building SoftHSM runtime image %s (source: %s)...\n", runtimeRef, hsmImageRef())
		if err := buildSoftHSMImage(engine); err != nil {
			fmt.Printf("❌ Failed to build SoftHSM image: %v\n", err)
			return
		}
		fmt.Printf("  ✅ Runtime image %s built.\n", runtimeRef)
	} else {
		fmt.Printf("⚡ Runtime image %s already present — skipping build.\n", runtimeRef)
	}

	// Restart Vault only when the container is not already running the SoftHSM image.
	currentImageOut, _ := exec.Command(engine, "inspect", vaultContainer,
		"--format", "{{.Config.Image}}").Output()
	if !strings.Contains(strings.TrimSpace(string(currentImageOut)), vaultSoftHSMRuntimeImage) {
		fmt.Println("♻️  Restarting Vault with SoftHSM runtime image...")
		if err := restartVaultWithSoftHSM(engine); err != nil {
			fmt.Printf("❌ Failed to restart Vault: %v\n", err)
			return
		}
		// Reconnect using the prod HTTPS endpoint + cached root token.
		// restartVaultWithSoftHSM already waited for the server to respond and
		// applied the unseal keys, so Vault should be active by this point.
		fmt.Println("⏳ Waiting for Vault to become active...")
		if err := waitForVaultHealthy(vaultProdHealthURL, 30); err != nil {
			fmt.Println("❌ Vault did not become active after unseal.")
			fmt.Printf("   Unseal key is in: %s\n", global.VaultInitCachePath())
			return
		}
		newClient, vaultErr := GetHealthyClient()
		if vaultErr != nil {
			fmt.Printf("❌ Vault unhealthy after restart: %v\n", vaultErr)
			return
		}
		client = newClient
		fmt.Println("  ✅ Vault restarted, unsealed, and ready.")
	} else {
		fmt.Println("⚡ Vault already running with SoftHSM runtime image.")
	}

	// Brief pause to let the Vault process finish its startup sequence.
	time.Sleep(2 * time.Second)

	// Initialise the SoftHSM token and capture the slot number.
	fmt.Printf("🔑 Initialising SoftHSM token '%s'...\n", softHSMLabel)
	slot, err := initSoftHSMToken(engine)
	if err != nil {
		fmt.Printf("❌ SoftHSM token init failed: %v\n", err)
		return
	}

	// Register the managed key in Vault.
	fmt.Printf("⚙️  Registering Vault managed key '%s' (slot %s)...\n", softHSMManagedKey, slot)
	if err := configureSoftHSMManagedKey(client, slot); err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}

	// ---- Root CA (kms) ----
	fmt.Printf("🔐 Enabling PKI engine at '%s' (max TTL: %s)...\n", pkiRootMount, pkiRootTTL)
	if isUpdate {
		_ = client.Sys().Unmount(pkiRootMount)
		_ = client.Sys().Unmount(pkiIntMount)
	}
	if err := client.Sys().Mount(pkiRootMount, &vault.MountInput{
		Type:   "pki",
		Config: vault.MountConfigInput{MaxLeaseTTL: pkiRootTTL},
	}); err != nil {
		_ = client.Sys().TuneMount(pkiRootMount, vault.MountConfigInput{MaxLeaseTTL: pkiRootTTL})
	}

	// Allow the PKI mount to use the managed key.
	_, _ = client.Logical().Write("sys/mounts/"+pkiRootMount+"/tune", map[string]interface{}{
		"allowed_managed_keys": []string{softHSMManagedKey},
	})

	fmt.Println("📜 Generating Root CA (HSM-backed key via PKCS#11)...")
	rootResp, err := client.Logical().Write(pkiRootMount+"/root/generate/kms", map[string]interface{}{
		"common_name":      "HAL Root CA (HSM)",
		"ttl":              pkiRootTTL,
		"managed_key_name": softHSMManagedKey,
	})
	if err != nil || rootResp == nil {
		fmt.Printf("❌ Failed to generate Root CA: %v\n", err)
		return
	}
	fmt.Println("  ✅ Root CA generated (key in SoftHSM).")
	_, _ = client.Logical().Write(pkiRootMount+"/config/urls", map[string]interface{}{
		"issuing_certificates":    "http://vault.localhost:8200/v1/" + pkiRootMount + "/ca",
		"crl_distribution_points": "http://vault.localhost:8200/v1/" + pkiRootMount + "/crl",
	})

	// ---- Intermediate CA (kms) ----
	fmt.Printf("🔐 Enabling PKI engine at '%s' (max TTL: %s)...\n", pkiIntMount, pkiIntTTL)
	if err := client.Sys().Mount(pkiIntMount, &vault.MountInput{
		Type:   "pki",
		Config: vault.MountConfigInput{MaxLeaseTTL: pkiIntTTL},
	}); err != nil {
		_ = client.Sys().TuneMount(pkiIntMount, vault.MountConfigInput{MaxLeaseTTL: pkiIntTTL})
	}

	_, _ = client.Logical().Write("sys/mounts/"+pkiIntMount+"/tune", map[string]interface{}{
		"allowed_managed_keys": []string{softHSMManagedKey},
	})
	if err := tunePKIACMEHeaders(client, pkiIntMount); err != nil {
		fmt.Printf("  ⚠️  Could not tune ACME headers on '%s': %v\n", pkiIntMount, err)
	}

	fmt.Println("📝 Generating Intermediate CA CSR (HSM-backed key)...")
	csrResp, err := client.Logical().Write(pkiIntMount+"/intermediate/generate/kms", map[string]interface{}{
		"common_name":      "HAL Intermediate CA (HSM)",
		"managed_key_name": softHSMManagedKey,
	})
	if err != nil || csrResp == nil {
		fmt.Printf("❌ Failed to generate Intermediate CSR: %v\n", err)
		return
	}
	csr, _ := csrResp.Data["csr"].(string)
	fmt.Println("  ✅ CSR generated.")

	fmt.Println("✍️  Signing Intermediate CA with Root CA...")
	signResp, err := client.Logical().Write(pkiRootMount+"/root/sign-intermediate", map[string]interface{}{
		"csr":    csr,
		"format": "pem_bundle",
		"ttl":    pkiIntTTL,
	})
	if err != nil || signResp == nil {
		fmt.Printf("❌ Failed to sign Intermediate CA: %v\n", err)
		return
	}
	signedCert, _ := signResp.Data["certificate"].(string)
	fmt.Println("  ✅ Intermediate CA signed.")

	if _, err := client.Logical().Write(pkiIntMount+"/intermediate/set-signed", map[string]interface{}{
		"certificate": signedCert,
	}); err != nil {
		fmt.Printf("❌ Failed to install signed certificate: %v\n", err)
		return
	}
	fmt.Println("  ✅ Intermediate CA certificate installed.")
	_, _ = client.Logical().Write(pkiIntMount+"/config/urls", map[string]interface{}{
		"issuing_certificates":    "http://vault.localhost:8200/v1/" + pkiIntMount + "/ca",
		"crl_distribution_points": "http://vault.localhost:8200/v1/" + pkiIntMount + "/crl",
	})

	// ---- Role ----
	fmt.Printf("⚙️  Creating role 'hal-role' on '%s'...\n", pkiIntMount)
	_, _ = client.Logical().Write(pkiIntMount+"/roles/hal-role", map[string]interface{}{
		"allowed_domains":     pkiAllowedDomains,
		"allow_subdomains":    true,
		"allow_bare_domains":  false,
		"allow_ip_sans":       true,
		"use_csr_common_name": true,
		"max_ttl":             pkiMaxCertTTL,
		"key_type":            "rsa",
		"key_bits":            2048,
	})
	fmt.Println("  ✅ Role 'hal-role' created.")

	// ---- Policy ----
	pkiPolicy := fmt.Sprintf(`
path "%s/sign/hal-role"  { capabilities = ["create", "update"] }
path "%s/issue/hal-role" { capabilities = ["create", "update"] }
`, pkiIntMount, pkiIntMount)
	_ = client.Sys().PutPolicy("hal-pki-issuer", pkiPolicy)
	fmt.Println("  ✅ Policy 'hal-pki-issuer' written.")

	fmt.Println("\n✅ Vault PKI (HSM-backed) setup complete!")
	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  Key storage: SoftHSM2 PKCS#11 token (keys never leave the HSM)")
	fmt.Printf("  Managed key : sys/managed-keys/pkcs11/%s\n", softHSMManagedKey)
	fmt.Printf("  PKCS#11 lib : %s\n", softHSMLibPath)
	fmt.Printf("  Token label : %s  (slot %s)\n", softHSMLabel, slot)
	fmt.Println("\n  Engine Layout:")
	fmt.Printf("    Root CA  : %s  (max TTL %s)\n", pkiRootMount, pkiRootTTL)
	fmt.Printf("    Int  CA  : %s  (max TTL %s)\n", pkiIntMount, pkiIntTTL)
	fmt.Printf("    Role     : %s/roles/hal-role\n", pkiIntMount)
	fmt.Println("\n  Issue a certificate:")
	fmt.Printf("    vault write %s/issue/hal-role \\\n", pkiIntMount)
	fmt.Println(`      common_name="test.hal.local" \`)
	fmt.Println(`      ttl="24h"`)
	fmt.Println("\n  Inspect the managed key:")
	fmt.Printf("    vault read sys/managed-keys/pkcs11/%s\n", softHSMManagedKey)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("\n💡 Next Step:")
	fmt.Println("   vault write " + pkiIntMount + "/issue/hal-role common_name=\"test.hal.local\" ttl=\"24h\"")
}

// hsmManagedKeyActive returns true when the Vault managed key is present,
// used by the status output to show whether the HSM layer is configured.
func hsmManagedKeyActive(client *vault.Client) bool {
	resp, err := client.Logical().Read("sys/managed-keys/pkcs11/" + softHSMManagedKey)
	return err == nil && resp != nil
}
