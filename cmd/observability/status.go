package observability

import (
	"fmt"
	"os/exec"
	"strings"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Display the health and status of the Observability stack",
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("⚪ Error: %v\n", err)
			return
		}

		fmt.Println("🔍 Checking Observability (PLG) Status...")
		fmt.Printf("Engine: %s\n", engine)
		fmt.Println()

		components := []struct {
			Name      string
			Container string
		}{
			{"Prometheus", "hal-prometheus"},
			{"Loki", "hal-loki"},
			{"Promtail", "hal-promtail"},
			{"Grafana", "hal-grafana"},
		}

		allRunning := true
		someExist := false

		for _, c := range components {
			out, err := exec.Command(engine, "inspect", "-f", "{{.State.Status}}", c.Container).Output()
			status := strings.TrimSpace(string(out))

			if err != nil {
				fmt.Printf("  ⚪ %-15s : Down\n", c.Name)
				allRunning = false
			} else if status == "running" {
				someExist = true
				fmt.Printf("  🟢 %-15s : Up\n", c.Name)
			} else {
				someExist = true
				allRunning = false
				fmt.Printf("  🟡 %-15s : %s\n", c.Name, strings.ToUpper(status))
			}
		}

		fmt.Println("\n💡 Tips:")
		if !someExist {
			fmt.Println("   To deploy the full PLG stack, run:")
			fmt.Println("   hal obs create")
		} else if allRunning {
			fmt.Println("   All systems green. Stack is capturing telemetry.")
			fmt.Println("   🔗 Grafana UI: http://grafana.localhost:3000")
			fmt.Println("   🔗 Prometheus: http://prometheus.localhost:9090")
			fmt.Println("   🔗 Loki API:   http://loki.localhost:3100/ready")
		} else {
			fmt.Println("   Environment is partially degraded. To safely reset, run:")
			fmt.Println("   hal obs create --force")
		}
		fmt.Println("   Run 'hal obs status' after changes to confirm all PLG components are healthy.")
	},
}

func init() {
	Cmd.AddCommand(statusCmd)
}
