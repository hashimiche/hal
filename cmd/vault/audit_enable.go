package vault

import (
	"fmt"
	"hal/internal/global"

	vault "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
)

var (
	auditType string
	auditPath string
	lokiStack bool
)

var vaultAuditEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable a Vault audit device",
	Run: func(cmd *cobra.Command, args []string) {
		// 1. Setup the Vault Go Client
		client, err := GetHealthyClient()
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		// 2. UX: The Magic Loki Flag
		// If --loki is passed, we force the type to 'file' because our
		// robust observability stack uses the shared Docker volume.
		if lokiStack {
			auditType = "file"
			fmt.Println("🌊 Loki flag detected! Using shared Docker volume architecture...")
		} else if auditType == "file" {
			fmt.Println("📝 Enabling 'file' audit device at path 'file/' (/vault/logs/audit.log)...")
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
			fmt.Printf("[DRY RUN] Would call API: sys/audit/%s with type %s\n", auditPath, auditType)
			return
		}

		// 3. Enable the Audit Device
		err = client.Sys().EnableAuditWithOptions(auditPath, options)
		if err != nil {
			fmt.Printf("⚠️  Note: Could not enable %s (It might already be enabled!)\n", auditType)
		} else {
			fmt.Printf("✅ Audit device (%s) enabled successfully!\n", auditType)
		}

		// 4. Output the correct instructions to the user
		if lokiStack {
			fmt.Println("\n💡 HAL Tip for Observability:")
			fmt.Println("   Vault is now writing to the 'hal-vault-logs' Docker volume.")
			fmt.Println("   Promtail reads this passively, meaning Vault will never block on network errors!")
		} else if auditType == "file" {
			fmt.Println("\n💡 HAL Tip: To view these logs live from the container, run:")
			fmt.Println("   docker exec -it hal-vault tail -f /vault/logs/audit.log")
		}
	},
}

func init() {
	vaultAuditEnableCmd.Flags().StringVarP(&auditType, "type", "t", "file", "Type of audit device (file, socket, syslog)")
	vaultAuditEnableCmd.Flags().StringVarP(&auditPath, "path", "p", "file", "Path to mount the audit device (e.g., file/)")

	// The Observability Magic Flag
	vaultAuditEnableCmd.Flags().BoolVar(&lokiStack, "loki", false, "Auto-configure the shared volume integration for Promtail/Loki")

	vaultAuditCmd.AddCommand(vaultAuditEnableCmd)
}
