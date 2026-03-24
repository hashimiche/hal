package vault

import (
	"fmt"
	"hal/internal/global"
	"strings"

	"github.com/spf13/cobra"
)

var (
	disableAuditPath string
	disableLoki      bool
)

var vaultAuditDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable a Vault audit device",
	Run: func(cmd *cobra.Command, args []string) {
		// 1. Setup the Vault Go Client using our LB-style Helper
		client, err := GetHealthyClient()
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		// 2. Determine what we are disabling based on flags
		pathsToDisable := []string{}

		if disableLoki {
			pathsToDisable = append(pathsToDisable, "socket-loki/")
		} else {
			// Vault API expects paths with a trailing slash, but we handle it gracefully
			path := disableAuditPath
			if !strings.HasSuffix(path, "/") {
				path += "/"
			}
			pathsToDisable = append(pathsToDisable, path)
		}

		// 3. Execute the teardown
		for _, path := range pathsToDisable {
			if global.DryRun {
				fmt.Printf("[DRY RUN] Would call API: sys/audit/%s (DELETE)\n", path)
				continue
			}

			fmt.Printf("🛑 Disabling audit device at path '%s'...\n", path)

			err := client.Sys().DisableAudit(path)
			if err != nil {
				// Graceful error handling for idempotency
				if strings.Contains(err.Error(), "no matching mount") {
					fmt.Printf("⚠️  Audit device '%s' is not currently enabled (nothing to do).\n", path)
				} else {
					fmt.Printf("❌ Failed to disable '%s': %v\n", path, err)
				}
			} else {
				fmt.Printf("✅ Successfully disabled audit device: %s\n", path)
			}
		}
	},
}

func init() {
	vaultAuditDisableCmd.Flags().StringVarP(&disableAuditPath, "path", "p", "file/", "Path of the audit device to disable (e.g., file/)")

	// Symmetrical flag to easily clean up our fail-safe socket
	vaultAuditDisableCmd.Flags().BoolVar(&disableLoki, "loki", false, "Disable the Loki TCP socket audit device (socket-loki/)")

	vaultAuditCmd.AddCommand(vaultAuditDisableCmd)
}
