package vault

import (
	"fmt"
	"hal/internal/global"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

// The "Known Universe" of Vault infrastructure.
// As you build new Vault features that require Docker containers, just add them here!
var vaultEcosystem = []string{
	"hal-vault",
	"hal-keycloak",
	"hal-gitlab",
	"hal-gitlab-runner",
	"hal-openldap",
	"hal-phpldapadmin",
	"hal-mariadb",
}

var vaultVolumes = []string{
	"hal-vault-logs", // Spun up by Audit/Loki
}

var vaultDestroyCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete the local Vault instance and associated extensions (like Keycloak)",
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Println("❌ Error: Neither Docker nor Podman appear to be running.")
			return
		}

		if global.Debug {
			fmt.Printf("[DEBUG] Using container engine: %s\n", engine)
		}

		fmt.Printf("⚙️  Destroying Vault ecosystem via %s...\n", engine)

		// 1. Destroy all associated containers
		for _, container := range vaultEcosystem {
			if global.DryRun {
				fmt.Printf("[DRY RUN] Would execute: %s rm -f %s\n", engine, container)
				continue
			}

			out, err := exec.Command(engine, "rm", "-f", container).CombinedOutput()
			if err != nil {
				// We only care if the error is something OTHER than "container not found"
				outputStr := string(out)
				if !strings.Contains(outputStr, "No such container") && !strings.Contains(outputStr, "no container") {
					fmt.Printf("⚠️  Failed to destroy '%s': %s\n", container, strings.TrimSpace(outputStr))
				}
			} else {
				// If it successfully deleted something, let the user know!
				if strings.TrimSpace(string(out)) == container {
					fmt.Printf("  ✅ Destroyed container: %s\n", container)
				}
			}
		}

		// 2. Destroy all associated volumes
		for _, volume := range vaultVolumes {
			if global.DryRun {
				fmt.Printf("[DRY RUN] Would execute: %s volume rm -f %s\n", engine, volume)
				continue
			}

			// Volumes fail loudly if they are in use, but we just killed the containers, so it's safe.
			_ = exec.Command(engine, "volume", "rm", "-f", volume).Run()
		}

		// 3. Attempt to clean the network (Only deletes hal-net if NO containers are using it)
		global.CleanNetworkIfEmpty(engine)

		if err := global.RemoveObsPromTargetFile("vault"); err != nil {
			fmt.Printf("⚠️  Could not remove Vault observability target file: %v\n", err)
		}

		if !global.DryRun {
			fmt.Println("\n✅ Vault instance and all extensions destroyed successfully!")
			global.RefreshHalStatus(engine)
		}
	},
}

func init() {
	Cmd.AddCommand(vaultDestroyCmd)
}
