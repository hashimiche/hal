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
	"hal-tfe-proxy",
	"hal-tfe-db",
	"hal-tfe-redis",
	"hal-tfe-minio",
	"hal-tfe-cli",
	"hal-tfe-agent",
}

var destroyCmd = &cobra.Command{
	Use:     "delete",
	Aliases: []string{"destroy"},
	Short:   "Tear down the TFE stack and wipe all local state for a fresh restart",
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

		if extraAgentIDs, err := global.ListTFEAgentContainerIDs(engine); err != nil {
			fmt.Printf("⚠️  Could not discover TFE agent containers: %v\n", err)
		} else if len(extraAgentIDs) > 0 {
			args := append([]string{"rm", "-f"}, extraAgentIDs...)
			if global.DryRun {
				fmt.Printf("[DRY RUN] Would execute: %s %s\n", engine, strings.Join(args, " "))
			} else if out, err := exec.Command(engine, args...).CombinedOutput(); err != nil {
				fmt.Printf("⚠️  Failed to destroy TFE agent containers: %s\n", strings.TrimSpace(string(out)))
			} else {
				fmt.Printf("  ✅ Destroyed %d TFE agent container(s).\n", len(extraAgentIDs))
			}
		}

		if global.DryRun {
			fmt.Printf("[DRY RUN] Would execute: %s image rm -f %s\n", engine, tfeCLIImageName)
		} else {
			if out, err := exec.Command(engine, "image", "rm", "-f", tfeCLIImageName).CombinedOutput(); err != nil {
				outputStr := strings.ToLower(strings.TrimSpace(string(out)))
				if !strings.Contains(outputStr, "no such image") && !strings.Contains(outputStr, "image not known") {
					fmt.Printf("⚠️  Failed to remove helper image '%s': %s\n", tfeCLIImageName, strings.TrimSpace(string(out)))
				}
			} else {
				fmt.Printf("  ✅ Removed helper image: %s\n", tfeCLIImageName)
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

		if err := global.RemoveObsPromTargetFile("terraform"); err != nil {
			fmt.Printf("⚠️  Could not remove Terraform observability target file: %v\n", err)
		}

		if err := global.RemoveCachedTFEAPIToken(); err != nil {
			fmt.Printf("⚠️  Could not remove cached TFE API token: %v\n", err)
		} else {
			fmt.Println("  🧹 Removed cached TFE API token.")
		}

		if err := removeTFEAgentState(); err != nil {
			fmt.Printf("⚠️  Could not remove cached TFE agent state: %v\n", err)
		} else {
			fmt.Println("  🧹 Removed cached TFE agent state.")
		}

		if !global.DryRun {
			fmt.Println("\n✅ TFE environment wiped. You are ready for a clean 'hal terraform create'.")
		}
	},
}

func init() {
	Cmd.AddCommand(destroyCmd)
}
