package vault

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hal/internal/global"

	vaultapi "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
)

var (
	tpmEnable     bool
	tpmDisable    bool
	tpmUpdate     bool
	tpmDemoProxy  bool
	tpmDemoCLI    bool
	tpmProxyImage = defaultVaultTPMUbuntuImage
	tpmProxyCPUs  = defaultVaultTPMVMCPUs
	tpmProxyMem   = defaultVaultTPMVMMem
	tpmProxyApp   = defaultVaultTPMApp
)

var vaultTPMCmd = &cobra.Command{
	Use:   "tpm [status|enable|disable|update]",
	Short: "Vault Enterprise TPM auth labs",
	Long: `Stand up one Multipass VM with swtpm and enroll it with Vault Enterprise TPM auth.

  --proxy   Vault Proxy auto-auth on a guest Unix socket
  --cli     vault tpm attest, then vault login -method=tpm

Both demos share the VM and auth/tpm/. Enabling one leaves the other in place.
Disabling one removes the VM only when the other demo is gone.`,
	Args: cobra.MaximumNArgs(1),
	Run:  runVaultTPM,
}

func runVaultTPM(cmd *cobra.Command, args []string) {
	if err := parseLifecycleAction(args, &tpmEnable, &tpmDisable, &tpmUpdate); err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}
	if err := validateVaultTPMApp(tpmProxyApp); err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}

	acting := tpmEnable || tpmDisable || tpmUpdate
	if acting && !tpmDemoProxy && !tpmDemoCLI {
		fmt.Println("❌ Pass --proxy, --cli, or both.")
		return
	}

	if err := exec.Command("multipass", "version").Run(); err != nil {
		fmt.Println("❌ Error: Multipass is not installed or not running.")
		fmt.Println("   💡 Install Multipass: https://multipass.run/install")
		return
	}

	client, vaultErr := GetHealthyClient()

	if !acting {
		switch {
		case tpmDemoProxy && !tpmDemoCLI:
			printVaultTPMProxyStatus(client, vaultErr)
		case tpmDemoCLI && !tpmDemoProxy:
			printVaultTPMCLIStatus(client, vaultErr)
		default:
			printVaultTPMStatus(client, vaultErr)
		}
		return
	}

	if tpmDisable {
		if tpmDemoProxy {
			disableVaultTPMProxy(client, vaultErr, tpmDemoCLI)
		}
		if tpmDemoCLI {
			disableVaultTPMCLI(client, vaultErr, tpmDemoProxy)
		}
		return
	}

	if tpmDemoProxy {
		enableVaultTPMProxy(client, vaultErr)
	}
	if tpmDemoCLI {
		enableVaultTPMCLI(client, vaultErr)
	}
}

func printVaultTPMStatus(client *vaultapi.Client, vaultErr error) {
	fmt.Println("🔍 Checking Vault TPM labs...")
	printVaultTPMProxyStatus(client, vaultErr)
	fmt.Println()
	printVaultTPMCLIStatus(client, vaultErr)
}

func printVaultTPMProxyStatus(client *vaultapi.Client, vaultErr error) {
	fmt.Println("🔍 Checking Vault TPM proxy...")

	vmExists := exec.Command("multipass", "info", vaultTPMInstance).Run() == nil
	vmIP := ""
	if vmExists {
		ipOut, _ := exec.Command("multipass", "info", vaultTPMInstance, "--format", "csv").Output()
		vmIP = extractMultipassIP(string(ipOut))
	}

	authMounted := false
	if vaultErr == nil {
		auths, _ := client.Sys().ListAuth()
		_, authMounted = auths["tpm/"]
	}

	proxyActive := false
	if vmExists {
		out, err := exec.Command("multipass", "exec", vaultTPMInstance, "--", "systemctl", "is-active", "vault-proxy").Output()
		proxyActive = err == nil && strings.TrimSpace(string(out)) == "active"
	}

	if vmExists {
		fmt.Printf("  ✅ VM            : Active (%s, %s)\n", vaultTPMInstance, vmIP)
	} else {
		fmt.Printf("  ❌ VM            : Not running\n")
	}
	if authMounted {
		fmt.Printf("  ✅ TPM auth      : Mounted (auth/tpm/)\n")
	} else {
		fmt.Printf("  ❌ TPM auth      : Not mounted\n")
	}
	if proxyActive {
		fmt.Printf("  ✅ Vault proxy   : Active (unix://%s)\n", vaultTPMProxySocket(tpmProxyApp))
	} else {
		fmt.Printf("  ❌ Vault proxy   : Not running\n")
	}

	fmt.Println("\n💡 Next Step:")
	switch {
	case !vmExists && !authMounted:
		fmt.Println("   hal vault tpm enable --proxy")
	case vmExists && authMounted && proxyActive:
		fmt.Printf("   multipass shell %s\n", vaultTPMInstance)
		fmt.Printf("   sudo -u %s VAULT_ADDR=unix://%s vault kv get secret/tpm-demo\n", tpmProxyApp, vaultTPMProxySocket(tpmProxyApp))
		fmt.Println("   hal vault tpm disable --proxy")
	default:
		fmt.Println("   hal vault tpm update --proxy")
	}
}

func disableVaultTPMProxy(client *vaultapi.Client, vaultErr error, alsoCLI bool) {
	if global.DryRun {
		fmt.Println("[DRY RUN] Would stop vault-proxy in the TPM VM")
		if alsoCLI {
			return
		}
		if !tpmCLIStatePresent() {
			fmt.Printf("[DRY RUN] Would disable auth/tpm and delete Multipass VM %s\n", vaultTPMInstance)
		}
		return
	}
	fmt.Println("🛑 Stopping Vault TPM proxy...")
	if tpmVMExists() {
		_, _ = exec.Command("multipass", "exec", vaultTPMInstance, "--", "sudo", "systemctl", "disable", "--now", "vault-proxy").CombinedOutput()
		_, _ = exec.Command("multipass", "exec", vaultTPMInstance, "--", "sudo", "rm", "-f", "/etc/systemd/system/vault-proxy.service", vaultTPMProxyConfigPath()).CombinedOutput()
	}
	if alsoCLI {
		return
	}
	if tpmCLIStatePresent() {
		fmt.Println("✅ Proxy stopped. The CLI demo is still on this VM.")
		fmt.Println("   hal vault tpm")
		return
	}
	teardownVaultTPMLab(client, vaultErr)
}

func teardownVaultTPMLab(client *vaultapi.Client, vaultErr error) {
	if vaultErr == nil && client != nil {
		fmt.Println("⚙️  Cleaning up Vault resources...")
		_ = client.Sys().DisableAuth("tpm")
		_ = client.Sys().DeletePolicy(vaultTPMPolicyName)
		_, _ = client.Logical().Delete(vaultTPMSecretPath)
		_, _ = client.Logical().Delete("secret/metadata/tpm-demo")
		deleteEnrolledTPMs(client)
	} else {
		fmt.Println("⚠️  Vault is offline. Skipped Vault-internal cleanup.")
	}

	if tpmVMExists() {
		fmt.Println("⚙️  Removing Multipass VM...")
		_ = exec.Command("multipass", "delete", vaultTPMInstance).Run()
		_ = exec.Command("multipass", "purge").Run()
	}
	fmt.Println("✅ TPM lab destroyed.")
	refreshHalHealth()
}

func deleteEnrolledTPMs(client *vaultapi.Client) {
	secret, err := client.Logical().List("identity/tpm")
	if err != nil || secret == nil || secret.Data == nil {
		return
	}
	keys, _ := secret.Data["keys"].([]interface{})
	for _, key := range keys {
		id, ok := key.(string)
		if !ok || id == "" {
			continue
		}
		_, _ = client.Logical().Delete("identity/tpm/" + id)
	}
}

func prepareTPMLab(client *vaultapi.Client, vaultErr error) (string, string, bool) {
	if vaultErr != nil {
		fmt.Printf("❌ Cannot deploy: Vault must be running and healthy. %v\n", vaultErr)
		return "", "", false
	}
	if !vaultProdActive() {
		fmt.Println("❌ TPM auth needs the production Vault listener (HTTPS).")
		fmt.Println("   Dev mode does not serve TLS, and the client verifies Vault's certificate.")
		fmt.Println("   hal vault delete")
		fmt.Println("   export VAULT_LICENSE='...'")
		fmt.Println("   hal vault create --mode prod")
		return "", "", false
	}

	health, err := client.Sys().Health()
	if err != nil {
		fmt.Printf("❌ Cannot read Vault health: %v\n", err)
		return "", "", false
	}
	if !vaultSupportsTPMAuth(health.Version) {
		fmt.Printf("❌ TPM auth needs Vault Enterprise 2.2 or newer (running %s).\n", health.Version)
		fmt.Println("   hashicorp/vault-enterprise:2.2.0-rc1-ent is not on Docker Hub.")
		fmt.Println("   The 2.2.0-rc1+ent binaries are at https://releases.hashicorp.com/vault/2.2.0-rc1+ent/")
		return "", "", false
	}

	caHostPath := vaultProdCertPath()
	if caHostPath == "" {
		fmt.Println("❌ Could not find the prod Vault CA under ~/.hal/vault-prod/certs/.")
		return "", "", false
	}
	return strings.TrimPrefix(strings.TrimSpace(health.Version), "v"), caHostPath, true
}

func enableVaultTPMProxy(client *vaultapi.Client, vaultErr error) {
	version, caHostPath, ok := prepareTPMLab(client, vaultErr)
	if !ok {
		return
	}

	if global.DryRun {
		fmt.Printf("[DRY RUN] Would ensure Multipass VM %s with swtpm\n", vaultTPMInstance)
		fmt.Println("[DRY RUN] Would forward 127.0.0.1:8200 to the host Vault")
		fmt.Printf("[DRY RUN] Would enable auth/tpm, role %s, and secret tpm-demo\n", vaultTPMRoleName(tpmProxyApp))
		fmt.Printf("[DRY RUN] Would enroll the guest TPM and start vault proxy as %s\n", tpmProxyApp)
		return
	}

	vmIP, err := ensureTPMGuest(version, caHostPath)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}

	fmt.Println("⚙️  Configuring TPM auth, policy, and a demo secret...")
	if err := configureVaultTPMAuth(client); err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}
	if err := enrollGuestTPM(client.Token()); err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}
	if err := authorizeEnrolledTPM(client); err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}

	fmt.Println("⚙️  Installing the proxy config and starting vault proxy...")
	if err := installAndStartTPMProxy(); err != nil {
		fmt.Printf("❌ %v\n", err)
		fmt.Println("   💡 multipass exec " + vaultTPMInstance + " -- journalctl -u vault-proxy -n 40 --no-pager")
		return
	}

	fmt.Println("⏳ Waiting for the proxy to authenticate and read the secret...")
	if err := waitTPMProxyRead(); err != nil {
		fmt.Printf("❌ Proxy did not serve the secret: %v\n", err)
		out, _ := exec.Command("multipass", "exec", vaultTPMInstance, "--", "journalctl", "-u", "vault-proxy", "-n", "40", "--no-pager").CombinedOutput()
		if len(out) > 0 {
			fmt.Printf("%s\n", string(out))
		}
		return
	}

	fmt.Println("\n✅ Vault TPM proxy is ready.")
	fmt.Println("---------------------------------------------------------")
	fmt.Printf("🔗 VM           : %s (%s)\n", vaultTPMInstance, vmIP)
	fmt.Printf("🔐 Auth         : auth/tpm/  role %s\n", vaultTPMRoleName(tpmProxyApp))
	fmt.Printf("🧦 Proxy socket : unix://%s\n", vaultTPMProxySocket(tpmProxyApp))
	fmt.Println("\n📋 From the VM, the proxy supplies the token:")
	fmt.Printf("   multipass shell %s\n", vaultTPMInstance)
	fmt.Printf("   sudo -u %s VAULT_ADDR=unix://%s vault kv get secret/tpm-demo\n", tpmProxyApp, vaultTPMProxySocket(tpmProxyApp))
	fmt.Println("---------------------------------------------------------")
	refreshHalHealth()
}

func refreshHalHealth() {
	engine, err := global.DetectEngine()
	if err != nil {
		return
	}
	global.RefreshHalHealth(engine)
}

func configureVaultTPMAuth(client *vaultapi.Client) error {
	mounts, err := client.Sys().ListMounts()
	if err != nil {
		return fmt.Errorf("list mounts: %w", err)
	}
	secretMount, hasSecret := mounts["secret/"]
	switch {
	case !hasSecret:
		if err := client.Sys().Mount("secret", &vaultapi.MountInput{
			Type:    "kv",
			Options: map[string]string{"version": "2"},
		}); err != nil {
			return fmt.Errorf("enable kv-v2 at secret/: %w", err)
		}
	case secretMount.Type != "kv" && secretMount.Type != "kv-v2":
		return fmt.Errorf("secret/ is mounted as %q, want kv-v2", secretMount.Type)
	case secretMount.Options != nil && secretMount.Options["version"] != "" && secretMount.Options["version"] != "2":
		return fmt.Errorf("secret/ is kv version %s, want 2", secretMount.Options["version"])
	}

	policy := `path "secret/data/tpm-demo" {
  capabilities = ["read"]
}
`
	if err := client.Sys().PutPolicy(vaultTPMPolicyName, policy); err != nil {
		return fmt.Errorf("write policy %s: %w", vaultTPMPolicyName, err)
	}
	if _, err := client.Logical().Write(vaultTPMSecretPath, map[string]interface{}{
		"data": map[string]interface{}{"message": "tpm auth ok"},
	}); err != nil {
		return fmt.Errorf("write %s: %w", vaultTPMSecretPath, err)
	}

	auths, err := client.Sys().ListAuth()
	if err != nil {
		return fmt.Errorf("list auth methods: %w", err)
	}
	if _, ok := auths["tpm/"]; !ok {
		if err := client.Sys().EnableAuthWithOptions("tpm", &vaultapi.EnableAuthOptions{
			Type:        "tpm",
			Description: "HAL TPM auth lab",
		}); err != nil {
			return fmt.Errorf("enable auth/tpm: %w", err)
		}
	}
	// Writing config creates the backend CA. Attest refuses to run until this exists.
	if _, err := client.Logical().Write("auth/tpm/config", map[string]interface{}{
		"ca_lifetime":    "8760h",
		"ca_soft_expiry": "720h",
	}); err != nil {
		return fmt.Errorf("configure auth/tpm: %w", err)
	}
	return nil
}

func authorizeEnrolledTPM(client *vaultapi.Client) error {
	secret, err := client.Logical().Read("identity/tpm/name/" + vaultTPMInstance)
	if err != nil {
		return fmt.Errorf("read enrolled TPM: %w", err)
	}
	if secret == nil || secret.Data == nil {
		return fmt.Errorf("TPM %s is not enrolled", vaultTPMInstance)
	}
	tpmID, _ := secret.Data["id"].(string)
	if tpmID == "" {
		return fmt.Errorf("enrolled TPM %s has no id", vaultTPMInstance)
	}
	if _, err := client.Logical().Write("auth/tpm/role/"+vaultTPMRoleName(tpmProxyApp), map[string]interface{}{
		"token_policies": vaultTPMPolicyName,
		"token_ttl":      "1h",
		"tpm_ids":        tpmID,
	}); err != nil {
		return fmt.Errorf("write auth/tpm role %s: %w", vaultTPMRoleName(tpmProxyApp), err)
	}
	return nil
}

func waitMultipassIP(instance string) string {
	for i := 0; i < 30; i++ {
		time.Sleep(2 * time.Second)
		ipOut, err := exec.Command("multipass", "info", instance, "--format", "csv").Output()
		if err != nil {
			continue
		}
		ip := extractMultipassIP(string(ipOut))
		if ip != "" && ip != "127.0.0.1" {
			return ip
		}
	}
	return ""
}

func waitTPMProxyRead() error {
	readCmd := fmt.Sprintf(
		"sudo -u %s env -u VAULT_TOKEN VAULT_ADDR=unix://%s vault kv get -format=json secret/tpm-demo",
		tpmProxyApp, vaultTPMProxySocket(tpmProxyApp),
	)
	var last []byte
	for i := 0; i < 30; i++ {
		out, err := exec.Command("multipass", "exec", vaultTPMInstance, "--", "bash", "-c", readCmd).CombinedOutput()
		last = out
		if err == nil && strings.Contains(string(out), "tpm auth ok") {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out waiting for a successful read\n%s", strings.TrimSpace(string(last)))
}

func tpmProxyEnrollScript(token string) string {
	return fmt.Sprintf("VAULT_ADDR=%q\nVAULT_CACERT=%q\nVAULT_TOKEN=%q\n",
		vaultProdLocalAPIURL, vaultTPMCAPath(tpmProxyApp), token)
}

func tpmVMExists() bool {
	return exec.Command("multipass", "info", vaultTPMInstance).Run() == nil
}

func tpmProxyActive() bool {
	if !tpmVMExists() {
		return false
	}
	out, err := exec.Command("multipass", "exec", vaultTPMInstance, "--", "systemctl", "is-active", "vault-proxy").Output()
	return err == nil && strings.TrimSpace(string(out)) == "active"
}

func tpmCLIStatePresent() bool {
	if !tpmVMExists() {
		return false
	}
	marker := vaultTPMCLIStateDir(tpmProxyApp) + "/client.crt"
	return exec.Command("multipass", "exec", vaultTPMInstance, "--", "sudo", "test", "-f", marker).Run() == nil
}

func ensureTPMGuest(version, caHostPath string) (string, error) {
	launched := false
	if !tpmVMExists() {
		fmt.Println("🚀 Deploying TPM lab VM...")
		fmt.Println("📦 Provisioning Ubuntu VM (this takes a moment)...")
		launchArgs := []string{"launch", tpmProxyImage, "--name", vaultTPMInstance, "--cpus", tpmProxyCPUs, "--memory", tpmProxyMem}
		out, err := exec.Command("multipass", launchArgs...).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("launch VM: %v\n%s", err, string(out))
		}
		launched = true
	} else {
		fmt.Printf("♻️  Reusing Multipass VM %s\n", vaultTPMInstance)
	}

	vmIP := ""
	if launched {
		fmt.Println("⏳ Waiting for VM to get an IP address...")
		vmIP = waitMultipassIP(vaultTPMInstance)
	} else {
		ipOut, err := exec.Command("multipass", "info", vaultTPMInstance, "--format", "csv").Output()
		if err != nil {
			return "", fmt.Errorf("read VM info: %w", err)
		}
		vmIP = extractMultipassIP(string(ipOut))
	}
	if vmIP == "" || vmIP == "127.0.0.1" {
		return "", fmt.Errorf("VM %s has no IP address", vaultTPMInstance)
	}

	dir, err := os.MkdirTemp("", "hal-vault-tpm-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	caBytes, err := os.ReadFile(caHostPath)
	if err != nil {
		return "", fmt.Errorf("read Vault CA: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vault-ca.pem"), caBytes, 0o644); err != nil {
		return "", err
	}
	setup := tpmProxyGuestSetupScript(tpmProxyApp, version)
	if err := os.WriteFile(filepath.Join(dir, "setup.sh"), []byte(setup), 0o755); err != nil {
		return "", err
	}

	fmt.Println("⚙️  Installing swtpm and the Vault CLI in the VM...")
	for _, file := range []string{"vault-ca.pem", "setup.sh"} {
		if err := multipassTransfer(filepath.Join(dir, file), file); err != nil {
			return "", err
		}
	}
	if out, err := exec.Command("multipass", "exec", vaultTPMInstance, "--", "sudo", "bash", "/tmp/setup.sh").CombinedOutput(); err != nil {
		return "", fmt.Errorf("guest setup failed: %v\n%s", err, string(out))
	}
	fmt.Printf("✅ VM ready at %s\n", vmIP)
	return vmIP, nil
}

func multipassTransfer(local, remoteName string) error {
	remote := fmt.Sprintf("%s:/tmp/%s", vaultTPMInstance, remoteName)
	out, err := exec.Command("multipass", "transfer", local, remote).CombinedOutput()
	if err != nil {
		return fmt.Errorf("copy %s into the VM: %v\n%s", remoteName, err, string(out))
	}
	return nil
}

func enrollGuestTPM(token string) error {
	dir, err := os.MkdirTemp("", "hal-vault-tpm-enroll-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "enroll.env"), []byte(tpmProxyEnrollScript(token)), 0o600); err != nil {
		return err
	}
	if err := multipassTransfer(filepath.Join(dir, "enroll.env"), "enroll.env"); err != nil {
		return err
	}
	script := fmt.Sprintf(`set -euo pipefail
install -m 0600 /tmp/enroll.env /run/vault-enroll.env
chown %[1]s:%[1]s /run/vault-enroll.env
set +e
out=$(sudo -u %[1]s bash -c 'set -a; . /run/vault-enroll.env; set +a; vault tpm enroll -name=%[2]s -tpm-device-path=%[3]s -metadata=lab=hal' 2>&1)
status=$?
set -e
rm -f /run/vault-enroll.env /tmp/enroll.env
printf '%%s\n' "$out"
if [ "$status" -ne 0 ]; then
  echo "$out" | grep -qi 'already' && exit 0
  exit "$status"
fi
`, tpmProxyApp, vaultTPMInstance, vaultTPMSwtpmSocket)
	fmt.Println("⚙️  Enrolling the guest TPM...")
	out, err := exec.Command("multipass", "exec", vaultTPMInstance, "--", "sudo", "bash", "-c", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("TPM enroll failed: %v\n%s", err, string(out))
	}
	return nil
}

func installAndStartTPMProxy() error {
	dir, err := os.MkdirTemp("", "hal-vault-tpm-proxy-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	hcl := vaultTPMProxyConfig(tpmProxyApp, vaultProdLocalAPIURL, vaultTPMCAPath(tpmProxyApp), vaultTPMSwtpmSocket, vaultTPMInstance)
	if err := os.WriteFile(filepath.Join(dir, "tpm.hcl"), []byte(hcl), 0o644); err != nil {
		return err
	}
	if err := multipassTransfer(filepath.Join(dir, "tpm.hcl"), "tpm.hcl"); err != nil {
		return err
	}
	script := fmt.Sprintf(`set -euo pipefail
install -d -m 0755 /etc/vault-proxy
install -m 0644 /tmp/tpm.hcl %s
rm -f /tmp/tpm.hcl
cat > /etc/systemd/system/vault-proxy.service <<'UNIT'
[Unit]
Description=Vault proxy TPM auth
After=swtpm.service vault-forward.service
Requires=swtpm.service vault-forward.service

[Service]
Type=simple
User=%s
Group=%s
RuntimeDirectory=vault-proxy
RuntimeDirectoryMode=0750
LimitMEMLOCK=infinity
ExecStart=/usr/local/bin/vault proxy -config=%s
Restart=on-failure
RestartSec=2

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable --now vault-proxy
systemctl restart vault-proxy
`, vaultTPMProxyConfigPath(), tpmProxyApp, tpmProxyApp, vaultTPMProxyConfigPath())
	out, err := exec.Command("multipass", "exec", vaultTPMInstance, "--", "sudo", "bash", "-c", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("start vault proxy: %v\n%s", err, string(out))
	}
	return nil
}

func tpmProxyGuestSetupScript(app, vaultVersion string) string {
	return fmt.Sprintf(`set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y swtpm socat unzip curl ca-certificates
# Ubuntu 24.04's swtpm AppArmor profile only allows libvirt socket paths and
# does not grant file create, so binding /run/swtpm/swtpm.sock is denied.
if [ -f /etc/apparmor.d/usr.bin.swtpm ]; then
  install -d /etc/apparmor.d/disable
  ln -sfn /etc/apparmor.d/usr.bin.swtpm /etc/apparmor.d/disable/usr.bin.swtpm
  apparmor_parser -R /etc/apparmor.d/usr.bin.swtpm 2>/dev/null || true
fi
id -u %[1]s >/dev/null 2>&1 || useradd --create-home --shell /bin/bash %[1]s
install -d -o %[1]s -g %[1]s -m 0750 %[2]s %[3]s /var/lib/vault-cli/%[1]s/tpm /etc/vault-proxy
chown -R %[1]s:%[1]s /var/lib/vault-agent /var/lib/vault-cli
install -m 0644 /tmp/vault-ca.pem %[4]s
chown %[1]s:%[1]s %[4]s

arch=$(dpkg --print-architecture)
ver=%[5]q
if ! /usr/local/bin/vault version 2>/dev/null | grep -q "$ver"; then
  url="https://releases.hashicorp.com/vault/${ver}/vault_${ver}_linux_${arch}.zip"
  curl -fsSL -o /tmp/vault.zip "$url"
  unzip -o /tmp/vault.zip -d /usr/local/bin
  chmod 0755 /usr/local/bin/vault
  rm -f /tmp/vault.zip
fi

gw=$(ip route show default | awk '{print $3; exit}')
if ! timeout 5 bash -c "echo >/dev/tcp/${gw}/8200"; then
  echo "The VM cannot reach Vault at ${gw}:8200." >&2
  echo "Prod Vault must be published on the host port 8200." >&2
  exit 1
fi

cat > /etc/systemd/system/swtpm.service <<'UNIT'
[Unit]
Description=swtpm for HAL Vault TPM auth

[Service]
Type=simple
User=%[1]s
Group=%[1]s
RuntimeDirectory=swtpm
StateDirectory=swtpm
ExecStart=/usr/bin/swtpm socket --tpm2 --tpmstate dir=/var/lib/swtpm --server type=unixio,path=%[6]s --ctrl type=unixio,path=/run/swtpm/swtpm-ctrl.sock --flags not-need-init,startup-clear
Restart=on-failure

[Install]
WantedBy=multi-user.target
UNIT

cat > /etc/systemd/system/vault-forward.service <<EOF
[Unit]
Description=Forward guest localhost:8200 to the host Vault
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/bin/socat TCP-LISTEN:8200,bind=127.0.0.1,reuseaddr,fork TCP:${gw}:8200
Restart=on-failure

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now swtpm vault-forward

for _ in $(seq 1 30); do
  if [ -S %[6]s ] && timeout 2 bash -c 'echo >/dev/tcp/127.0.0.1/8200'; then
    rm -f /tmp/setup.sh
    exit 0
  fi
  sleep 1
done
echo "swtpm socket or the Vault forwarder did not come up" >&2
systemctl status swtpm vault-forward --no-pager || true
exit 1
`,
		app,
		vaultTPMTLSDir(app),
		vaultTPMCertDir(app),
		vaultTPMCAPath(app),
		vaultVersion,
		vaultTPMSwtpmSocket,
	)
}

func init() {
	vaultTPMCmd.Flags().BoolVarP(&tpmEnable, "enable", "e", false, "Launch the selected TPM demo")
	vaultTPMCmd.Flags().BoolVarP(&tpmDisable, "disable", "d", false, "Remove the selected TPM demo")
	vaultTPMCmd.Flags().BoolVarP(&tpmUpdate, "update", "u", false, "Reconcile the selected TPM demo")
	_ = vaultTPMCmd.Flags().MarkHidden("enable")
	_ = vaultTPMCmd.Flags().MarkHidden("disable")
	_ = vaultTPMCmd.Flags().MarkHidden("update")

	vaultTPMCmd.Flags().BoolVar(&tpmDemoProxy, "proxy", false, "Vault Proxy auto-auth on the guest Unix socket")
	vaultTPMCmd.Flags().BoolVar(&tpmDemoCLI, "cli", false, "Attest with the Vault CLI and log in with -method=tpm")

	vaultTPMCmd.Flags().StringVar(&tpmProxyImage, "ubuntu-image", defaultVaultTPMUbuntuImage, "Ubuntu image for the Multipass VM")
	vaultTPMCmd.Flags().StringVar(&tpmProxyCPUs, "vm-cpus", defaultVaultTPMVMCPUs, "Number of CPUs for the VM")
	vaultTPMCmd.Flags().StringVar(&tpmProxyMem, "vm-mem", defaultVaultTPMVMMem, "Amount of RAM for the VM")
	vaultTPMCmd.Flags().StringVar(&tpmProxyApp, "app", defaultVaultTPMApp, "Linux user that owns the proxy socket and runs vault login")

	Cmd.AddCommand(vaultTPMCmd)
}
