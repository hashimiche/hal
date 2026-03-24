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
		// 1. Setup the Vault Go Client using our LB-style Helper
		client, err := GetHealthyClient()
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		// 2. We will ALWAYS enable the file audit device first for safety.
		// (Unless the user explicitly overrides the type via flag)
		if !lokiStack && auditType == "file" {
			fmt.Println("📝 Enabling 'file' audit device at path 'file/' (/tmp/vault_audit.log)...")
		}

		options := &vault.EnableAuditOptions{
			Type:    auditType,
			Options: map[string]string{},
		}

		// Using a tagged switch statement
		switch auditType {
		case "file":
			options.Options["file_path"] = "/tmp/vault_audit.log"
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

		// 3. Enable the Primary Audit Device
		err = client.Sys().EnableAuditWithOptions(auditPath, options)
		if err != nil {
			// If it's already enabled, Vault throws an error. We can catch it gracefully.
			fmt.Printf("⚠️  Note: Could not enable %s (It might already be enabled!)\n", auditType)
		} else {
			fmt.Printf("✅ Primary audit device (%s) enabled successfully!\n", auditType)
		}

		// 4. The Loki Fail-Safe Stack
		if lokiStack {
			fmt.Println("\n🌊 Loki flag detected! Configuring secondary TCP socket for log streaming...")

			lokiOptions := &vault.EnableAuditOptions{
				Type: "socket",
				Options: map[string]string{
					"address":     "127.0.0.1:9080",
					"socket_type": "tcp",
				},
			}

			err = client.Sys().EnableAuditWithOptions("socket-loki", lokiOptions)
			if err != nil {
				fmt.Printf("⚠️  Could not enable Loki socket: %v\n", err)
			} else {
				fmt.Println("✅ Loki TCP socket enabled at path 'socket-loki/'!")
				fmt.Println("\n💡 HAL Tip for Observability:")
				fmt.Println("   Vault is now writing to BOTH a local file and blasting logs to TCP port 9080.")
				fmt.Println("   If your Loki/Promtail stack goes down, Vault will NOT block requests because the file acts as a fail-safe.")
			}
		} else if auditType == "file" {
			fmt.Println("\n💡 HAL Tip: To view these logs live from the container, run:")
			fmt.Println("   docker exec -it hal-vault tail -f /tmp/vault_audit.log")
		}
	},
}

func init() {
	vaultAuditEnableCmd.Flags().StringVarP(&auditType, "type", "t", "file", "Type of audit device (file, socket, syslog)")
	vaultAuditEnableCmd.Flags().StringVarP(&auditPath, "path", "p", "file", "Path to mount the audit device (e.g., file/)")

	// The Observability Magic Flag
	vaultAuditEnableCmd.Flags().BoolVar(&lokiStack, "loki", false, "Auto-configure a fail-safe socket to stream logs to Loki")

	vaultAuditCmd.AddCommand(vaultAuditEnableCmd)
}
