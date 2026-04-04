package vault

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var vaultStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the deep status of the local Vault container, API, and ecosystem",
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Println("⚪ Error: Container engine not running.")
			return
		}

		fmt.Println("🔍 Analyzing HashiCorp Vault Ecosystem...")

		// ==========================================
		// 1. INFRASTRUCTURE LAYER (Containers)
		// ==========================================
		fmt.Println("  [ Container Infrastructure ]")

		// 🎯 FIX: Use Output() instead of CombinedOutput() so we don't capture stderr garbage
		vaultCheck, err := exec.Command(engine, "inspect", "-f", "{{.State.Status}}", "hal-vault").Output()
		vaultStatus := strings.TrimSpace(string(vaultCheck))

		// 🎯 FIX: If err != nil, the container just doesn't exist
		if err != nil {
			fmt.Println("  ⚪ hal-vault          : Down (hal vault deploy to start)")
			fmt.Println("\n💡 Tip: Deploy the core Vault instance first to see API health.")
			return
		} else if vaultStatus == "running" {
			fmt.Println("  🟢 hal-vault          : Up   (vault.localhost:8200)")
		} else {
			fmt.Printf("  🟡 hal-vault          : %s\n", strings.ToUpper(vaultStatus))
			fmt.Println("\n  📜 Fetching recent crash logs...")

			// We DO want stderr here so we can see why it crashed!
			logsOut, _ := exec.Command(engine, "logs", "--tail", "10", "hal-vault").CombinedOutput()
			logStr := strings.TrimSpace(string(logsOut))

			if logStr != "" {
				fmt.Println("  ----------------- ENGINE LOGS -----------------")
				fmt.Println("  " + strings.ReplaceAll(logStr, "\n", "\n  "))
				fmt.Println("  -----------------------------------------------")

				if strings.Contains(strings.ToLower(logStr), "license") {
					fmt.Println("  💡 HAL Tip: Looks like a Vault Enterprise license rejection.")
					fmt.Println("     Ensure $VAULT_LICENSE is valid, then run: hal vault deploy --edition ent --force")
				}
			}
			return // Stop execution if Vault is crashed
		}

		// Ecosystem Containers
		features := []struct {
			Name      string
			Container string
			Command   string
		}{
			{"OIDC (Keycloak)", "hal-keycloak", "oidc"},
			{"JWT (GitLab)", "hal-gitlab", "jwt"},
			{"LDAP (OpenLDAP)", "hal-openldap", "ldap"},
			{"DBs (MariaDB)", "hal-mariadb", "mariadb"},
			{"K8s (KinD)", "kind-control-plane", "k8s"},
		}

		for _, f := range features {
			// 🎯 FIX: Same here. Ignore stderr, just check the error code.
			out, err := exec.Command(engine, "inspect", "-f", "{{.State.Status}}", f.Container).Output()
			status := strings.TrimSpace(string(out))

			if err != nil {
				// Container is completely gone
				fmt.Printf("  ⚪ %-18s : Down (hal vault %s -e to enable)\n", f.Name, f.Command)
			} else if status == "running" {
				// Container is happy
				fmt.Printf("  🟢 %-18s : Up   (hal vault %s -d to disable)\n", f.Name, f.Command)
			} else {
				// Container exists but is "exited", "paused", "created", etc.
				fmt.Printf("  🟡 %-18s : %-4s (hal vault %s -f to reset)\n", f.Name, strings.ToUpper(status), f.Command)
			}
		}

		// ==========================================
		// 2. CORE APP LAYER (API Health)
		// ==========================================
		fmt.Println("\n  [ Vault API Health ]")
		client := http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get("http://127.0.0.1:8200/v1/sys/health")

		if err != nil {
			fmt.Println("  🟡 API is unreachable (Vault might still be booting up).")
			return
		}
		defer resp.Body.Close()

		var health map[string]interface{}
		isUnsealed := false

		if err := json.NewDecoder(resp.Body).Decode(&health); err == nil {
			fmt.Printf("  Version     : %v\n", health["version"])
			fmt.Printf("  Initialized : %v\n", health["initialized"])
			fmt.Printf("  Sealed      : %v\n", health["sealed"])

			if health["sealed"] == false {
				isUnsealed = true
			}
		} else {
			fmt.Printf("  🟢 Reachable (HTTP %d), but couldn't parse health JSON.\n", resp.StatusCode)
		}

		// ==========================================
		// 3. CONFIGURATION LAYER (Deep API Check)
		// ==========================================
		if isUnsealed {
			fmt.Println("\n  [ Active Integrations ]")
			vaultClient, err := GetHealthyClient()

			if err != nil {
				fmt.Println("  🟡 Could not authenticate to Vault. Ensure VAULT_TOKEN is set.")
			} else {
				// Auth Methods
				auths, _ := vaultClient.Sys().ListAuth()
				fmt.Print("  Auth Mounts : ")
				activeAuths := []string{}
				for path := range auths {
					if path != "token/" {
						activeAuths = append(activeAuths, strings.TrimSuffix(path, "/"))
					}
				}
				if len(activeAuths) > 0 {
					fmt.Println(strings.Join(activeAuths, ", "))
				} else {
					fmt.Println("None")
				}

				// Secrets Engines
				mounts, _ := vaultClient.Sys().ListMounts()
				fmt.Print("  Secrets     : ")
				activeMounts := []string{}
				for path, mount := range mounts {
					if mount.Type != "system" && mount.Type != "cubbyhole" && mount.Type != "identity" {
						activeMounts = append(activeMounts, fmt.Sprintf("%s (%s)", strings.TrimSuffix(path, "/"), mount.Type))
					}
				}
				if len(activeMounts) > 0 {
					fmt.Println(strings.Join(activeMounts, ", "))
				} else {
					fmt.Println("None")
				}

				// Audit Devices
				audits, _ := vaultClient.Sys().ListAudit()
				fmt.Print("  Audit Log   : ")
				activeAudits := []string{}
				for path, audit := range audits {
					activeAudits = append(activeAudits, fmt.Sprintf("%s (%s)", strings.TrimSuffix(path, "/"), audit.Type))
				}
				if len(activeAudits) > 0 {
					fmt.Println(strings.Join(activeAudits, ", "))
				} else {
					fmt.Println("None")
				}
			}
		} else {
			fmt.Println("\n  ⚪ Vault is sealed. Run 'vault operator unseal' to check internal configurations.")
		}

		fmt.Println("\n💡 Tip: Run 'hal vault <feature>' for a detailed micro-status check.")
	},
}

func init() {
	Cmd.AddCommand(vaultStatusCmd)
}
