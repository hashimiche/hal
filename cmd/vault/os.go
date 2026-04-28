package vault

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"hal/internal/global"

	vault "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
)

var (
	osEnable      bool
	osDisable     bool
	osUpdate      bool
	osUbuntuImage string
	osVMCPUs      string
	osVMMem       string
)

var vaultOSCmd = &cobra.Command{
	Use:   "os [status|enable|disable|update]",
	Short: "Deploy Ubuntu VM and configure Vault OS secret engine for Linux user management",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := parseLifecycleAction(args, &osEnable, &osDisable, &osUpdate); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		if err := exec.Command("multipass", "version").Run(); err != nil {
			fmt.Println("❌ Error: Multipass is not installed or not running.")
			fmt.Println("   💡 Install Multipass: https://multipass.run/install")
			return
		}

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		client, vaultErr := GetHealthyClient()

		// ==========================================
		// 1. SMART STATUS MODE (Default behavior)
		// ==========================================
		if !osEnable && !osDisable && !osUpdate {
			fmt.Println("🔍 Checking Vault OS Secret Engine Status...")

			vmExists := exec.Command("multipass", "info", "hal-vault-os").Run() == nil
			vmIP := ""
			if vmExists {
				ipOut, _ := exec.Command("multipass", "info", "hal-vault-os", "--format", "csv").Output()
				vmIP = extractMultipassIP(string(ipOut))
			}

			osMounted := false
			if vaultErr == nil {
				mounts, _ := client.Sys().ListMounts()
				_, osMounted = mounts["os/"]
			}

			if vmExists {
				fmt.Printf("  ✅ VM Instance   : Active (hal-vault-os, %s)\n", vmIP)
			} else {
				fmt.Printf("  ❌ VM Instance   : Not running\n")
			}

			if osMounted {
				fmt.Printf("  ✅ Vault Secrets : Configured (os/)\n")
			} else {
				fmt.Printf("  ❌ Vault Secrets : Not configured\n")
			}

			fmt.Println("\n💡 Next Step:")
			if !vmExists && !osMounted {
				fmt.Println("   To deploy Ubuntu VM and wire up Vault OS secret engine, run:")
				fmt.Println("   hal vault os enable")
			} else if vmExists && osMounted {
				fmt.Println("   Demo is ready! Rotate a password or read credentials:")
				fmt.Println("   vault write -f os/hosts/demo-vm/accounts/demouser/rotate")
				fmt.Println("   vault read os/hosts/demo-vm/accounts/demouser/creds")
				fmt.Println("\n   To completely remove this demo environment, run:")
				fmt.Println("   hal vault os disable")
			} else {
				fmt.Println("   Environment is partially degraded. To safely reset, run:")
				fmt.Println("   hal vault os update")
			}
			return
		}

		// ==========================================
		// 2. TEARDOWN / RESET PATH (--disable / --update)
		// ==========================================
		if osDisable || osUpdate {
			if global.DryRun {
				fmt.Println("[DRY RUN] Would execute: multipass delete hal-vault-os && multipass purge")
				fmt.Println("[DRY RUN] Would call API to revoke leases and unmount 'os/'")
			} else {
				if osDisable {
					fmt.Println("🛑 Tearing down OS secret engine environment...")
				} else {
					fmt.Println("♻️  Update requested. Destroying OS environment for reset...")
				}

				// Vault cleanup MUST happen before killing the VM
				if vaultErr == nil && client != nil {
					fmt.Println("⚙️  Cleaning up Vault resources...")
					_ = client.Sys().RevokePrefix("os/")
					_ = client.Sys().Unmount("os")
				} else {
					fmt.Println("⚠️  Vault is offline. Skipped Vault-internal cleanup.")
				}

				fmt.Println("⚙️  Removing Multipass VM...")
				_ = exec.Command("multipass", "delete", "hal-vault-os").Run()
				_ = exec.Command("multipass", "purge").Run()

				if osDisable {
					fmt.Println("✅ OS secret engine environment destroyed successfully!")
					global.RefreshHalStatus(engine)
				}
			}

			if osDisable && !global.DryRun {
				return
			}
		}

		// ==========================================
		// 3. DEPLOY / ENABLE PATH (--enable / --update)
		// ==========================================
		if osEnable || osUpdate {
			if vaultErr != nil {
				fmt.Printf("❌ Cannot deploy: Vault must be running and healthy. %v\n", vaultErr)
				return
			}

			health, err := client.Sys().Health()
			if err == nil {
				version := health.Version
				if !strings.Contains(version, "ent") && !strings.Contains(version, "+") {
					fmt.Println("❌ Error: The OS secret engine requires Vault Enterprise.")
					fmt.Println("   💡 Your current Vault version is Community Edition.")
					fmt.Println("\n   To deploy Vault Enterprise, run:")
					fmt.Println("   hal vault delete")
					fmt.Println("   export VAULT_LICENSE='your_license_string'")
					fmt.Println("   # or: export VAULT_LICENSE_PATH='/path/to/vault.hclic'")
					fmt.Println("   hal vault create --edition ent")
					return
				}
			}

			if global.DryRun {
				fmt.Println("[DRY RUN] Would launch Multipass VM 'hal-vault-os'")
				fmt.Println("[DRY RUN] Would create mgmt-user, demouser, appadmin on VM")
				fmt.Println("[DRY RUN] Would configure Vault OS secrets engine with parent-managed rotation")
				return
			}

			fmt.Println("🚀 Deploying Ubuntu VM for OS Secret Engine Demo...")

			// 1. Launch the VM
			fmt.Println("📦 Provisioning Ubuntu VM (this takes a moment)...")
			launchArgs := []string{"launch", osUbuntuImage, "--name", "hal-vault-os", "--cpus", osVMCPUs, "--memory", osVMMem}
			out, err := exec.Command("multipass", launchArgs...).CombinedOutput()
			if err != nil {
				if strings.Contains(string(out), "already exists") {
					fmt.Println("⚠️  VM 'hal-vault-os' already exists. Use 'hal vault os update' to reconcile.")
					return
				}
				fmt.Printf("❌ Failed to launch VM: %v\nOutput: %s\n", err, string(out))
				return
			}

			// 2. Wait for VM IP
			fmt.Println("⏳ Waiting for VM to get an IP address...")
			var vmIP string
			for i := 0; i < 30; i++ {
				time.Sleep(2 * time.Second)
				ipOut, err := exec.Command("multipass", "info", "hal-vault-os", "--format", "csv").Output()
				if err != nil {
					continue
				}
				vmIP = extractMultipassIP(string(ipOut))
				if vmIP != "127.0.0.1" && vmIP != "" {
					break
				}
			}
			if vmIP == "127.0.0.1" || vmIP == "" {
				fmt.Println("❌ Failed to get VM IP address")
				return
			}
			fmt.Printf("✅ VM launched at %s\n", vmIP)

			// 3. Configure VM: create users, set permissions, enable SSH password auth
			//
			// Uses parent-managed rotation: mgmt-user has sudo access to chpasswd only,
			// and performs password rotations for demouser and appadmin on Vault's behalf.
			fmt.Println("⚙️  Configuring VM users and SSH access...")
			setupScript := `
				# Create management account used by Vault to rotate other users
				sudo useradd -m -s /bin/bash mgmt-user 2>/dev/null || true
				echo 'mgmt-user:mgmt-password-789' | sudo chpasswd

				# Create target accounts whose passwords Vault will manage
				sudo useradd -m -s /bin/bash demouser 2>/dev/null || true
				sudo useradd -m -s /bin/bash appadmin 2>/dev/null || true
				echo 'demouser:initial-password-123' | sudo chpasswd
				echo 'appadmin:admin-password-456' | sudo chpasswd

				# Grant mgmt-user passwordless sudo for chpasswd only (required by Vault OS plugin)
				echo 'mgmt-user ALL=NOPASSWD:/usr/sbin/chpasswd' | sudo tee /etc/sudoers.d/vault-mgmt > /dev/null
				sudo chmod 0440 /etc/sudoers.d/vault-mgmt

				# Enable SSH password authentication.
				# Ubuntu cloud images ship 60-cloudimg-settings.conf with PasswordAuthentication no.
				# OpenSSH uses first-match-wins across included files (alphabetical order), so we
				# overwrite that file directly rather than trying to win with a higher-numbered file.
				echo 'PasswordAuthentication yes' | sudo tee /etc/ssh/sshd_config.d/60-cloudimg-settings.conf > /dev/null
				sudo systemctl restart ssh
			`
			execArgs := []string{"exec", "hal-vault-os", "--", "bash", "-c", setupScript}
			if out, err := exec.Command("multipass", execArgs...).CombinedOutput(); err != nil {
				fmt.Printf("❌ Failed to configure VM: %v\nOutput: %s\n", err, string(out))
				return
			}
			fmt.Println("   ✅ VM users and SSH configuration applied")

			// 4. Verify the Vault container can reach the VM over SSH before proceeding
			fmt.Printf("⚙️  Verifying network connectivity from Vault container to VM (%s:22)...\n", vmIP)
			connCheck := exec.Command(engine, "exec", "hal-vault", "sh", "-c",
				fmt.Sprintf("nc -z -w5 %s 22 && echo reachable || echo unreachable", vmIP))
			connOut, _ := connCheck.CombinedOutput()
			if strings.TrimSpace(string(connOut)) != "reachable" {
				fmt.Printf("❌ The Vault container cannot reach %s:22\n", vmIP)
				fmt.Println("   The OS plugin will be unable to connect to the VM.")
				fmt.Println("   💡 This is usually a Docker-to-Multipass networking issue on macOS.")
				fmt.Println("      Ensure Docker Desktop can route to the Multipass subnet (192.168.64.x).")
				fmt.Println("      You can test manually: docker exec hal-vault nc -z " + vmIP + " 22")
				return
			}
			fmt.Println("   ✅ Vault container can reach VM")

			// 5. Register OS plugin
			fmt.Println("⚙️  Registering OS secret engine plugin...")
			osPluginVersion := "0.1.0+ent"
			registerCmd := fmt.Sprintf(
				"VAULT_ADDR='http://127.0.0.1:8200' VAULT_TOKEN='root' vault plugin register -download -version='%s' secret vault-plugin-secrets-os",
				osPluginVersion,
			)
			if out, err := exec.Command(engine, "exec", "hal-vault", "sh", "-c", registerCmd).CombinedOutput(); err != nil {
				fmt.Printf("❌ Failed to register OS plugin: %v\nOutput: %s\n", err, string(out))
				fmt.Println("   💡 Check https://releases.hashicorp.com/vault-plugin-secrets-os for the latest version.")
				return
			}

			// 6. Mount OS secrets engine
			fmt.Println("⚙️  Enabling OS Secrets Engine...")
			_ = client.Sys().Unmount("os")
			if err = client.Sys().Mount("os", &vault.MountInput{
				Type: "vault-plugin-secrets-os",
			}); err != nil {
				fmt.Printf("❌ Failed to enable OS secrets engine: %v\n", err)
				return
			}

			// 7. Configure TOFU (trust-on-first-use for SSH host key verification)
			fmt.Println("⚙️  Configuring OS secret engine (TOFU host key verification)...")
			if _, err = client.Logical().Write("os/config", map[string]interface{}{
				"ssh_host_key_trust_on_first_use": true,
			}); err != nil {
				fmt.Printf("❌ Failed to configure OS settings: %v\n", err)
				return
			}

			// 8. Register the host
			fmt.Println("⚙️  Registering host configuration...")
			if _, err = client.Logical().Write("os/hosts/demo-vm", map[string]interface{}{
				"address": vmIP,
				"port":    22,
			}); err != nil {
				fmt.Printf("❌ Failed to create host: %v\n", err)
				return
			}
			fmt.Println("   ✅ Host 'demo-vm' registered")

			// 9. Register accounts
			//
			// mgmt-user: the privileged parent account. Vault SSHes in as this user and
			// runs `sudo chpasswd` to rotate other accounts' passwords.
			//
			// demouser / appadmin: target accounts. Vault rotates their passwords via mgmt-user.
			fmt.Println("⚙️  Registering accounts...")

			if _, err = client.Logical().Write("os/hosts/demo-vm/accounts/mgmt-user", map[string]interface{}{
				"username":          "mgmt-user",
				"password":          "mgmt-password-789",
				"verify_connection": false,
			}); err != nil {
				fmt.Printf("❌ Failed to register mgmt-user: %v\n", err)
				return
			}
			fmt.Println("   ✅ mgmt-user registered (parent account)")

			if _, err = client.Logical().Write("os/hosts/demo-vm/accounts/demouser", map[string]interface{}{
				"username":           "demouser",
				"password":           "initial-password-123",
				"parent_account_ref": "mgmt-user",
				"verify_connection":  false,
			}); err != nil {
				fmt.Printf("❌ Failed to register demouser: %v\n", err)
				return
			}
			fmt.Println("   ✅ demouser registered")

			if _, err = client.Logical().Write("os/hosts/demo-vm/accounts/appadmin", map[string]interface{}{
				"username":           "appadmin",
				"password":           "admin-password-456",
				"parent_account_ref": "mgmt-user",
				"verify_connection":  false,
			}); err != nil {
				fmt.Printf("❌ Failed to register appadmin: %v\n", err)
				return
			}
			fmt.Println("   ✅ appadmin registered")

			// 10. Rotate demouser password to verify end-to-end connectivity
			fmt.Println("⚙️  Testing end-to-end password rotation for demouser...")
			if _, err = client.Logical().Write("os/hosts/demo-vm/accounts/demouser/rotate", nil); err != nil {
				fmt.Printf("❌ Rotation failed: %v\n", err)
				fmt.Println("   💡 The plugin could not SSH into the VM. Check:")
				fmt.Println("      1. VM is reachable: nc -z " + vmIP + " 22")
				fmt.Println("      2. Password auth is enabled: multipass exec hal-vault-os -- sshd -T | grep passwordauth")
				fmt.Println("      3. SSH service is running: multipass exec hal-vault-os -- systemctl status ssh")
				return
			}

			secret, err := client.Logical().Read("os/hosts/demo-vm/accounts/demouser/creds")
			if err != nil || secret == nil {
				fmt.Printf("❌ Failed to read credentials after rotation: %v\n", err)
				return
			}
			password := secret.Data["password"].(string)

			fmt.Println("\n✅ Vault OS Secret Engine Integration Complete!")
			global.RefreshHalStatus(engine)
			fmt.Println("---------------------------------------------------------")
			fmt.Printf("🔗 VM Address  : %s\n", vmIP)
			fmt.Printf("👤 Users       : mgmt-user (parent), demouser, appadmin\n")
			fmt.Printf("🔑 demouser pwd: %s (just rotated by Vault)\n", password)
			fmt.Println("\n💡 HOW IT WORKS:")
			fmt.Println("   Vault SSHes into the VM as 'mgmt-user' and runs:")
			fmt.Println("   echo \"<user>:<newpass>\" | sudo /usr/sbin/chpasswd")
			fmt.Println("   mgmt-user has passwordless sudo for chpasswd only.")
			fmt.Println("\n📋 Try these commands:")
			fmt.Println("   vault write -f os/hosts/demo-vm/accounts/demouser/rotate   # Rotate demouser password")
			fmt.Println("   vault read os/hosts/demo-vm/accounts/demouser/creds        # Read current password")
			fmt.Println("   vault read os/hosts/demo-vm/accounts/appadmin/creds        # Read appadmin password")
			fmt.Printf("   multipass shell hal-vault-os                               # Shell into VM\n")
			fmt.Println("---------------------------------------------------------")
		}
	},
}

func extractMultipassIP(csvData string) string {
	lines := strings.Split(csvData, "\n")
	if len(lines) > 1 {
		cols := strings.Split(lines[1], ",")
		if len(cols) > 2 {
			return cols[2]
		}
	}
	return "127.0.0.1"
}

func init() {
	vaultOSCmd.Flags().BoolVarP(&osEnable, "enable", "e", false, "Deploy Ubuntu VM and configure Vault OS secret engine")
	vaultOSCmd.Flags().BoolVarP(&osDisable, "disable", "d", false, "Remove VM and clean up Vault OS configuration")
	vaultOSCmd.Flags().BoolVarP(&osUpdate, "update", "u", false, "Reconcile VM and Vault OS integration")
	_ = vaultOSCmd.Flags().MarkHidden("enable")
	_ = vaultOSCmd.Flags().MarkHidden("disable")
	_ = vaultOSCmd.Flags().MarkHidden("update")

	vaultOSCmd.Flags().StringVar(&osUbuntuImage, "ubuntu-image", "22.04", "Ubuntu image for Multipass VM")
	vaultOSCmd.Flags().StringVar(&osVMCPUs, "vm-cpus", "1", "Number of CPUs for the VM")
	vaultOSCmd.Flags().StringVar(&osVMMem, "vm-mem", "1G", "Amount of RAM for the VM")

	Cmd.AddCommand(vaultOSCmd)
}

// Made with Bob
