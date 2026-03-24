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
	withDB             bool
	withSSH            bool
	boundaryForce      bool
	boundaryJoinConsul bool // NEW: The unified Control Plane flag
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy a local Boundary instance with an external Control Plane DB",
	Run: func(cmd *cobra.Command, args []string) {

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		// PRE-FLIGHT CHECK
		if boundaryJoinConsul && !global.IsConsulRunning(engine) {
			fmt.Println("❌ Error: --join-consul was requested, but the global Consul brain is not running.")
			fmt.Println("   💡 Run 'hal consul deploy' first to bring the Control Plane online.")
			return
		}

		if boundaryForce {
			if global.Debug {
				fmt.Println("[DEBUG] --force flag detected. Purging existing Boundary resources...")
			}
			_ = exec.Command(engine, "rm", "-f", "hal-boundary", "hal-boundary-backend", "hal-boundary-target-db").Run()
			if withSSH {
				_ = exec.Command("multipass", "delete", "hal-boundary-ssh").Run()
				_ = exec.Command("multipass", "purge").Run()
			}
		}

		fmt.Printf(" Deploying Boundary %s (with Postgres %s) via %s...\n", boundaryVersion, pgVersion, engine)

		// 1. Ensure the global HAL network exists
		global.EnsureNetwork(engine)

		// 2. Deploy the Control Plane Database (Backend)
		fmt.Printf("  Provisioning Boundary Control Plane Database (postgres:%s-alpine)...\n", pgVersion)
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
		time.Sleep(3 * time.Second) // Give Postgres a moment to wake up

		// 3. Boot Boundary Core
		fmt.Println("  Booting Boundary Controller & Worker...")
		boundaryArgs := []string{
			"run", "-d",
			"--name", "hal-boundary",
			"--network", "hal-net",
			"-p", "9200:9200",
			"-p", "9201:9201",
			"-p", "9202:9202",
		}

		// Inject the Consul Tether!
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
				fmt.Println("⚠️  Boundary is already deployed! Use '--force' to redeploy.")
				return
			}
			fmt.Printf("❌ Failed to start Boundary: %s\n", string(out))
			return
		}

		// 4. THE HEALTH CHECK PHASE
		fmt.Println("⏳ Waiting for Boundary to initialize (this can take 10-15 seconds)...")

		if err := waitForService("Boundary", "http://boundary.localhost:9200", 30); err != nil {
			handleDockerFailure("hal-boundary", engine)
			return
		}

		fmt.Println("✅ Boundary Controller & Worker are up!")
		fmt.Println("   🔗 UI Address: http://boundary.localhost:9200")
		fmt.Println("   👤 Login:      admin / password")

		if boundaryJoinConsul {
			fmt.Println("   🟢 Boundary is successfully tethered to the global Consul Control Plane!")
		}

		// 5. The Data Plane (Dummy Targets)
		if withDB {
			fmt.Printf("\n🎯 Deploying dummy Postgres Target Database (postgres:%s-alpine)...\n", pgVersion)
			dbArgs := []string{
				"run", "-d",
				"--name", "hal-boundary-target-db",
				"--network", "hal-net",
				"-p", "5432:5432",
				"-e", "POSTGRES_PASSWORD=targetpass",
				"-e", "POSTGRES_USER=admin",
				fmt.Sprintf("postgres:%s-alpine", pgVersion),
			}
			_, dbErr := exec.Command(engine, dbArgs...).CombinedOutput()
			if dbErr == nil {
				fmt.Println("   ✅ DB Target ready! (db.boundary.localhost:5432 | admin / targetpass)")
			} else {
				fmt.Println("   ❌ Failed to start Target Database container.")
			}
		}

		if withSSH {
			fmt.Println("\n Deploying dummy SSH Linux target via Multipass (Micro-VM)...")
			vmArgs := []string{"launch", "22.04", "--name", "hal-boundary-ssh", "--cpus", "1", "--mem", "512M"}
			_, vmErr := exec.Command("multipass", vmArgs...).CombinedOutput()
			if vmErr == nil {
				ipOut, _ := exec.Command("multipass", "info", "hal-boundary-ssh", "--format", "csv").Output()
				ip := extractMultipassIP(string(ipOut))
				fmt.Printf("   ✅ SSH Server ready! (IP: %s | Port: 22)\n", ip)
			} else {
				fmt.Println("   ❌ Failed to start SSH VM. (Might already exist)")
			}
		}
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
	fmt.Println("⚠️  Deployment halted. Run 'hal boundary destroy' to clean up the broken resources.")
}

func extractMultipassIP(csvData string) string {
	lines := strings.Split(csvData, "\n")
	if len(lines) > 1 {
		cols := strings.Split(lines[1], ",")
		if len(cols) > 2 {
			return cols[2]
		}
	}
	return "127.0.0.1"
}

func init() {
	deployCmd.Flags().StringVarP(&boundaryVersion, "version", "v", "0.21.1", "Boundary version to deploy")
	deployCmd.Flags().StringVar(&pgVersion, "pg-version", "17", "PostgreSQL version for Boundary backend and targets")

	deployCmd.Flags().BoolVar(&withDB, "with-db", false, "Deploy a dummy Postgres DB container as a target")
	deployCmd.Flags().BoolVar(&withSSH, "with-ssh", false, "Deploy a tiny Multipass Ubuntu VM as an SSH target")
	deployCmd.Flags().BoolVarP(&boundaryForce, "force", "f", false, "Force redeploy")

	// The unified global join flag
	deployCmd.Flags().BoolVarP(&boundaryJoinConsul, "join-consul", "c", false, "Tether Boundary to the global HAL Consul instance")

	Cmd.AddCommand(deployCmd)
}
