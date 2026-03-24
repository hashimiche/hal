package terraform

import (
	"fmt"
	"os/exec"
	"strings"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Display the health and status of the local Terraform Enterprise environment",
	Run: func(cmd *cobra.Command, args []string) {

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		fmt.Println("📊 Analyzing Terraform Enterprise (FDO) environment...")

		// Define our topology
		components := []struct {
			Name      string
			Container string
			Icon      string
		}{
			{"TFE Core (Application)", "hal-tfe", ""},
			{"Database (Postgres)", "hal-tfe-db", " "},
			{"Cache (Redis)", "hal-tfe-redis", ""},
			{"Object Storage (MinIO)", "hal-tfe-minio", " "},
		}

		fmt.Printf("%-30s %s\n", "  Component", "Status")
		fmt.Println("---------------------------------------------------------")

		allRunning := true
		someExist := false

		// Loop through each component to check its real state in the engine
		for _, c := range components {
			out, err := exec.Command(engine, "inspect", "-f", "{{.State.Status}}", c.Container).CombinedOutput()
			status := strings.TrimSpace(string(out))

			// If the container doesn't exist at all
			if err != nil || strings.Contains(status, "No such object") || strings.Contains(status, "no such container") {
				fmt.Printf("%s %-27s 👻 Not deployed\n", c.Icon, c.Name)
				allRunning = false
			} else {
				someExist = true
				if status == "running" {
					fmt.Printf("%s %-27s ✅ Running\n", c.Icon, c.Name)
				} else {
					fmt.Printf("%s %-27s ❌ %s\n", c.Icon, c.Name, strings.ToUpper(status))
					allRunning = false
				}
			}
		}

		fmt.Println("---------------------------------------------------------")

		// Health assessment
		if allRunning {
			fmt.Println("\n All systems green! The TFE flavor is fully operational.")
			fmt.Println("🔗 UI Address: https://tfe.localhost")
		} else if someExist {
			fmt.Println("\n⚠️  Environment is partially deployed or some services are stopped.")
			fmt.Println("💡 Use 'hal terraform deploy' to restart missing services.")
		} else {
			fmt.Println("\n🕳️  No TFE instance detected on this machine.")
			fmt.Println("💡 Start an environment with 'hal terraform deploy'.")
		}
	},
}

func init() {
	Cmd.AddCommand(statusCmd)
}
