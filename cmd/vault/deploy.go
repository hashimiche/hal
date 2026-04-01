package vault

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var (
	vaultVersion    string
	vaultEdition    string // ce or ent
	vaultForce      bool
	vaultJoinConsul bool
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy a local Vault instance",
	Run: func(cmd *cobra.Command, args []string) {

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		// ==========================================
		// PRE-FLIGHT CHECKS
		// ==========================================
		if vaultJoinConsul && !global.IsConsulRunning(engine) {
			fmt.Println("❌ Error: --join-consul was requested, but the global Consul brain is not running.")
			fmt.Println("   💡 Run 'hal consul deploy' first to bring the Control Plane online.")
			return
		}

		// THE NEW LICENSE CHECK
		if vaultEdition == "ent" || vaultEdition == "enterprise" {
			if os.Getenv("VAULT_LICENSE") == "" {
				fmt.Println("❌ Error: Vault Enterprise requested but VAULT_LICENSE environment variable is not set.")
				fmt.Println("   💡 Please export your license key first: export VAULT_LICENSE='your_license_string'")
				return
			}
		}

		if vaultForce {
			if global.Debug {
				fmt.Println("[DEBUG] --force flag detected. Purging existing Vault and Volumes...")
			}
			_ = exec.Command(engine, "rm", "-f", "hal-vault").Run()
			_ = exec.Command(engine, "volume", "rm", "-f", "hal-vault-logs").Run() // Purge des anciens logs
		}

		// Determine the Image Repository and Version based on Edition
		imageRepo := "hashicorp/vault"
		actualVersion := vaultVersion

		if vaultEdition == "ent" || vaultEdition == "enterprise" {
			imageRepo = "hashicorp/vault-enterprise"

			// If the user didn't explicitly specify a version, give them the Enterprise default
			if !cmd.Flags().Changed("version") {
				actualVersion = "1.21-ent"
			}
		}

		fmt.Printf("🚀 Deploying Vault %s (%s) via %s...\n", actualVersion, strings.ToUpper(vaultEdition), engine)

		// 1. Ensure the global HAL network exists
		global.EnsureNetwork(engine)

		// NOUVEAU : Correction des permissions du volume d'audit pour l'utilisateur Vault (UID 100)
		fmt.Println("⚙️  Preparing shared audit volume permissions...")
		_ = exec.Command(engine, "run", "--rm", "-v", "hal-vault-logs:/vault/logs", "alpine", "chown", "-R", "100:1000", "/vault/logs").Run()

		// 2. Build the Docker run arguments
		vaultArgs := []string{
			"run", "-d",
			"--name", "hal-vault",
			"--network", "hal-net",
			"--cap-add", "IPC_LOCK",
			"-p", "8200:8200",
			"-v", "hal-vault-logs:/vault/logs",
		}

		// Inject the Enterprise License (we already know it exists thanks to the pre-flight check)
		if vaultEdition == "ent" || vaultEdition == "enterprise" {
			fmt.Println("   🔐 Injecting VAULT_LICENSE into container...")
			vaultArgs = append(vaultArgs, "-e", fmt.Sprintf("VAULT_LICENSE=%s", os.Getenv("VAULT_LICENSE")))
		}

		// Inject the Consul Tether
		if vaultJoinConsul {
			fmt.Println("   🤝 --join-consul detected! Tethering Vault to the global HAL Consul...")
			vaultArgs = append(vaultArgs, "-e", "CONSUL_HTTP_ADDR=http://hal-consul:8500")
		}

		// Append the image and the Vault Dev mode commands
		vaultArgs = append(vaultArgs,
			fmt.Sprintf("%s:%s", imageRepo, actualVersion),
			"server", "-dev", "-dev-listen-address=0.0.0.0:8200", "-dev-root-token-id=root",
		)

		if global.DryRun {
			fmt.Printf("[DRY RUN] Would execute: %s %s\n", engine, strings.Join(vaultArgs, " "))
			return
		}

		out, err := exec.Command(engine, vaultArgs...).CombinedOutput()
		if err != nil {
			if strings.Contains(string(out), "AlreadyExists") || strings.Contains(string(out), "already in use") {
				fmt.Println("⚠️  Vault is already deployed! Use '--force' to redeploy.")
				return
			}
			fmt.Printf("❌ Failed to start Vault: %s\n", string(out))
			return
		}

		// 3. THE HEALTH CHECK PHASE
		fmt.Println("⏳ Waiting for Vault to initialize...")

		if err := waitForService("Vault", "http://vault.localhost:8200/v1/sys/health", 30); err != nil {
			handleDockerFailure("hal-vault", engine)
			return
		}

		fmt.Println("✅ Vault is up and running in Dev mode!")
		fmt.Printf("   🏗️  Edition: %s\n", strings.ToUpper(vaultEdition))
		fmt.Println("   🔗 UI Address: http://vault.localhost:8200")
		fmt.Println("   🔑 Root Token: root")

		if vaultJoinConsul {
			fmt.Println("   🟢 Vault is successfully tethered to the global Consul Control Plane!")
		}

		if err := global.UpsertObsPromTargetIfRunning(engine, "vault", []string{"hal-vault:8200"}); err != nil {
			fmt.Printf("⚠️  Observability target registration skipped: %v\n", err)
		}

		fmt.Println("\n💡 Tip: Export your environment variables to use your local CLI:")
		fmt.Println("   export VAULT_ADDR='http://vault.localhost:8200'")
		fmt.Println("   export VAULT_TOKEN='root'")
	},
}

// waitForService pings the URL every 2 seconds until it gets an HTTP 200 or hits the timeout limit
func waitForService(name string, url string, maxRetries int) error {
	client := http.Client{Timeout: 2 * time.Second}

	for i := 0; i < maxRetries; i++ {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout waiting for %s at %s", name, url)
}

// handleDockerFailure pulls the container logs directly to diagnose the crash
func handleDockerFailure(container string, engine string) {
	fmt.Printf("❌ %s failed to start or become healthy.\n", container)
	fmt.Println("📜 Fetching recent container logs...")

	out, _ := exec.Command(engine, "logs", "--tail", "20", container).CombinedOutput()
	logStr := strings.TrimSpace(string(out))

	if logStr != "" {
		fmt.Println("----------------- CONTAINER LOGS -----------------")
		fmt.Println(logStr)
		fmt.Println("--------------------------------------------------")
	} else {
		fmt.Println("(No logs found)")
	}
	fmt.Println("⚠️  Deployment halted. Run 'hal vault destroy' to clean up the broken resources.")
}

func init() {
	deployCmd.Flags().StringVarP(&vaultVersion, "version", "v", "1.21", "Vault version to deploy")
	deployCmd.Flags().StringVarP(&vaultEdition, "edition", "e", "ce", "Vault edition to deploy: 'ce' (Community) or 'ent' (Enterprise)")
	deployCmd.Flags().BoolVarP(&vaultForce, "force", "f", false, "Force redeploy")

	// The unified global join flag
	deployCmd.Flags().BoolVarP(&vaultJoinConsul, "join-consul", "c", false, "Tether Vault to the global HAL Consul instance")

	Cmd.AddCommand(deployCmd)
}
