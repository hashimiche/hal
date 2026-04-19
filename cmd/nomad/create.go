package nomad

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
	nomadVersion      string
	nomadUbuntuImage  string
	nomadCPUs         string
	nomadMem          string
	nomadJoinConsul   bool // The new unified Control Plane flag
	nomadUpdate       bool
	nomadConfigureObs bool
)

var deployCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a local Nomad cluster via Multipass",
	Run: func(cmd *cobra.Command, args []string) {
		if nomadConfigureObs {
			if global.DryRun {
				fmt.Println("[DRY RUN] Would refresh Nomad Prometheus target and Grafana dashboard artifacts")
				return
			}
			if !global.MultipassInstanceExists("hal-nomad") {
				fmt.Println("❌ Nomad VM is not present. Deploy it first before configuring observability artifacts.")
				fmt.Println("   💡 Run 'hal nomad create' and then retry with '--configure-obs' if needed.")
				return
			}
			engine, err := global.DetectEngine()
			if err != nil {
				fmt.Printf("❌ Error: %v\n", err)
				return
			}
			if !global.IsObsReady(engine) {
				fmt.Printf("❌ Observability stack is not ready. Missing: %s\n", strings.Join(global.ObsMissingComponents(engine), ", "))
				fmt.Println("   💡 Run 'hal obs create' first, then retry '--configure-obs'.")
				return
			}

			ipOut, _ := exec.Command("multipass", "info", "hal-nomad", "--format", "csv").Output()
			ip := extractMultipassIP(string(ipOut))
			fmt.Println("🩺 Configuring observability artifacts for Nomad...")
			for _, warning := range global.RegisterObsArtifacts("nomad", []string{fmt.Sprintf("%s:4646", ip)}) {
				fmt.Printf("⚠️  %s\n", warning)
			}
			fmt.Println("✅ Nomad observability artifacts refreshed.")
			return
		}

		if err := exec.Command("multipass", "version").Run(); err != nil {
			fmt.Println("❌ Error: Multipass is not installed or not running.")
			return
		}

		// PRE-FLIGHT CHECK
		if nomadJoinConsul {
			engine, err := global.DetectEngine()
			if err != nil || !global.IsConsulRunning(engine) {
				fmt.Println("❌ Error: --join-consul was requested, but the global Consul brain is not running.")
				fmt.Println("   💡 Run 'hal consul create' first to bring the Control Plane online.")
				return
			}
		}

		if nomadUpdate {
			if global.DryRun {
				fmt.Println("[DRY RUN] Would delete existing VM 'hal-nomad' and purge")
			}
			if global.Debug {
				fmt.Println("[DEBUG] --update detected. Purging existing VM 'hal-nomad' for reconciliation...")
			}
			if !global.DryRun {
				_ = exec.Command("multipass", "delete", "hal-nomad").Run()
				_ = exec.Command("multipass", "purge").Run()
			}
		}

		if global.DryRun {
			fmt.Printf("[DRY RUN] Would launch Multipass VM 'hal-nomad' (%s, %s CPU, %s RAM)\n", nomadUbuntuImage, nomadCPUs, nomadMem)
			fmt.Printf("[DRY RUN] Would install and start Nomad %s via systemd\n", nomadVersion)
			if nomadJoinConsul {
				fmt.Println("[DRY RUN] Would configure Nomad to join global Consul")
			}
			fmt.Println("[DRY RUN] Would wait for Nomad health endpoint")
			fmt.Println("[DRY RUN] Would refresh Nomad observability artifacts")
			return
		}

		fmt.Printf(" Deploying Nomad %s via Multipass (Ubuntu VM)...\n", nomadVersion)

		// 1. Launch the VM
		fmt.Println("📦 Provisioning Ubuntu VM (This takes a few seconds)...")
		launchArgs := []string{"launch", nomadUbuntuImage, "--name", "hal-nomad", "--cpus", nomadCPUs, "--mem", nomadMem}
		out, err := exec.Command("multipass", launchArgs...).CombinedOutput()
		if err != nil {
			if strings.Contains(string(out), "already exists") {
				fmt.Println("⚠️  VM 'hal-nomad' already exists. Use '--update' to reconcile it.")
				return
			}
			fmt.Printf("❌ Failed to launch VM: %v\nOutput: %s\n", err, string(out))
			return
		}

		// 2. Build the dynamic installation script
		fmt.Println("🔧 Installing binaries and configuring systemd services...")

		joinConsulStr := "false"
		if nomadJoinConsul {
			fmt.Println("   🤝 --join-consul detected! Tethering Nomad to the global HAL Consul...")
			joinConsulStr = "true"
		}

		installScript := fmt.Sprintf(`
			sudo apt-get update -yqq && sudo apt-get install unzip -yqq;
			
			ARCH=$(dpkg --print-architecture)
			curl -sLo nomad.zip https://releases.hashicorp.com/nomad/%s/nomad_%s_linux_${ARCH}.zip;
			unzip -o nomad.zip;
			sudo mv nomad /usr/local/bin/;
			
			# Dynamically fetch the Mac's Gateway IP from the Multipass bridge
			CONSUL_ENV=""
			if [ "%s" = "true" ]; then
				MAC_IP=$(ip route | awk '/default/ {print $3}')
				CONSUL_ENV="Environment=CONSUL_HTTP_ADDR=http://${MAC_IP}:8500"
			fi

			echo "[Unit]
			Description=Nomad
			After=network-online.target

			[Service]
			${CONSUL_ENV}
			ExecStart=/usr/local/bin/nomad agent -dev -bind 0.0.0.0
			Restart=always
			RestartSec=2

			[Install]
			WantedBy=multi-user.target" | sudo tee /etc/systemd/system/nomad.service;
			
			sudo systemctl daemon-reload;
			sudo systemctl enable --now nomad;
		`, nomadVersion, nomadVersion, joinConsulStr)

		execArgs := []string{"exec", "hal-nomad", "--", "bash", "-c", installScript}
		if out, err := exec.Command("multipass", execArgs...).CombinedOutput(); err != nil {
			fmt.Printf("❌ Failed to configure VM: %v\nOutput: %s\n", err, string(out))
			return
		}

		// 3. Fetch the VM's IP Address
		ipOut, _ := exec.Command("multipass", "info", "hal-nomad", "--format", "csv").Output()
		ip := extractMultipassIP(string(ipOut))

		// 4. THE HEALTH CHECK PHASE
		fmt.Println("⏳ Waiting for Nomad to become healthy...")
		if err := waitForService("Nomad", fmt.Sprintf("http://%s:4646/v1/status/leader", ip), 45); err != nil {
			handleServiceFailure("nomad")
			return
		}

		fmt.Println("\n✅ Environment is fully verified and ready!")
		fmt.Printf("   🔗 Nomad UI:  http://%s:4646\n", ip)

		for _, warning := range global.RegisterObsArtifacts("nomad", []string{fmt.Sprintf("%s:4646", ip)}) {
			fmt.Printf("⚠️  %s\n", warning)
		}

		if nomadJoinConsul {
			fmt.Println("   🟢 Nomad is successfully tethered to the global Consul Control Plane!")
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

// handleServiceFailure pulls the systemd logs directly from the VM to diagnose the crash
func handleServiceFailure(service string) {
	fmt.Printf("❌ %s service failed to start or become healthy.\n", strings.Title(service))
	fmt.Println("📜 Fetching recent systemd logs from the VM...")

	out, _ := exec.Command("multipass", "exec", "hal-nomad", "--", "journalctl", "-u", service, "-n", "15", "--no-pager").CombinedOutput()
	logStr := strings.TrimSpace(string(out))

	if logStr != "" {
		fmt.Println("----------------- SYSTEMD LOGS -----------------")
		fmt.Println(logStr)
		fmt.Println("------------------------------------------------")
	} else {
		fmt.Println("(No logs found)")
	}
	fmt.Println("⚠️  Deployment halted. Run 'hal nomad delete' to clean up the broken VM.")
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

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Reconcile an existing Nomad cluster",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		nomadUpdate = true
		deployCmd.Run(cmd, args)
	},
}

func bindLifecycleFlags(cmd *cobra.Command, includeUpdate bool) {
	cmd.Flags().StringVarP(&nomadVersion, "version", "v", "1.11.3", "Nomad version to install")
	cmd.Flags().StringVar(&nomadUbuntuImage, "ubuntu-image", "22.04", "Multipass image/channel used for the Nomad VM")
	cmd.Flags().StringVar(&nomadCPUs, "cpus", "2", "Number of CPUs for the VM")
	cmd.Flags().StringVar(&nomadMem, "mem", "2G", "Amount of RAM for the VM")
	if includeUpdate {
		cmd.Flags().BoolVarP(&nomadUpdate, "update", "u", false, "Reconcile an existing Nomad deployment in place")
	}
	cmd.Flags().BoolVar(&nomadConfigureObs, "configure-obs", false, "Refresh Prometheus target and Grafana dashboard artifacts without redeploying Nomad")
	cmd.Flags().BoolVarP(&nomadJoinConsul, "join-consul", "c", false, "Tether Nomad to the global HAL Consul instance")
}

func init() {
	bindLifecycleFlags(deployCmd, true)
	bindLifecycleFlags(updateCmd, false)
	Cmd.AddCommand(deployCmd)
	Cmd.AddCommand(updateCmd)
}
