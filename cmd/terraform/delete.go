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

// Primary-only Terraform Enterprise containers and helpers.
var tfePrimaryContainers = []string{
	"hal-tfe",
	"hal-tfe-proxy",
	tfeAPIPrimaryContainer,
	legacyTFECLIContainerName,
	"hal-tfe-agent",
}

// Shared backend components used by primary and twin TFE instances.
var tfeSharedBackendContainers = []string{
	"hal-tfe-db",
	"hal-tfe-redis",
	"hal-tfe-minio",
}

var destroyCmd = &cobra.Command{
	Use:   "delete",
	Short: "Tear down the TFE stack and wipe all local state for a fresh restart",
	Run: func(cmd *cobra.Command, args []string) {
		target, err := normalizeTFETarget(tfeLifecycleTarget)
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		if target == tfeTargetTwin || target == tfeTargetBoth {
			layout, layoutErr := buildTFETwinLayout()
			if layoutErr != nil {
				fmt.Printf("❌ Invalid twin configuration: %v\n", layoutErr)
				return
			}
			destroyTFETwin(engine, layout)
			if target == tfeTargetTwin {
				return
			}
		}

		preserveSharedBackend := false
		if target == tfeTargetPrimary {
			layout, layoutErr := buildTFETwinLayout()
			if layoutErr != nil {
				fmt.Printf("❌ Invalid twin configuration: %v\n", layoutErr)
				return
			}
			if global.IsContainerRunning(engine, layout.CoreContainer) {
				preserveSharedBackend = true
			}
		}

		fmt.Printf("⚙️  Destroying Terraform Enterprise ecosystem via %s...\n", engine)
		if preserveSharedBackend {
			fmt.Println("ℹ️  Twin instance is running; preserving shared backend containers (hal-tfe-db, hal-tfe-redis, hal-tfe-minio).")
		}

		// 1. Destroy all associated containers
		containersToDestroy := append([]string{}, tfePrimaryContainers...)
		if !preserveSharedBackend {
			containersToDestroy = append(containersToDestroy, tfeSharedBackendContainers...)
		}

		for _, container := range containersToDestroy {
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

		helperImages := []string{legacyTFECLIImageName, tfeAPIPrimaryImage, tfeAPITwinImage}
		for _, helperImage := range helperImages {
			if global.DryRun {
				fmt.Printf("[DRY RUN] Would execute: %s image rm -f %s\n", engine, helperImage)
				continue
			}
			if out, err := exec.Command(engine, "image", "rm", "-f", helperImage).CombinedOutput(); err != nil {
				outputStr := strings.ToLower(strings.TrimSpace(string(out)))
				if !strings.Contains(outputStr, "no such image") && !strings.Contains(outputStr, "image not known") {
					fmt.Printf("⚠️  Failed to remove helper image '%s': %s\n", helperImage, strings.TrimSpace(string(out)))
				}
			} else {
				fmt.Printf("  ✅ Removed helper image: %s\n", helperImage)
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

		if err := removeTFEAgentState(tfeTargetPrimary); err != nil {
			fmt.Printf("⚠️  Could not remove cached primary TFE agent state: %v\n", err)
		} else {
			fmt.Println("  🧹 Removed cached primary TFE agent state.")
		}
		if err := removeTFEAgentState(tfeTargetTwin); err != nil {
			fmt.Printf("⚠️  Could not remove cached twin TFE agent state: %v\n", err)
		} else {
			fmt.Println("  🧹 Removed cached twin TFE agent state.")
		}

		if preserveSharedBackend {
			fmt.Println("  ℹ️  Shared backend state retained for running twin instance.")
		}

		if !global.DryRun {
			fmt.Println("\n✅ TFE environment wiped. You are ready for a clean 'hal terraform create'.")
		}
	},
}

func init() {
	bindTFETargetFlag(destroyCmd)
	bindTwinFlags(destroyCmd)
	Cmd.AddCommand(destroyCmd)
}
