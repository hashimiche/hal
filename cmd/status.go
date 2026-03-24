package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the global status of all HAL deployments",
	Long:  `Displays a high-level overview of which HashiCorp products are currently running locally.`,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		if debug {
			fmt.Println("[DEBUG] Reading global state to determine status...")
		}

		fmt.Println("HAL Global Deployment Status")
		fmt.Println("===============================")

		// Check Consul (The Control Plane)
		if checkContainer(engine, "hal-consul") {
			fmt.Println("🟢 Consul:   Running (http://consul.localhost:8500)")
		} else {
			fmt.Println("🔴 Consul:   Not Deployed")
		}

		// Check Vault
		if checkContainer(engine, "hal-vault") {
			fmt.Println("🟢 Vault:    Running (http://vault.localhost:8200)")
		} else {
			fmt.Println("🔴 Vault:    Not Deployed")
		}

		// Check Nomad (Multipass VM)
		if checkMultipass("hal-nomad") {
			fmt.Println("🟢 Nomad:    Running (Multipass VM)")
		} else {
			fmt.Println("🔴 Nomad:    Not Deployed")
		}

		// Check Boundary
		if checkContainer(engine, "hal-boundary") {
			fmt.Println("🟢 Boundary: Running (http://boundary.localhost:9200)")
		} else {
			fmt.Println("🔴 Boundary: Not Deployed")
		}

		// Check Terraform Enterprise
		if checkContainer(engine, "hal-tfe") {
			fmt.Println("🟢 TFE:      Running (https://tfe.localhost)")
		} else {
			fmt.Println("🔴 TFE:      Not Deployed")
		}

		fmt.Println("===============================")
		fmt.Println("💡 Tip: Run 'hal <product> status' for detailed component health.")
	},
}

// Helper to silently check if a container exists and is running
func checkContainer(engine, name string) bool {
	out, err := exec.Command(engine, "ps", "-q", "-f", fmt.Sprintf("name=^%s$", name)).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

// Helper to silently check if a Multipass VM is running
func checkMultipass(name string) bool {
	out, err := exec.Command("multipass", "info", name, "--format", "csv").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Running")
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
