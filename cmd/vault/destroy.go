package vault

import (
	"fmt"
	"hal/internal/global"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var vaultDestroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Destroy the local Vault instance and associated extensions (like Keycloak)",
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Println("❌ Error: Neither Docker nor Podman appear to be running.")
			return
		}

		if global.Debug {
			fmt.Printf("[DEBUG] Using container engine: %s\n", engine)
		}

		fmt.Printf("⚙️  Destroying Vault instance via %s...\n", engine)

		if global.DryRun {
			fmt.Printf("[DRY RUN] Would execute: %s rm -f hal-vault hal-keycloak\n", engine)
			return
		}

		// 1. Destroy the primary Vault container
		out, err := exec.Command(engine, "rm", "-f", "hal-vault").CombinedOutput()
		if err != nil {
			outputStr := string(out)
			if strings.Contains(outputStr, "No such container") || strings.Contains(outputStr, "no container") {
				fmt.Println("⚠️  No Vault instance named 'hal-vault' found. It might already be destroyed.")
			} else {
				fmt.Printf("❌ Failed to destroy Vault container.\nReason: %s\nOutput: %s\n", err, outputStr)
			}
		}

		if global.Debug {
			fmt.Printf("[DEBUG] Engine output: %s\n", strings.TrimSpace(string(out)))
		}

		// 2. Silently sweep up any companion containers (like Keycloak for OIDC)
		_ = exec.Command(engine, "rm", "-f", "hal-keycloak").Run()

		// 3. Attempt to clean the network
		global.CleanNetworkIfEmpty(engine)

		// 4, Clean audit volume if created
		_ = exec.Command(engine, "volume", "rm", "-f", "hal-vault-logs").Run()

		fmt.Println("✅ Vault instance and extensions destroyed successfully!")
	},
}

func init() {
	Cmd.AddCommand(vaultDestroyCmd)
}
