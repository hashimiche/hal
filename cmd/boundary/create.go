package boundary

import (
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"hal/internal/global"
	"hal/internal/ui"

	"github.com/spf13/cobra"
)

var (
	boundaryVersion    string
	boundaryImage      string
	pgVersion          string
	pgImage            string
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

		ui.LogoStart("boundary")
		defer ui.LogoStop()
		step := func(cols int, format string, args ...any) {
			ui.LogoStep(format, args...)
			ui.LogoAdvance(cols)
		}

		if boundaryUpdate {
			step(1, "Reconciling existing Boundary Control Plane (update)")
			_ = exec.Command(engine, "rm", "-f", boundaryContainer, boundaryBackendContainer).Run()
		}

		step(1, "Deploying Boundary %s (Postgres %s)", boundaryVersion, pgVersion)

		global.EnsureNetwork(engine)

		ui.LogoStep("Provisioning Control Plane database")
		ui.LogoCreep(2 * time.Second)
		backendArgs := []string{
			"run", "-d",
			"--name", boundaryBackendContainer,
			"--network", global.HalNetName,
			"-e", "POSTGRES_USER=" + boundaryDBUser,
			"-e", "POSTGRES_PASSWORD=" + boundaryDBPassword,
			"-e", "POSTGRES_DB=" + boundaryDBName,
			fmt.Sprintf("%s:%s", pgImage, pgVersion),
		}

		if global.DryRun {
			ui.LogoStop()
			fmt.Printf("[DRY RUN] Would execute: %s %s\n", engine, strings.Join(backendArgs, " "))
			return
		}

		_ = exec.Command(engine, backendArgs...).Run()
		time.Sleep(3 * time.Second)

		step(2, "Booting Boundary Controller & Worker")
		boundaryArgs := []string{
			"run", "-d",
			"--name", boundaryContainer,
			"--network", global.HalNetName,
			"-p", fmt.Sprintf("%d:%d", boundaryAPIPort, boundaryAPIPort),
			"-p", fmt.Sprintf("%d:%d", boundaryClusterPort, boundaryClusterPort),
			"-p", fmt.Sprintf("%d:%d", boundaryProxyPort, boundaryProxyPort),
		}

		if boundaryJoinConsul {
			boundaryArgs = append(boundaryArgs, "-e", "CONSUL_HTTP_ADDR=http://hal-consul:8500")
		}

		boundaryArgs = append(boundaryArgs,
			fmt.Sprintf("%s:%s", boundaryImage, boundaryVersion),
			"boundary", "dev",
			fmt.Sprintf("-api-listen-address=0.0.0.0:%d", boundaryAPIPort),
			fmt.Sprintf("-proxy-listen-address=0.0.0.0:%d", boundaryProxyPort),
			fmt.Sprintf("-database-url=postgresql://%s:%s@%s:5432/%s?sslmode=disable", boundaryDBUser, boundaryDBPassword, boundaryBackendContainer, boundaryDBName),
		)

		out, err := exec.Command(engine, boundaryArgs...).CombinedOutput()
		if err != nil {
			ui.LogoStop()
			if strings.Contains(string(out), "AlreadyExists") || strings.Contains(string(out), "already in use") {
				fmt.Println("⚠️  Boundary already exists. Use '--update' to reconcile it.")
				return
			}
			fmt.Printf("❌ Failed to start Boundary: %s\n", string(out))
			return
		}

		ui.LogoStep("Waiting for Boundary to initialize")
		ui.LogoCreep(2 * time.Second)

		if err := waitForService("Boundary", boundaryLocalAPIURL, 30); err != nil {
			ui.LogoStop()
			handleDockerFailure(boundaryContainer, engine)
			return
		}

		ui.LogoStop()
		global.RefreshHalHealth(engine)
		ui.Success("Boundary Controller & Worker are up!")
		ui.Section("Access")
		ui.Field("UI", boundaryUIURL)
		ui.Field("Login", fmt.Sprintf("%s / %s", boundaryAdminUsername, boundaryAdminPassword))
		if boundaryJoinConsul {
			ui.Item("🟢 Tethered to the global Consul Control Plane")
		}
		ui.Section("Next: add targets")
		ui.Item("hal boundary mariadb enable")
		ui.Item("hal boundary mariadb enable --with-vault  (dynamic creds; Vault must be running)")
		ui.Item("hal boundary ssh enable")
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
	cmd.Flags().StringVarP(&boundaryVersion, "boundary-tag", "v", defaultBoundaryTag, "Boundary container image tag")
	cmd.Flags().StringVar(&boundaryImage, "boundary-image", defaultBoundaryImage, "Boundary container image name")
	cmd.Flags().StringVar(&pgVersion, "boundary-pg-tag", defaultBoundaryPGTag, "PostgreSQL image tag for Boundary backend")
	cmd.Flags().StringVar(&pgImage, "boundary-pg-image", defaultBoundaryPGImage, "PostgreSQL image name for Boundary backend")
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
