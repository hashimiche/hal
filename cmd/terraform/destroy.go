package terraform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

// The "Known Universe" of Terraform Enterprise infrastructure
var tfeEcosystem = []string{
	"hal-tfe",
	"hal-tfe-db",
	"hal-tfe-redis",
	"hal-tfe-minio",
}

var destroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Tear down the TFE stack and wipe all local state for a fresh restart",
	Run: func(cmd *cobra.Command, args []string) {

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		fmt.Printf("⚙️  Destroying Terraform Enterprise ecosystem via %s...\n", engine)

		// 1. Destroy all associated containers
		for _, container := range tfeEcosystem {
			if global.DryRun {
				fmt.Printf("[DRY RUN] Would execute: %s rm -f %s\n", engine, container)
				continue
			}

			out, err := exec.Command(engine, "rm", "-f", container).CombinedOutput()
			if err != nil {
				outputStr := string(out)
				if !strings.Contains(outputStr, "No such container") && !strings.Contains(outputStr, "no container") {
					fmt.Printf("⚠️  Failed to destroy '%s': %s\n", container, strings.TrimSpace(outputStr))
				}
			} else {
				if strings.TrimSpace(string(out)) == container {
					fmt.Printf("  ✅ Destroyed container: %s\n", container)
				}
			}
		}

		// 2. Wipe the local Cert cache
		homeDir, _ := os.UserHomeDir()
		certDir := filepath.Join(homeDir, ".hal", "tfe-certs")
		if _, err := os.Stat(certDir); err == nil {
			if global.DryRun {
				fmt.Printf("[DRY RUN] Would execute: rm -rf %s\n", certDir)
			} else {
				fmt.Println("  🧹 Wiping local TLS certificate cache...")
				_ = os.RemoveAll(certDir)
			}
		}

		// 3. Attempt to clean the network
		global.CleanNetworkIfEmpty(engine)

		if !global.DryRun {
			fmt.Println("\n✅ TFE environment wiped. You are ready for a clean 'hal terraform deploy'.")
		}
	},
}

func init() {
	Cmd.AddCommand(destroyCmd)
}
