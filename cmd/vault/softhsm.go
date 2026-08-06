package vault

// softhsm.go implements the SoftHSM2-backed Vault managed-key PKI runtime and
// CA setup. The runtime image is selected and built by `hal vault create
// --edition ent-hsm --mode prod`; `hal vault pki enable` detects that running
// HSM build and configures its managed-key-backed CA chain automatically.
//
// Flow:
//   1. Create builds hal-vault-softhsm:latest and boots it with kms_library.
//   2. PKI enable runs `softhsm2-util --init-token` and parses the
//      reassigned slot number from stdout.
//   3. Register a Vault managed key at sys/managed-keys/pkcs11/<name> using
//      the detected slot, library path, PIN, and key labels.
//   4. Generate the Root CA and Intermediate CA using the /kms generate path
//      so Vault delegates all private-key operations to the PKCS#11 token.
//
// Teardown (pki disable / update, via teardownSoftHSMLayer) reverses the
// PKI-owned state: the managed key is deleted after the PKI unmounts and token
// data is wiped unless an HSM-backed update immediately follows. The runtime
// container and image remain create/delete-owned.
//
// The SoftHSM token data directory is stored in the hal-vault-data volume at
// /vault/data/softhsm/tokens so it survives a container restart.  Ownership
// is fixed to vault (UID 100) when initSoftHSMToken creates the directory.

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"

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
	pkiNoHSM          bool
	softHSMBaseImage  string
	softHSMBaseTag    string
	softHSMLabel      string
	softHSMPin        string
	softHSMSOPin      string
	softHSMManagedKey string
	softHSMKeyLabel   string
	softHSMHMACLabel  string
)

// isHSMTag reports whether a tag string is an HSM-enabled Vault Enterprise tag.
// HashiCorp publishes HSM builds exclusively with the "-ent.hsm" suffix, e.g.
// "2.0.3-ent.hsm".  A plain "-ent" tag does not include the PKCS#11 subsystem.
func isHSMTag(tag string) bool {
	return strings.HasSuffix(tag, ".hsm")
}

// isVaultHSMBuild reports whether the running server is an Enterprise HSM
// build. Vault exposes versions such as "2.0.3+ent.hsm" via sys/health.
func isVaultHSMBuild(client *vault.Client) bool {
	health, err := client.Sys().Health()
	return err == nil && health != nil && strings.Contains(health.Version, "ent.hsm")
}

// buildSoftHSMImage builds hal-vault-softhsm:latest from a generated
// multi-stage Dockerfile. srcRef supplies the HSM-enabled Vault binary and
// baseRef supplies the glibc SoftHSM2 runtime.
func buildSoftHSMImage(engine, srcRef, baseRef string) error {
	arch := runtime.GOARCH
	var platform string
	switch arch {
	case "arm64":
		platform = "linux/arm64"
	default:
		platform = "linux/amd64"
	}

	imageRef := vaultSoftHSMRuntimeImage + ":" + vaultSoftHSMRuntimeTag
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
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

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

// vaultOnSoftHSMImage reports whether the hal-vault container is currently
// running the locally built SoftHSM runtime image.
func vaultOnSoftHSMImage(engine string) bool {
	out, _ := exec.Command(engine, "inspect", vaultContainer,
		"--format", "{{.Config.Image}}").Output()
	return strings.Contains(strings.TrimSpace(string(out)), vaultSoftHSMRuntimeImage)
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
	// The HSM runtime must be selected during create. Dev-mode HSM binaries and
	// old stock ent.hsm prod deployments do not include the SoftHSM utilities or
	// the boot-time kms_library configuration required below.
	if !vaultProdActive() {
		fmt.Println("❌ HSM-backed PKI requires Vault to be running in production mode.")
		fmt.Println("   Deploy with: hal vault create --edition ent-hsm --mode prod")
		return
	}
	if !vaultOnSoftHSMImage(engine) {
		fmt.Println("❌ Vault is an HSM build, but the running container does not include the HAL SoftHSM2 runtime.")
		fmt.Println("   Redeploy with: hal vault create --edition ent-hsm --mode prod --update")
		fmt.Println("   Or force software-backed CAs with: hal vault pki enable --no-hsm")
		return
	}

	// Confirm the licensed Enterprise APIs are reachable before token work.
	licResp, licErr := client.Logical().Read("sys/license/status")
	if licErr != nil || licResp == nil {
		fmt.Println("❌ HSM-backed PKI requires a reachable Vault Enterprise license endpoint.")
		fmt.Println("   Check the license, or redeploy with: hal vault create --edition ent-hsm --mode prod --update")
		return
	}

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
		_ = client.Sys().TuneMountAllowNil(pkiRootMount, vault.TuneMountConfigInput{MaxLeaseTTL: &pkiRootTTL})
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
		_ = client.Sys().TuneMountAllowNil(pkiIntMount, vault.TuneMountConfigInput{MaxLeaseTTL: &pkiIntTTL})
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

// teardownSoftHSMLayer reverses everything runVaultPKIHSMSetup added on top of
// the base prod deployment. Called from the pki disable/update teardown path
// AFTER the PKI mounts are unmounted — Vault refuses to delete a managed key
// while a mount still references it.
//
// keepToken is set when an HSM rebuild follows immediately: the managed key is
// deleted so it can be registered cleanly, while the token data is reused.
// Everything here is best-effort — a partial failure warns and continues so a
// broken HSM layer can never make disable un-runnable.
func teardownSoftHSMLayer(client *vault.Client, engine string, keepToken bool) {
	keyActive := client != nil && hsmManagedKeyActive(client)
	tokenPresent := exec.Command(engine, "exec", vaultContainer, "test", "-d", "/vault/data/softhsm").Run() == nil

	if !keyActive && !tokenPresent {
		return // no HSM layer deployed — nothing to do
	}

	fmt.Println("⚙️  Removing SoftHSM managed-key layer...")

	if keyActive {
		if _, err := client.Logical().Delete("sys/managed-keys/pkcs11/" + softHSMManagedKey); err == nil {
			fmt.Printf("  ✅ Managed key '%s' deleted.\n", softHSMManagedKey)
		} else {
			fmt.Printf("  ⚠️  Could not delete managed key '%s': %v\n", softHSMManagedKey, err)
		}
	}

	if keepToken {
		fmt.Println("  ℹ️  SoftHSM token kept for the upcoming HSM-backed PKI rebuild.")
		return
	}

	if tokenPresent {
		if out, err := exec.Command(engine, "exec", vaultContainer,
			"rm", "-rf", "/vault/data/softhsm").CombinedOutput(); err == nil {
			fmt.Println("  ✅ SoftHSM token data removed from volume.")
		} else {
			fmt.Printf("  ⚠️  Could not remove SoftHSM token data: %v (%s)\n", err, strings.TrimSpace(string(out)))
		}
	}
}
