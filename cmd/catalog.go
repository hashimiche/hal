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
		fmt.Println("   - vault     Core Vault + OIDC, JWT, K8s (VSO/CSI), LDAP, Audit, Database")
		fmt.Println("               obs: Prometheus targets + Grafana dashboards")
		fmt.Println("   - boundary  Control Plane + MariaDB & SSH Targets")
		fmt.Println("               obs: Prometheus targets + Grafana dashboards")

		fmt.Println()
		fmt.Println("🏗️  INFRASTRUCTURE & ORCHESTRATION")
		fmt.Println("   - nomad     Ubuntu VM cluster via Multipass + job workloads (--join-consul)")
		fmt.Println("               obs: Prometheus targets + Grafana dashboards")
		fmt.Println("   - consul    Standalone Control Plane")
		fmt.Println("               obs: Prometheus targets + Grafana dashboards")
		fmt.Println("   - terraform Local TFE (FDO) Sandbox + Agent Pool, API Workflow, VCS Workflow")
		fmt.Println("               obs: Prometheus targets + Grafana dashboards")

		fmt.Println()
		fmt.Println("📊 TELEMETRY & OBSERVABILITY")
		fmt.Println("   - obs       Prometheus, Loki, Grafana, Promtail (shared PLG stack)")
		fmt.Println("               Per-product obs artifacts: hal <product> obs create")

		fmt.Println()
		fmt.Println("🤖 AI & TOOLING")
		fmt.Println("   - mcp       HAL MCP server (stdio or streamable-HTTP transport)")
		fmt.Println("               Exposes HAL status tools to AI clients (e.g. Copilot, Claude)")
		fmt.Println("   - plus      HAL Plus web UI with Ollama/LLM integration")
		fmt.Println("               Conversational lab assistant backed by local MCP tooling")

		fmt.Println()
		fmt.Println("🔧 RUNTIME UTILITIES")
		fmt.Println("   - capacity  Engine capacity view + per-stack footprint estimates")
		fmt.Println("   - status    Global ecosystem status snapshot across all products")
		fmt.Println("   - health    hal-status sidecar lifecycle (create / update / delete)")
		fmt.Println("   - delete    Tear down all HAL infrastructure globally")

		fmt.Println()
		fmt.Println("=========================================================")
		fmt.Println("💡 Tip: Run 'hal <product>' to view its Smart Status dashboard!")
		fmt.Println("   Or run 'hal <product> create' to spin it up.")
		fmt.Println("   Each product supports: create / update / delete / status")
		fmt.Println("   Product features support: enable / update / disable / status")
		fmt.Println("=========================================================")
	},
}

func init() {
	rootCmd.AddCommand(catalogCmd)
}
