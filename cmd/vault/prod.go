package vault

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hal/internal/global"
	"hal/internal/ui"
)

// prod.go implements the single-node, production-mode Vault Enterprise boot
// path (`hal vault create --mode prod`). Unlike dev mode (`server -dev`,
// plaintext, in-memory, throwaway root token), prod mode runs a real
// `vault server -config` with integrated Raft storage on a persistent volume,
// TLS on localhost via a HAL-forged self-signed cert, and automatic
// `operator init` + `unseal`. The generated unseal key + root token are saved
// the standard HAL way (internal/global.CacheVaultInit) so they can always be
// retrieved via `hal vault status` and `hal creds status`.

// runVaultProd stands up the production Vault Enterprise instance. imageRef is
// the fully-qualified "<repo>:<tag>" already resolved by the create command
// (edition is forced to Enterprise on the prod path). The VAULT_LICENSE env var
// has already been validated and set by the caller.
func runVaultProd(engine, imageRef, displayVersion string) {
	stateDir := global.VaultProdStateDir()
	if stateDir == "" {
		fmt.Println("❌ Error: could not resolve home directory for ~/.hal/vault-prod")
		return
	}
	certDir := filepath.Join(stateDir, vaultProdCertsDirName)
	configPath := filepath.Join(stateDir, vaultProdConfigFileName)
	hostCertPath := filepath.Join(certDir, vaultProdCertFileName)

	isPodman := strings.Contains(engine, "podman")

	// --update reconciles in place: drop the container + Raft volume + the cached
	// init material so we re-initialize cleanly (the old root token/unseal key
	// become invalid once the Raft volume is wiped).
	if vaultUpdate {
		if global.Debug {
			fmt.Println("[DEBUG] --update detected. Replacing prod Vault runtime + cached init material...")
		}
		_ = exec.Command(engine, "rm", "-f", vaultContainer).Run()
		_ = exec.Command(engine, "volume", "rm", "-f", vaultDataVolume).Run()
		_ = global.RemoveCachedVaultInit()
	}

	if global.DryRun {
		fmt.Printf("[DRY RUN] Would deploy Vault Enterprise %s (prod, single-node Raft) via %s\n", displayVersion, engine)
		fmt.Printf("[DRY RUN]   image        : %s\n", imageRef)
		fmt.Printf("[DRY RUN]   container    : %s  (port %d, TLS)\n", vaultContainer, vaultHTTPPort)
		fmt.Printf("[DRY RUN]   raft volume  : %s -> %s\n", vaultDataVolume, vaultProdRaftMount)
		fmt.Printf("[DRY RUN]   config       : %s -> %s\n", configPath, vaultProdConfigMount)
		fmt.Printf("[DRY RUN]   TLS certs    : %s -> %s\n", certDir, vaultProdTLSMount)
		fmt.Printf("[DRY RUN]   init         : -key-shares=%d -key-threshold=%d, node_id=%s\n", vaultKeyShares, vaultKeyThreshold, vaultProdNodeID)
		fmt.Printf("[DRY RUN]   init cached  : %s (mode 0600)\n", global.VaultInitCachePath())
		return
	}

	ui.LogoStart("vault")
	defer ui.LogoStop()
	ui.LogoStep("Deploying Vault Enterprise %s (prod, single-node Raft) via %s", displayVersion, engine)

	// 1. Ensure the global HAL network exists.
	global.EnsureNetwork(engine)

	// 2. Forge the self-signed TLS material for https://vault.localhost:8200.
	ui.LogoStep("Forging local TLS certificates")
	if err := ensureVaultProdCerts(certDir); err != nil {
		ui.LogoStop()
		fmt.Printf("❌ Failed to generate TLS certificates: %v\n", err)
		return
	}

	// 3. Write the Raft server config that the container will boot from.
	ui.LogoStep("Writing Raft server configuration")
	if err := os.WriteFile(configPath, []byte(vaultProdConfigHCL()), 0o644); err != nil {
		ui.LogoStop()
		fmt.Printf("❌ Failed to write Vault config: %v\n", err)
		return
	}

	// 4. Fix Raft data volume ownership for the in-container vault user (UID 100).
	ui.LogoStep("Preparing persistent Raft volume")
	helperRef := vaultHelperImage + ":" + vaultHelperTag
	_ = exec.Command(engine, "run", "--rm", "-v", vaultDataVolume+":/vault/data", helperRef,
		"sh", "-c", "mkdir -p /vault/data && chown -R 100:1000 /vault/data").Run()

	// 5. Build the container run arguments.
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
	}

	// Vault 2.x tries to set the SETFCAP capability which fails on Docker Desktop.
	if strings.HasPrefix(displayVersion, "2.") {
		vaultArgs = append(vaultArgs, "-e", "SKIP_SETCAP=true")
	}

	// Inject the Enterprise license (validated by the caller); Vault autoloads it.
	vaultArgs = append(vaultArgs, "-e", fmt.Sprintf("VAULT_LICENSE=%s", os.Getenv("VAULT_LICENSE")))

	// Tether to the global Consul brain if requested.
	if vaultJoinConsul {
		ui.LogoStep("Tethering Vault to the global HAL Consul")
		vaultArgs = append(vaultArgs, "-e", "CONSUL_HTTP_ADDR=http://hal-consul:8500")
	}

	vaultArgs = append(vaultArgs,
		imageRef,
		"server", "-config="+vaultProdConfigMount,
	)

	// 6. Start the container.
	ui.LogoStep("Starting Vault container")
	out, err := exec.Command(engine, vaultArgs...).CombinedOutput()
	if err != nil {
		ui.LogoStop()
		if strings.Contains(string(out), "AlreadyExists") || strings.Contains(string(out), "already in use") {
			fmt.Println("⚠️  Vault already exists. Use '--update' to reconcile it.")
			return
		}
		fmt.Printf("❌ Failed to start Vault: %s\n", string(out))
		return
	}

	// 7. Wait for the server to answer at all (uninitialized/sealed both count).
	ui.LogoStep("Waiting for Vault server to come online")
	if err := waitForVaultResponding(vaultProdHealthURL, 30); err != nil {
		ui.LogoStop()
		handleDockerFailure(vaultContainer, engine)
		return
	}

	// 8. Initialize (1/1 by default) and capture the raw JSON for caching.
	ui.LogoStep("Initializing Vault (operator init)")
	initJSON, initData, err := vaultOperatorInit(engine)
	if err != nil {
		ui.LogoStop()
		fmt.Printf("❌ Failed to initialize Vault: %v\n", err)
		handleDockerFailure(vaultContainer, engine)
		return
	}
	if err := global.CacheVaultInit(initJSON); err != nil {
		// Non-fatal: the instance is up, but warn loudly since this is the only copy.
		fmt.Printf("⚠️  Could not save init material to %s: %v\n", global.VaultInitCachePath(), err)
	}

	// 9. Unseal with the threshold number of keys.
	ui.LogoStep("Unsealing Vault")
	if err := vaultOperatorUnseal(engine, initData.UnsealKeysB64); err != nil {
		ui.LogoStop()
		fmt.Printf("❌ Failed to unseal Vault: %v\n", err)
		handleDockerFailure(vaultContainer, engine)
		return
	}

	// 10. Confirm it reaches a healthy (unsealed, active) state.
	ui.LogoStep("Waiting for Vault to become active")
	if err := waitForVaultHealthy(vaultProdHealthURL, 30); err != nil {
		ui.LogoStop()
		handleDockerFailure(vaultContainer, engine)
		return
	}

	ui.LogoStop()
	global.RefreshHalHealth(engine)

	unsealKey := ""
	if len(initData.UnsealKeysB64) > 0 {
		unsealKey = initData.UnsealKeysB64[0]
	}

	ui.Success("Vault Enterprise %s is UP! (prod, single-node Raft)", displayVersion)
	ui.Section("Connection")
	ui.Field("Edition", "ENTERPRISE")
	ui.Field("Mode", "prod (integrated Raft, persistent)")
	ui.Field("UI", vaultProdPublicURL)
	ui.Field("Root token", initData.RootToken)
	ui.Field("Unseal key", unsealKey)
	if vaultJoinConsul {
		ui.Item("🟢 Tethered to the global Consul Control Plane")
	}

	ui.Section("Credentials saved")
	ui.Item("%s  (mode 0600)", global.VaultInitCachePath())
	ui.Item("⚠️  This file is the ONLY copy of your unseal key + root token — back it up.")
	ui.Item("Retrieve later with: hal vault status  |  hal creds status")

	ui.Section("Use your local CLI")
	ui.Item("export VAULT_ADDR='%s'", vaultProdPublicURL)
	ui.Item("export VAULT_TOKEN='%s'", initData.RootToken)
	ui.Item("export VAULT_CACERT='%s'", hostCertPath)
	ui.Item("⚠️  Self-signed cert — accept the browser warning or trust %s.", vaultProdCertFileName)
}

// vaultProdConfigHCL renders the single-node Raft server config mounted into the
// container at vaultProdConfigMount.
func vaultProdConfigHCL() string {
	return fmt.Sprintf(`storage "raft" {
  path    = %q
  node_id = %q
}

listener "tcp" {
  address         = "0.0.0.0:%d"
  cluster_address = "0.0.0.0:%d"
  tls_cert_file   = %q
  tls_key_file    = %q
}

api_addr      = %q
cluster_addr  = "https://%s:%d"
disable_mlock = true
ui            = true
`,
		vaultProdRaftMount,
		vaultProdNodeID,
		vaultHTTPPort,
		vaultProdClusterPort,
		vaultProdTLSMount+"/"+vaultProdCertFileName,
		vaultProdTLSMount+"/"+vaultProdKeyFileName,
		vaultProdPublicURL,
		vaultContainer, vaultProdClusterPort,
	)
}

// vaultOperatorInit runs `vault operator init -format=json` inside the container
// and returns the raw JSON (for caching verbatim) plus the parsed keys/token.
func vaultOperatorInit(engine string) ([]byte, global.VaultInit, error) {
	var parsed global.VaultInit
	cmd := exec.Command(engine, "exec",
		"-e", "VAULT_ADDR="+vaultProdLocalAPIURL,
		"-e", "VAULT_SKIP_VERIFY=true",
		vaultContainer,
		"vault", "operator", "init",
		fmt.Sprintf("-key-shares=%d", vaultKeyShares),
		fmt.Sprintf("-key-threshold=%d", vaultKeyThreshold),
		"-format=json",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, parsed, fmt.Errorf("operator init failed: %v (%s)", err, extractStderr(err))
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, parsed, fmt.Errorf("could not parse init output: %v", err)
	}
	if parsed.RootToken == "" || len(parsed.UnsealKeysB64) == 0 {
		return nil, parsed, fmt.Errorf("init output missing root token or unseal keys")
	}
	return out, parsed, nil
}

// vaultOperatorUnseal applies the configured threshold of unseal keys.
func vaultOperatorUnseal(engine string, keys []string) error {
	need := vaultKeyThreshold
	if need > len(keys) {
		need = len(keys)
	}
	for i := 0; i < need; i++ {
		cmd := exec.Command(engine, "exec",
			"-e", "VAULT_ADDR="+vaultProdLocalAPIURL,
			"-e", "VAULT_SKIP_VERIFY=true",
			vaultContainer,
			"vault", "operator", "unseal", keys[i],
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("unseal with key %d failed: %v (%s)", i+1, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// extractStderr surfaces the stderr captured by exec.Cmd.Output() on failure.
func extractStderr(err error) string {
	if ee, ok := err.(*exec.ExitError); ok {
		return strings.TrimSpace(string(ee.Stderr))
	}
	return ""
}

// prodHTTPClient returns an HTTP client that trusts the self-signed prod cert.
func prodHTTPClient() http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	return http.Client{Timeout: 2 * time.Second, Transport: tr}
}

// waitForVaultResponding blocks until Vault answers the health endpoint with ANY
// status (200 active, 429 standby, 501 uninitialized, 503 sealed) — i.e. the
// server process is listening — or the retry budget is exhausted.
func waitForVaultResponding(url string, maxRetries int) error {
	client := prodHTTPClient()
	for i := 0; i < maxRetries; i++ {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout waiting for Vault to start listening at %s", url)
}

// waitForVaultHealthy blocks until Vault reports a 200 (initialized + unsealed +
// active) or the retry budget is exhausted.
func waitForVaultHealthy(url string, maxRetries int) error {
	client := prodHTTPClient()
	for i := 0; i < maxRetries; i++ {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout waiting for Vault to become healthy at %s", url)
}

// ensureVaultProdCerts forges (or reuses) a self-signed cert/key pair valid for
// vault.localhost, localhost, the container name, and 127.0.0.1. It mirrors the
// TFE ensureCerts/shouldRotate pattern in cmd/terraform/create.go.
func ensureVaultProdCerts(certDir string) error {
	certPath := filepath.Join(certDir, vaultProdCertFileName)
	keyPath := filepath.Join(certDir, vaultProdKeyFileName)

	if _, err := os.Stat(certPath); err == nil {
		if !shouldRotateVaultProdCert(certPath) {
			return nil
		}
		_ = os.Remove(certPath)
		_ = os.Remove(keyPath)
	}

	if err := os.MkdirAll(certDir, 0o755); err != nil {
		return err
	}

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return err
	}
	if serialNumber.Sign() == 0 {
		serialNumber = big.NewInt(time.Now().UnixNano())
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"HAL Vault Enterprise Local Dev Environment"},
			CommonName:   "vault.localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost", vaultContainer, "vault.localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	certOut, err := os.Create(certPath)
	if err != nil {
		return err
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return err
	}

	keyOut, err := os.Create(keyPath)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	return pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
}

// shouldRotateVaultProdCert returns true if the on-disk cert is missing, invalid,
// or does not cover vault.localhost.
func shouldRotateVaultProdCert(certPath string) bool {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return true
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return true
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return true
	}
	for _, name := range cert.DNSNames {
		if name == "vault.localhost" {
			return false
		}
	}
	return true
}
