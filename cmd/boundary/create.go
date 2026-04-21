package boundary

import (
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var (
	boundaryVersion    string
	pgVersion          string
	boundaryUpdate     bool
	boundaryJoinConsul bool
)

var deployCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a local Boundary Control Plane (Controller + Backend DB)",
	Run: func(cmd *cobra.Command, args []string) {

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		if boundaryJoinConsul && !global.IsConsulRunning(engine) {
			fmt.Println("❌ Error: --join-consul was requested, but the global Consul brain is not running.")
			fmt.Println("   💡 Run 'hal consul create' first to bring the Control Plane online.")
			return
		}

		if boundaryUpdate {
			fmt.Println("♻️  Update requested. Reconciling existing Boundary Control Plane...")
			_ = exec.Command(engine, "rm", "-f", "hal-boundary", "hal-boundary-backend").Run()
		}

		fmt.Printf("🚀 Deploying Boundary %s (with Postgres %s) via %s...\n", boundaryVersion, pgVersion, engine)

		global.EnsureNetwork(engine)

		fmt.Printf("⚙️  Provisioning Boundary Control Plane Database (postgres:%s-alpine)...\n", pgVersion)
		backendArgs := []string{
			"run", "-d",
			"--name", "hal-boundary-backend",
			"--network", "hal-net",
			"-e", "POSTGRES_USER=boundary",
			"-e", "POSTGRES_PASSWORD=boundary",
			"-e", "POSTGRES_DB=boundary",
			fmt.Sprintf("postgres:%s-alpine", pgVersion),
		}

		if global.DryRun {
			fmt.Printf("[DRY RUN] Would execute: %s %s\n", engine, strings.Join(backendArgs, " "))
			return
		}

		_ = exec.Command(engine, backendArgs...).Run()
		time.Sleep(3 * time.Second)

		fmt.Println("⚙️  Booting Boundary Controller & Worker...")
		boundaryArgs := []string{
			"run", "-d",
			"--name", "hal-boundary",
			"--network", "hal-net",
			"-p", "9200:9200",
			"-p", "9201:9201",
			"-p", "9202:9202",
		}

		if boundaryJoinConsul {
			fmt.Println("   🤝 --join-consul detected! Tethering Boundary to the global HAL Consul...")
			boundaryArgs = append(boundaryArgs, "-e", "CONSUL_HTTP_ADDR=http://hal-consul:8500")
		}

		boundaryArgs = append(boundaryArgs,
			fmt.Sprintf("hashicorp/boundary:%s", boundaryVersion),
			"boundary", "dev",
			"-api-listen-address=0.0.0.0:9200",
			"-proxy-listen-address=0.0.0.0:9202",
			"-database-url=postgresql://boundary:boundary@hal-boundary-backend:5432/boundary?sslmode=disable",
		)

		out, err := exec.Command(engine, boundaryArgs...).CombinedOutput()
		if err != nil {
			if strings.Contains(string(out), "AlreadyExists") || strings.Contains(string(out), "already in use") {
				fmt.Println("⚠️  Boundary already exists. Use '--update' to reconcile it.")
				return
			}
			fmt.Printf("❌ Failed to start Boundary: %s\n", string(out))
			return
		}

		fmt.Println("⏳ Waiting for Boundary to initialize (this can take 10-15 seconds)...")

		if err := waitForService("Boundary", "http://127.0.0.1:9200", 30); err != nil {
			handleDockerFailure("hal-boundary", engine)
			return
		}

		fmt.Println()
		fmt.Println("✅ Boundary Controller & Worker are up!")
		fmt.Println("---------------------------------------------------------")
		fmt.Println("   🔗 UI Address: http://boundary.localhost:9200")
		fmt.Println("   👤 Login:      admin / password")
		if boundaryJoinConsul {
			fmt.Println("   🟢 Tethered:   Global Consul Control Plane")
		}
		fmt.Println("---------------------------------------------------------")
		fmt.Println("💡 Next Step: Deploy some targets to connect to!")
		fmt.Println("   hal boundary mariadb enable")
		fmt.Println("   hal boundary mariadb enable --with-vault (for dynamic credentials, Vault must be running)")
		fmt.Println("   hal boundary ssh enable")
	},
}

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
	fmt.Println("⚠️  Deployment halted. Run 'hal boundary delete' to clean up the broken resources.")
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Reconcile an existing Boundary deployment",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		boundaryUpdate = true
		deployCmd.Run(cmd, args)
	},
}

func bindLifecycleFlags(cmd *cobra.Command, includeUpdate bool) {
	cmd.Flags().StringVarP(&boundaryVersion, "version", "v", "0.15.2", "Boundary version to deploy")
	cmd.Flags().StringVar(&pgVersion, "pg-version", "16", "PostgreSQL version for Boundary backend")
	if includeUpdate {
		cmd.Flags().BoolVarP(&boundaryUpdate, "update", "u", false, "Reconcile an existing Boundary deployment in place")
	}
	cmd.Flags().BoolVarP(&boundaryJoinConsul, "join-consul", "c", false, "Tether Boundary to the global HAL Consul instance")
}

func init() {
	bindLifecycleFlags(deployCmd, true)
	bindLifecycleFlags(updateCmd, false)
	Cmd.AddCommand(deployCmd)
	Cmd.AddCommand(updateCmd)
}
