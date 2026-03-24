package vault

import (
	"encoding/json"
	"fmt"
	"hal/internal/global"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var vaultStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the status of the local Vault instance",
	Run: func(cmd *cobra.Command, args []string) {
		// 1. Check the engine
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Println("❌ Error: Container engine not running.")
			return
		}

		if global.Debug {
			fmt.Printf("[DEBUG] Checking %s for container 'hal-vault'...\n", engine)
		}

		// 2. Check if the container exists AT ALL (notice the -a flag)
		out, err := exec.Command(engine, "ps", "-a", "-q", "-f", "name=hal-vault").Output()
		if err != nil || strings.TrimSpace(string(out)) == "" {
			fmt.Println("🔴 Vault is not running. (No container named 'hal-vault' found)")
			return
		}

		// 3. Inspect the actual state of the container
		stateOut, err := exec.Command(engine, "inspect", "-f", "{{.State.Status}}", "hal-vault").Output()
		if err != nil {
			fmt.Printf("⚠️  Could not determine container state: %v\n", err)
			return
		}

		state := strings.TrimSpace(string(stateOut))

		// 4. Handle crashed/exited containers with Log Extraction
		if state != "running" {
			fmt.Printf("🔴 Vault container exists, but is currently: %s\n", strings.ToUpper(state))
			fmt.Println("📜 Fetching crash logs...")

			// Grab the last 10 lines of logs
			logsOut, _ := exec.Command(engine, "logs", "--tail", "10", "hal-vault").CombinedOutput()
			logStr := strings.TrimSpace(string(logsOut))

			if logStr != "" {
				fmt.Println("----------------- ENGINE LOGS -----------------")
				fmt.Println(logStr)
				fmt.Println("-----------------------------------------------")

				// Add a little HashiCorp empathy for known issues
				if strings.Contains(strings.ToLower(logStr), "license") {
					fmt.Println("💡 HAL Tip: Looks like a Vault Enterprise license rejection.")

					// DYNAMIC VERSION EXTRACTION
					// Ask the engine what image the container was using
					imageOut, _ := exec.Command(engine, "inspect", "-f", "{{.Config.Image}}", "hal-vault").Output()
					imageStr := strings.TrimSpace(string(imageOut))

					// Parse the version tag (e.g., hashicorp/vault-enterprise:1.15.2 -> 1.15.2)
					versionFlag := ""
					parts := strings.Split(imageStr, ":")
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

		// 5. If we made it here, the container is running. Proceed with API Ping.
		fmt.Printf("🟢 Container 'hal-vault' is running via %s.\n", engine)
		fmt.Println("🔍 Pinging Vault API (/v1/sys/health)...")

		client := http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get("http://127.0.0.1:8200/v1/sys/health")
		if err != nil {
			fmt.Printf("⚠️  Container is up, but API is unreachable: %v\n", err)
			fmt.Println("   (Vault might still be booting up. Try again in a few seconds.)")
			return
		}
		defer resp.Body.Close()

		var health map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&health); err == nil {
			fmt.Println("\n✅ Vault API is awake and responding!")
			fmt.Println("=====================================")
			fmt.Printf("   Version:     %v\n", health["version"])
			fmt.Printf("   Initialized: %v\n", health["initialized"])
			fmt.Printf("   Sealed:      %v\n", health["sealed"])

			if health["sealed"] == false {
				fmt.Println("   Status:      Ready for requests ")
			}
			fmt.Println("=====================================")
		} else {
			fmt.Printf("✅ Vault API is reachable (HTTP %d), but couldn't parse health JSON.\n", resp.StatusCode)
		}
	},
}

func init() {
	Cmd.AddCommand(vaultStatusCmd)
}
