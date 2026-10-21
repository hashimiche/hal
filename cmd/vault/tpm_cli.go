package vault

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"hal/internal/global"

	vaultapi "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
)

var (
	tpmCLIEnable  bool
	tpmCLIDisable bool
	tpmCLIUpdate  bool
)

var vaultTPMCLICmd = &cobra.Command{
	Use:   "cli [status|enable|disable|update]",
	Short: "Log in with the Vault CLI TPM auth method inside a Multipass VM",
	Args:  cobra.MaximumNArgs(1),
	Run:   runVaultTPMCLI,
}

func runVaultTPMCLI(cmd *cobra.Command, args []string) {
	if err := parseLifecycleAction(args, &tpmCLIEnable, &tpmCLIDisable, &tpmCLIUpdate); err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}
	if err := validateVaultTPMApp(tpmProxyApp); err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}

	if err := exec.Command("multipass", "version").Run(); err != nil {
		fmt.Println("❌ Error: Multipass is not installed or not running.")
		fmt.Println("   💡 Install Multipass: https://multipass.run/install")
		return
	}

	client, vaultErr := GetHealthyClient()

	if !tpmCLIEnable && !tpmCLIDisable && !tpmCLIUpdate {
		printVaultTPMCLIStatus(client, vaultErr)
		return
	}

	if tpmCLIDisable {
		disableVaultTPMCLI(client, vaultErr)
		return
	}

	if tpmCLIEnable || tpmCLIUpdate {
		enableVaultTPMCLI(client, vaultErr)
	}
}

func printVaultTPMCLIStatus(client *vaultapi.Client, vaultErr error) {
	fmt.Println("🔍 Checking Vault TPM CLI...")

	vmExists := tpmVMExists()
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

	cliReady := tpmCLIStatePresent()

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
	if cliReady {
		fmt.Printf("  ✅ CLI state     : %s\n", vaultTPMCLIStateDir(tpmProxyApp))
	} else {
		fmt.Printf("  ❌ CLI state     : Not attested\n")
	}

	fmt.Println("\n💡 Next Step:")
	switch {
	case !vmExists && !authMounted:
		fmt.Println("   hal vault tpm cli enable")
	case cliReady:
		fmt.Printf("   multipass shell %s\n", vaultTPMInstance)
		fmt.Println("   " + vaultTPMCLILoginCommand())
		fmt.Println("   " + vaultTPMCLIReadCommand())
		fmt.Println("   hal vault tpm cli disable")
	default:
		fmt.Println("   hal vault tpm cli update")
	}
}

func disableVaultTPMCLI(client *vaultapi.Client, vaultErr error) {
	if global.DryRun {
		fmt.Printf("[DRY RUN] Would remove %s\n", vaultTPMCLIStateDir(tpmProxyApp))
		if !tpmProxyActive() {
			fmt.Printf("[DRY RUN] Would disable auth/tpm and delete Multipass VM %s\n", vaultTPMInstance)
		}
		return
	}
	fmt.Println("🛑 Removing the TPM CLI login state...")
	if tpmVMExists() {
		_, _ = exec.Command("multipass", "exec", vaultTPMInstance, "--", "sudo", "rm", "-rf", vaultTPMCLIStateDir(tpmProxyApp)).CombinedOutput()
	}
	if tpmProxyActive() {
		fmt.Println("✅ CLI state removed. The proxy demo is still on this VM.")
		fmt.Println("   hal vault tpm proxy")
		return
	}
	teardownVaultTPMLab(client, vaultErr)
}

func enableVaultTPMCLI(client *vaultapi.Client, vaultErr error) {
	version, caHostPath, ok := prepareTPMLab(client, vaultErr)
	if !ok {
		return
	}

	if global.DryRun {
		fmt.Printf("[DRY RUN] Would ensure Multipass VM %s with swtpm\n", vaultTPMInstance)
		fmt.Printf("[DRY RUN] Would enable auth/tpm, role %s, and secret tpm-demo\n", vaultTPMRoleName(tpmProxyApp))
		fmt.Printf("[DRY RUN] Would attest into %s and vault login -method=tpm\n", vaultTPMCLIStateDir(tpmProxyApp))
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

	fmt.Println("⚙️  Attesting and logging in with the Vault CLI...")
	if err := attestAndLoginTPMCLI(client.Token()); err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}

	state := vaultTPMCLIStateDir(tpmProxyApp)
	role := vaultTPMRoleName(tpmProxyApp)
	fmt.Println("\n✅ Vault TPM CLI login is ready.")
	fmt.Println("---------------------------------------------------------")
	fmt.Printf("🔗 VM        : %s (%s)\n", vaultTPMInstance, vmIP)
	fmt.Printf("🔐 Auth      : auth/tpm/  role %s\n", role)
	fmt.Printf("📁 State dir : %s\n", state)
	fmt.Println("\n📋 From the VM:")
	fmt.Printf("   multipass shell %s\n", vaultTPMInstance)
	fmt.Println("   " + vaultTPMCLILoginCommand())
	fmt.Println("   " + vaultTPMCLIReadCommand())
	fmt.Println("---------------------------------------------------------")
	refreshHalHealth()
}

func vaultTPMCLIEnvPrefix() string {
	return fmt.Sprintf("sudo -u %s env -u VAULT_TOKEN VAULT_ADDR=%s VAULT_CACERT=%s",
		tpmProxyApp, vaultProdLocalAPIURL, vaultTPMCAPath(tpmProxyApp))
}

func vaultTPMCLILoginCommand() string {
	return fmt.Sprintf("%s vault login -method=tpm -tpm-state-dir=%s -tpm-device-path=%s role_name=%s",
		vaultTPMCLIEnvPrefix(), vaultTPMCLIStateDir(tpmProxyApp), vaultTPMSwtpmSocket, vaultTPMRoleName(tpmProxyApp))
}

func vaultTPMCLIReadCommand() string {
	return vaultTPMCLIEnvPrefix() + " vault kv get secret/tpm-demo"
}

func attestAndLoginTPMCLI(token string) error {
	dir, err := os.MkdirTemp("", "hal-vault-tpm-cli-*")
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

	state := vaultTPMCLIStateDir(tpmProxyApp)
	role := vaultTPMRoleName(tpmProxyApp)
	script := fmt.Sprintf(`set -euo pipefail
install -m 0600 /tmp/enroll.env /run/vault-enroll.env
chown %[1]s:%[1]s /run/vault-enroll.env
rm -rf %[2]s
install -d -o %[1]s -g %[1]s -m 0700 %[2]s
sudo -u %[1]s bash -c 'set -a; . /run/vault-enroll.env; set +a; vault tpm attest -role-name=%[3]s -tpm-device-path=%[4]s -tpm-state-dir=%[2]s -cert-subject-CN=%[5]s'
rm -f /run/vault-enroll.env /tmp/enroll.env
sudo -u %[1]s env -u VAULT_TOKEN VAULT_ADDR=%[6]s VAULT_CACERT=%[7]s \
  vault login -method=tpm -tpm-state-dir=%[2]s -tpm-device-path=%[4]s role_name=%[3]s
sudo -u %[1]s env -u VAULT_TOKEN VAULT_ADDR=%[6]s VAULT_CACERT=%[7]s \
  vault kv get -format=json secret/tpm-demo
`, tpmProxyApp, state, role, vaultTPMSwtpmSocket, vaultTPMInstance, vaultProdLocalAPIURL, vaultTPMCAPath(tpmProxyApp))

	out, err := exec.Command("multipass", "exec", vaultTPMInstance, "--", "sudo", "bash", "-c", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("TPM attest or login failed: %v\n%s", err, string(out))
	}
	if !strings.Contains(string(out), "tpm auth ok") {
		return fmt.Errorf("login succeeded but the demo secret was not readable\n%s", strings.TrimSpace(string(out)))
	}
	return nil
}

func init() {
	vaultTPMCLICmd.Flags().BoolVarP(&tpmCLIEnable, "enable", "e", false, "Attest the guest TPM and log in with the Vault CLI")
	vaultTPMCLICmd.Flags().BoolVarP(&tpmCLIDisable, "disable", "d", false, "Remove CLI attest state. Removes the VM when the proxy is absent")
	vaultTPMCLICmd.Flags().BoolVarP(&tpmCLIUpdate, "update", "u", false, "Re-attest and log in again on the existing VM")
	_ = vaultTPMCLICmd.Flags().MarkHidden("enable")
	_ = vaultTPMCLICmd.Flags().MarkHidden("disable")
	_ = vaultTPMCLICmd.Flags().MarkHidden("update")

	vaultTPMCLICmd.Flags().StringVar(&tpmProxyImage, "ubuntu-image", defaultVaultTPMUbuntuImage, "Ubuntu image for the Multipass VM")
	vaultTPMCLICmd.Flags().StringVar(&tpmProxyCPUs, "vm-cpus", defaultVaultTPMVMCPUs, "Number of CPUs for the VM")
	vaultTPMCLICmd.Flags().StringVar(&tpmProxyMem, "vm-mem", defaultVaultTPMVMMem, "Amount of RAM for the VM")
	vaultTPMCLICmd.Flags().StringVar(&tpmProxyApp, "app", defaultVaultTPMApp, "Linux user that runs vault login")

	vaultTPMCmd.AddCommand(vaultTPMCLICmd)
}
