package vault

import (
	"fmt"
	"hal/internal/global"
	"os/exec"
	"strings"

	vault "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
)

var (
	enableFlag  bool
	disableFlag bool
	updateFlag  bool
	forceFlag   bool
	lokiStack   bool
	auditType   string
	auditPath   string
)

var vaultAuditCmd = &cobra.Command{
	Use:   "audit [status|enable|disable|update]",
	Short: "Manage Vault audit logging (Defaults to smart status check)",
	Run: func(cmd *cobra.Command, args []string) {
		if err := parseLifecycleAction(args, &enableFlag, &disableFlag, &updateFlag); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		client, err := GetHealthyClient()
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		// Ensure path has trailing slash for Vault API consistency
		apiPath := auditPath
		if !strings.HasSuffix(apiPath, "/") {
			apiPath += "/"
		}

		// ==========================================
		// 1. SMART STATUS MODE (Default behavior)
		// ==========================================
		if !enableFlag && !disableFlag && !updateFlag && !forceFlag {
			fmt.Println("🔍 Checking Vault Audit Status...")

			// Check Vault for the audit mount
			audits, _ := client.Sys().ListAudit()
			_, fileEnabled := audits[apiPath]

			// Output Status
			if fileEnabled {
				fmt.Printf("  ✅ Audit Device : Active at '%s'\n", apiPath)
				fmt.Printf("  📝 Log File     : /vault/logs/audit.log (Shared Volume)\n")
			} else {
				fmt.Printf("  ❌ Audit Device : Not configured\n")
			}

			// The Streamlined Assistant Logic
			fmt.Println("\n💡 Next Step:")
			if !fileEnabled {
				fmt.Println("   To enable audit logging (with Loki support), run:")
				fmt.Println("   hal vault audit enable --loki")
			} else {
				fmt.Println("   To remove this configuration, run:")
				fmt.Println("   hal vault audit disable")
			}
			return
		}

		// ==========================================
		// 2. TEARDOWN / RESET PATH (--disable / --force)
		// ==========================================
		if disableFlag || updateFlag || forceFlag {
			if global.DryRun {
				fmt.Printf("[DRY RUN] Would disable audit device: %s\n", apiPath)
			} else {
				if disableFlag {
					fmt.Printf("🛑 Disabling audit device at path '%s'...\n", apiPath)
				} else {
					fmt.Printf("♻️  Force flag detected. Resetting audit device at '%s'...\n", apiPath)
				}

				err := client.Sys().DisableAudit(apiPath)
				if err != nil {
					if strings.Contains(err.Error(), "no matching mount") {
						if disableFlag {
							fmt.Printf("⚠️  Audit device '%s' is not currently enabled (nothing to do).\n", apiPath)
						}
					} else {
						fmt.Printf("⚠️  Could not disable '%s': %v\n", apiPath, err)
					}
				} else {
					fmt.Printf("✅ Successfully disabled audit device: %s\n", apiPath)
				}
			}

			// THE CLEAN SLATE FIX: We can't delete the Docker volume, but we CAN delete the data inside it!
			if apiPath == "file/" || lokiStack {
				fmt.Println("🧹 Purging old audit logs from the shared volume...")
				engine, _ := global.DetectEngine()
				// We use 'exec' to reach inside the running container and delete the file
				_ = exec.Command(engine, "exec", "hal-vault", "sh", "-c", "rm -f /vault/logs/audit.log").Run()
			}

			// If the user ONLY wanted to disable, exit here.
			if disableFlag && !global.DryRun {
				return
			}
		}

		// ==========================================
		// 3. DEPLOY / ENABLE PATH (--enable / --force)
		// ==========================================
		if enableFlag || updateFlag || forceFlag {
			if lokiStack {
				auditType = "file"
				fmt.Println("🌊 Loki flag detected! Using shared Docker volume architecture...")
			} else if auditType == "file" {
				fmt.Println("📝 Enabling 'file' audit device...")
			}

			options := &vault.EnableAuditOptions{
				Type:    auditType,
				Options: map[string]string{},
			}

			switch auditType {
			case "file":
				options.Options["file_path"] = "/vault/logs/audit.log"
			case "socket":
				options.Options["address"] = "127.0.0.1:9080"
				options.Options["socket_type"] = "tcp"
			case "syslog":
				options.Options["facility"] = "AUTH"
				options.Options["tag"] = "vault"
			default:
				fmt.Printf("❌ Unsupported audit type: %s\n", auditType)
				return
			}

			if global.DryRun {
				fmt.Printf("[DRY RUN] Would enable API: sys/audit/%s with type %s\n", auditPath, auditType)
				return
			}

			err = client.Sys().EnableAuditWithOptions(auditPath, options)
			if err != nil {
				fmt.Printf("⚠️  Note: Could not enable %s (It might already be enabled! Try --force)\n", auditType)
			} else {
				fmt.Printf("✅ Audit device (%s) enabled successfully!\n", auditType)
			}

			if lokiStack {
				fmt.Println("\n💡 HAL Tip for Observability:")
				fmt.Println("   Vault is now writing to the 'hal-vault-logs' Docker volume.")
				fmt.Println("   Promtail reads this passively, meaning Vault will never block on network errors!")
			} else if auditType == "file" {
				fmt.Println("\n💡 HAL Tip: To view these logs live from the container, run:")
				fmt.Println("   docker exec -it hal-vault tail -f /vault/logs/audit.log")
			}
		}
	},
}

func init() {
	// Standard Lifecycle Flags
	vaultAuditCmd.Flags().BoolVarP(&enableFlag, "enable", "e", false, "Enable the audit configuration")
	vaultAuditCmd.Flags().BoolVarP(&disableFlag, "disable", "d", false, "Disable the audit configuration")
	vaultAuditCmd.Flags().BoolVarP(&updateFlag, "update", "u", false, "Reconcile the audit configuration (disable then enable)")
	vaultAuditCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force a clean reconfiguration (disable then enable)")
	_ = vaultAuditCmd.Flags().MarkHidden("enable")
	_ = vaultAuditCmd.Flags().MarkHidden("disable")
	_ = vaultAuditCmd.Flags().MarkHidden("update")
	_ = vaultAuditCmd.Flags().MarkDeprecated("force", "use --update instead")

	// Feature-Specific Flags
	vaultAuditCmd.Flags().StringVarP(&auditType, "type", "t", "file", "Type of audit device (file, socket, syslog)")
	vaultAuditCmd.Flags().StringVarP(&auditPath, "path", "p", "file", "Path to mount the audit device (e.g., file/)")
	vaultAuditCmd.Flags().BoolVar(&lokiStack, "loki", false, "Auto-configure the shared volume integration for Promtail/Loki")

	Cmd.AddCommand(vaultAuditCmd)
}
