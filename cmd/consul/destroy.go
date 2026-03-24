package consul

import (
	"fmt"
	"hal/internal/global"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var consulDestroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Destroy the local Consul instance",
	Run: func(cmd *cobra.Command, args []string) {
		// 1. Detect Container Engine (Reusing the function from deploy.go)
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Println("❌ Error: Neither Docker nor Podman appear to be running.")
			return
		}

		if global.Debug {
			fmt.Printf("[DEBUG] Using container engine: %s\n", engine)
		}

		fmt.Printf("💥 Destroying Consul instance via %s...\n", engine)

		if global.DryRun {
			fmt.Printf("[DRY RUN] Would execute: %s rm -f hal-consul\n", engine)
			return
		}

		// 2. Execute the destroy command (Force remove skips the need to 'stop' first)
		out, err := exec.Command(engine, "rm", "-f", "hal-consul").CombinedOutput()
		if err != nil {
			// Handle the case where the container doesn't even exist
			outputStr := string(out)
			if strings.Contains(outputStr, "No such container") || strings.Contains(outputStr, "no container") {
				fmt.Println("⚠️  No Consul instance named 'hal-consul' found. It might already be destroyed.")
				return
			}

			fmt.Printf("❌ Failed to destroy Consul container.\nReason: %s\nOutput: %s\n", err, outputStr)
			return
		}

		if global.Debug {
			fmt.Printf("[DEBUG] Engine output: %s\n", strings.TrimSpace(string(out)))
		}

		global.CleanNetworkIfEmpty(engine)

		fmt.Println("✅ Consul instance destroyed successfully!")
	},
}

func init() {
	Cmd.AddCommand(consulDestroyCmd)
}
