package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var catalogCmd = &cobra.Command{
	Use:   "catalog",
	Short: "List all available HashiCorp products and features",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("=========================================================")
		fmt.Println("               🪐 HAL PRODUCT CATALOG               ")
		fmt.Println("=========================================================")

		fmt.Println()
		fmt.Println("🛡️  SECURITY & ACCESS")
		fmt.Println("   - vault     (Core + OIDC, JWT, K8s, LDAP, Audit)")
		fmt.Println("   - boundary  (Control Plane + MariaDB & SSH Targets)")

		fmt.Println()
		fmt.Println("🏗️  INFRASTRUCTURE & ORCHESTRATION")
		fmt.Println("   - nomad     (Ubuntu VM cluster + --join-consul)")
		fmt.Println("   - consul    (Standalone Control Plane)")
		fmt.Println("   - terraform (Local FDO Enterprise Sandbox)")

		fmt.Println()
		fmt.Println("📊 TELEMETRY & OBSERVABILITY")
		fmt.Println("   - obs       (Prometheus, Loki, Grafana, Promtail)")

		fmt.Println()
		fmt.Println("=========================================================")
		fmt.Println("💡 Tip: Run 'hal <product>' to view its Smart Status dashboard!")
		fmt.Println("   Or run 'hal <product> deploy' to spin it up.")
		fmt.Println("=========================================================")
	},
}

func init() {
	rootCmd.AddCommand(catalogCmd)
}
