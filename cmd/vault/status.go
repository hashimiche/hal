package vault

import (
	"encoding/json"
	"fmt"
	"hal/internal/global"
	"net/http"
	"os/exec"
	"strings"
	"time"

	vault "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
)

var vaultStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the status of the local Vault container and its configurations",
	Run: func(cmd *cobra.Command, args []string) {
		// ==========================================
		// 1. INFRASTRUCTURE LAYER (Container Check)
		// ==========================================
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Println("❌ Error: Container engine not running.")
			return
		}

		if global.Debug {
			fmt.Printf("[DEBUG] Checking %s for container 'hal-vault'...\n", engine)
		}

		out, err := exec.Command(engine, "ps", "-a", "-q", "-f", "name=hal-vault").Output()
		if err != nil || strings.TrimSpace(string(out)) == "" {
			fmt.Println("🔴 Vault is not running. (No container named 'hal-vault' found)")
			return
		}

		stateOut, err := exec.Command(engine, "inspect", "-f", "{{.State.Status}}", "hal-vault").Output()
		if err != nil {
			fmt.Printf("⚠️  Could not determine container state: %v\n", err)
			return
		}

		state := strings.TrimSpace(string(stateOut))

		if state != "running" {
			fmt.Printf("🔴 Vault container exists, but is currently: %s\n", strings.ToUpper(state))
			fmt.Println("📜 Fetching crash logs...")

			logsOut, _ := exec.Command(engine, "logs", "--tail", "10", "hal-vault").CombinedOutput()
			logStr := strings.TrimSpace(string(logsOut))

			if logStr != "" {
				fmt.Println("----------------- ENGINE LOGS -----------------")
				fmt.Println(logStr)
				fmt.Println("-----------------------------------------------")

				if strings.Contains(strings.ToLower(logStr), "license") {
					fmt.Println("💡 HAL Tip: Looks like a Vault Enterprise license rejection.")

					imageOut, _ := exec.Command(engine, "inspect", "-f", "{{.Config.Image}}", "hal-vault").Output()
					parts := strings.Split(strings.TrimSpace(string(imageOut)), ":")
					versionFlag := ""
					if len(parts) == 2 {
						versionFlag = fmt.Sprintf(" --version %s", parts[1])
					}
					fmt.Printf("   Ensure $VAULT_LICENSE is valid, then run: hal vault deploy --edition ent%s --force\n", versionFlag)
				}
			} else {
				fmt.Println("(No logs found in container)")
			}
			return
		}

		// ==========================================
		// 2. CORE APP LAYER (API Health Ping)
		// ==========================================
		fmt.Printf("🟢 Container 'hal-vault' is running via %s.\n", engine)

		client := http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get("http://127.0.0.1:8200/v1/sys/health")
		if err != nil {
			fmt.Printf("⚠️  Container is up, but API is unreachable: %v\n", err)
			fmt.Println("   (Vault might still be booting up. Try again in a few seconds.)")
			return
		}
		defer resp.Body.Close()

		var health map[string]interface{}
		isUnsealed := false

		if err := json.NewDecoder(resp.Body).Decode(&health); err == nil {
			fmt.Println("\n✅ Vault API is awake and responding!")
			fmt.Printf("   Version:     %v\n", health["version"])
			fmt.Printf("   Initialized: %v\n", health["initialized"])
			fmt.Printf("   Sealed:      %v\n", health["sealed"])

			if health["sealed"] == false {
				isUnsealed = true
			}
		} else {
			fmt.Printf("✅ Vault API is reachable (HTTP %d), but couldn't parse health JSON.\n", resp.StatusCode)
			return
		}

		// ==========================================
		// 3. CONFIGURATION LAYER (Deep API Check)
		// ==========================================
		// Only run this if Vault is unsealed and ready
		if isUnsealed {
			fmt.Println("\n--- Auth Methods ---")
			vaultClient, err := GetHealthyClient()
			if err != nil {
				fmt.Println("⚠️  Could not authenticate to read configurations. Make sure VAULT_TOKEN is set.")
				return
			}

			auths, _ := vaultClient.Sys().ListAuth()
			checkAuthStatus("JWT/OIDC", "jwt/", auths)
			checkAuthStatus("OIDC (Standalone)", "oidc/", auths)
			checkAuthStatus("Kubernetes", "kubernetes/", auths)
			checkAuthStatus("LDAP", "ldap/", auths)

			fmt.Println("\n--- Secrets Engines ---")
			mounts, _ := vaultClient.Sys().ListMounts()
			checkMountStatus("Database (MariaDB)", "database/", mounts)
			checkMountStatus("PKI", "pki/", mounts)
			checkMountStatus("KV-V2", "secret/", mounts)

			// Let's also check your audit devices!
			fmt.Println("\n--- Audit Devices ---")
			audits, _ := vaultClient.Sys().ListAudit()
			if len(audits) == 0 {
				fmt.Println("  ❌ No audit devices configured")
			} else {
				for path, audit := range audits {
					fmt.Printf("  ✅ %-20s : %s\n", strings.TrimSuffix(path, "/"), audit.Type)
				}
			}
			fmt.Println("")
		} else {
			fmt.Println("\n🔴 Vault is sealed. Run 'vault operator unseal' to check configurations.")
		}
	},
}

// Helpers
func checkAuthStatus(name, path string, auths map[string]*vault.AuthMount) {
	if _, exists := auths[path]; exists {
		fmt.Printf("  ✅ %-20s : ready\n", name)
	} else {
		fmt.Printf("  ❌ %-20s : not configured\n", name)
	}
}

func checkMountStatus(name, path string, mounts map[string]*vault.MountOutput) {
	if _, exists := mounts[path]; exists {
		fmt.Printf("  ✅ %-20s : ready\n", name)
	} else {
		fmt.Printf("  ❌ %-20s : not configured\n", name)
	}
}

func init() {
	Cmd.AddCommand(vaultStatusCmd)
}
