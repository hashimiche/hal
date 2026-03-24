package observability

import (
	"fmt"

	"github.com/spf13/cobra"
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy the Grafana/Prometheus/Loki telemetry stack",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("⚙️ Deploying Observability Stack...")
		// TODO: Spin up Prometheus container attached to hal-net
		// TODO: Spin up Loki container for Vault/Nomad logs
		// TODO: Spin up Grafana container and provision default HAL dashboards
		fmt.Println("✅ Observability stack ready at http://grafana.localhost:3000")
	},
}

func init() {
	Cmd.AddCommand(deployCmd)
}
