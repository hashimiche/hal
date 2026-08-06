package vault

import (
	"fmt"
	"hal/internal/global"
	"hal/internal/integrations"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

// The "Known Universe" of Vault infrastructure.
// As you build new Vault features that require Docker containers, just add them here!
// NOTE: GitLab (hal-gitlab / hal-gitlab-runner) is intentionally NOT listed here.
// It is a shared service (also used by 'hal tf vcs-workflow'), so it is torn down
// via teardownSharedGitLab() with a consumer/TFE-runtime check instead of being
// force-removed unconditionally.
var vaultEcosystem = []string{
	vaultContainer,
	openLDAPContainer,
	phpLDAPAdminContainer,
	vaultMariaDBContainer,
	vaultOracleContainer,
}

var vaultVolumes = []string{
	vaultLogsVolume,    // Spun up by Audit/Loki
	vaultPluginsVolume, // Spun up for external plugins (OS secret engine)
	vaultDataVolume,    // Vault file storage (/vault/file VOLUME directive)
}

var vaultDestroyCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete the local Vault instance and associated extensions (like Authentik OIDC and GitLab)",
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

		// The SoftHSM runtime is owned by vault create/delete. Remove it only
		// after the Vault container is gone so the engine cannot report it in use.
		softHSMRuntimeRef := vaultSoftHSMRuntimeImage + ":" + vaultSoftHSMRuntimeTag
		if global.DryRun {
			fmt.Printf("[DRY RUN] Would remove local image %s if present\n", softHSMRuntimeRef)
		} else if exec.Command(engine, "image", "inspect", softHSMRuntimeRef).Run() == nil {
			if err := exec.Command(engine, "image", "rm", "-f", softHSMRuntimeRef).Run(); err != nil {
				fmt.Printf("⚠️  Could not remove image %s: %v\n", softHSMRuntimeRef, err)
			} else {
				fmt.Printf("  ✅ Removed local image: %s\n", softHSMRuntimeRef)
			}
		}

		// 1b. Authentik is a shared IdP (also used by 'hal tf saml'), so it is
		// deregistered and only torn down when no other product still depends on
		// it — mirroring the GitLab shared-service model rather than force-removing
		// a container another lab may be using.
		if global.DryRun {
			fmt.Println("[DRY RUN] Would deregister vault-oidc from the Authentik shared service and stop the stack if unused")
		} else {
			remaining, regErr := global.RemoveSharedServiceConsumer(integrations.AuthentikSharedServiceKey, oidcSharedServiceKey)
			if regErr != nil {
				fmt.Printf("⚠️  Could not update shared service registry: %v\n", regErr)
			}
			if len(remaining) == 0 {
				if err := integrations.StopAuthentikStack(engine, true); err != nil {
					fmt.Printf("⚠️  Warning during Authentik teardown: %v\n", err)
				} else {
					fmt.Println("  ✅ Authentik stack stopped (no other products depend on it)")
				}
			} else {
				fmt.Printf("  ℹ️  Authentik still in use by: %s — stack left running\n", strings.Join(remaining, ", "))
			}
		}

		// 1c. GitLab is a shared service (also used by 'hal tf vcs-workflow'), so
		// it is deregistered and only removed when no other product depends on it
		// and no Terraform Enterprise runtime is running. The vault-jwt runner is
		// always removed.
		if global.DryRun {
			fmt.Println("[DRY RUN] Would remove hal-gitlab-runner and deregister vault-jwt; hal-gitlab stops only if unused by TF")
		} else {
			teardownSharedGitLab(engine)
		}

		// 2. Destroy KinD cluster if one is running — all three --k8s Vault
		//    features (hal vault k8s, hal vault database --k8s, hal vault pki
		//    --k8s/--acme) share a single cluster that is exclusively owned by
		//    the Vault ecosystem, so hal vault delete always removes it.
		if global.DryRun {
			fmt.Println("[DRY RUN] Would execute: kind delete cluster (if running)")
		} else {
			clusterOut, _ := exec.Command("kind", "get", "clusters").Output()
			if strings.Contains(string(clusterOut), "kind") {
				fmt.Println("⚙️  Destroying KinD cluster...")
				if err := exec.Command("kind", "delete", "cluster").Run(); err != nil {
					fmt.Printf("⚠️  Could not remove KinD cluster: %v\n", err)
				} else {
					fmt.Println("  ✅ Destroyed KinD cluster")
				}
			}
		}

		// 3. Destroy all associated volumes
		for _, volume := range vaultVolumes {
			if global.DryRun {
				fmt.Printf("[DRY RUN] Would execute: %s volume rm -f %s\n", engine, volume)
				continue
			}

			// Volumes fail loudly if they are in use, but we just killed the containers, so it's safe.
			_ = exec.Command(engine, "volume", "rm", "-f", volume).Run()
		}

		// 3. Remove production-mode state (~/.hal/vault-prod: init.json, vault.hcl,
		//    TLS certs) so a delete never strands the saved unseal key / root token.
		if global.DryRun {
			fmt.Printf("[DRY RUN] Would remove prod state dir: %s\n", global.VaultProdStateDir())
		} else if err := global.RemoveCachedVaultInit(); err != nil {
			fmt.Printf("⚠️  Could not remove prod Vault state (%s): %v\n", global.VaultProdStateDir(), err)
		}

		// 5. Attempt to clean the network (Only deletes hal-net if NO containers are using it)
		global.CleanNetworkIfEmpty(engine)

		if err := global.RemoveObsPromTargetFile("vault"); err != nil {
			fmt.Printf("⚠️  Could not remove Vault observability target file: %v\n", err)
		}

		if !global.DryRun {
			fmt.Println("\n✅ Vault instance and all extensions destroyed successfully!")
			global.RefreshHalHealth(engine)
		}
	},
}

func init() {
	Cmd.AddCommand(vaultDestroyCmd)
}
