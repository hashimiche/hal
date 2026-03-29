package observability

import (
	"fmt"
	"hal/internal/global"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	obsDestroy  bool
	obsForce    bool
	lokiVer     string
	grafanaVer  string
	promVer     string
	promtailVer string
)

var deployCmd = &cobra.Command{
	Use:   "deploy", // 🎯 THIS was the bug. It was still set to "obs".
	Short: "Deploy the PLG Stack (Prometheus, Loki, Grafana, Promtail)",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		// ==========================================
		// THE DESTROY LOGIC (--destroy OR --force)
		// ==========================================
		if obsDestroy || obsForce {
			fmt.Println("⚙️  Cleaning up Observability stack (in order)...")

			_ = exec.Command(engine, "rm", "-f", "hal-grafana", "hal-promtail", "hal-loki", "hal-prometheus").Run()
			_ = os.RemoveAll("/tmp/hal-obs")

			if obsDestroy {
				fmt.Println("✅ Observability environment destroyed successfully!")
				return
			}
			fmt.Println("♻️  Force cleanup complete. Proceeding with fresh deployment...")
		}

		// ==========================================
		// THE DEPLOY LOGIC
		// ==========================================
		global.EnsureNetwork(engine)

		// 0. Explicitly Pull Images
		fmt.Println("📥 Pulling Observability images (this might take a minute)...")
		images := []string{
			"prom/prometheus:" + promVer,
			"grafana/loki:" + lokiVer,
			"grafana/promtail:" + promtailVer,
			"grafana/grafana:" + grafanaVer,
		}

		for _, img := range images {
			pullCmd := exec.Command(engine, "pull", img)
			pullCmd.Stdout = os.Stdout // Wire Docker's output to our terminal
			pullCmd.Stderr = os.Stderr
			if err := pullCmd.Run(); err != nil {
				fmt.Printf("⚠️  Warning: Could not pull %s (it might already exist locally)\n", img)
			}
		}

		// 1. Generate Configurations safely in the User's Home Directory
		fmt.Println("⚙️  Generating PLG stack configurations...")

		homeDir, err := os.UserHomeDir()
		if err != nil {
			fmt.Printf("❌ Could not find home directory: %v\n", err)
			return
		}

		// Changes /tmp/hal-obs to ~/.hal/obs
		configDir := filepath.Join(homeDir, ".hal", "obs")
		_ = os.MkdirAll(configDir, 0755)

		promConfig := `global:
  scrape_interval: 15s
scrape_configs:
  - job_name: 'vault'
    metrics_path: '/v1/sys/metrics'
    params:
      format: ['prometheus']
    static_configs:
      - targets: ['hal-vault:8200']
`
		_ = os.WriteFile(filepath.Join(configDir, "prometheus.yml"), []byte(promConfig), 0644)

		lokiConfig := `auth_enabled: false
server:
  http_listen_port: 3100
common:
  path_prefix: /tmp/loki
  storage:
    filesystem:
      chunks_directory: /tmp/loki/chunks
      rules_directory: /tmp/loki/rules
  replication_factor: 1
  ring:
    kvstore:
      store: inmemory
schema_config:
  configs:
    - from: 2020-10-24
      store: tsdb
      object_store: filesystem
      schema: v13
      index:
        prefix: index_
        period: 24h
`
		_ = os.WriteFile(filepath.Join(configDir, "loki-config.yaml"), []byte(lokiConfig), 0644)

		promtailConfig := `server:
  http_listen_port: 9080
  grpc_listen_port: 0
positions:
  filename: /tmp/positions.yaml
clients:
  - url: http://hal-loki:3100/loki/api/v1/push
scrape_configs:
  - job_name: vault-audit
    static_configs:
      - targets:
          - localhost
        labels:
          job: vault-audit
          __path__: /vault/logs/audit.log
`
		_ = os.WriteFile(filepath.Join(configDir, "promtail-config.yaml"), []byte(promtailConfig), 0644)

		grafanaDatasources := `apiVersion: 1
datasources:
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://hal-prometheus:9090
    isDefault: true
  - name: Loki
    type: loki
    access: proxy
    url: http://hal-loki:3100
`
		_ = os.WriteFile(filepath.Join(configDir, "datasources.yml"), []byte(grafanaDatasources), 0644)

		// Helper function to boot containers and catch errors
		bootContainer := func(name string, args ...string) {
			fmt.Printf("⚙️  Booting %s...\n", name)
			out, err := exec.Command(engine, args...).CombinedOutput()
			if err != nil {
				fmt.Printf("❌ Failed to boot %s!\n", name)
				fmt.Printf("   Error: %v\n", err)
				fmt.Printf("   Docker Output: %s\n", string(out))
				os.Exit(1) // Stop the CLI if a core component fails
			}
		}

		// 2. Boot Containers using the dynamic versions
		bootContainer("Prometheus", "run", "-d", "--name", "hal-prometheus", "--network", "hal-net", "-p", "9090:9090", "-v", filepath.Join(configDir, "prometheus.yml")+":/etc/prometheus/prometheus.yml", "prom/prometheus:"+promVer)

		bootContainer("Loki", "run", "-d", "--name", "hal-loki", "--network", "hal-net", "-p", "3100:3100", "-v", filepath.Join(configDir, "loki-config.yaml")+":/etc/loki/local-config.yaml", "grafana/loki:"+lokiVer, "-config.file=/etc/loki/local-config.yaml")

		bootContainer("Promtail", "run", "-d", "--name", "hal-promtail", "--network", "hal-net", "-v", "hal-vault-logs:/vault/logs:ro", "-v", filepath.Join(configDir, "promtail-config.yaml")+":/etc/promtail/config.yml", "grafana/promtail:"+promtailVer, "-config.file=/etc/promtail/config.yml")

		bootContainer("Grafana", "run", "-d", "--name", "hal-grafana", "--network", "hal-net", "-p", "3000:3000", "-v", filepath.Join(configDir, "datasources.yml")+":/etc/grafana/provisioning/datasources/datasources.yml", "-e", "GF_AUTH_ANONYMOUS_ENABLED=true", "-e", "GF_AUTH_ANONYMOUS_ORG_ROLE=Admin", "grafana/grafana:"+grafanaVer)

		fmt.Println("\n✅ Observability Stack Deployed Successfully!")
		fmt.Println("---------------------------------------------------------")
		fmt.Println("🔗 Grafana:    http://localhost:3000 (Auto-logged in as Admin)")
		fmt.Println("🔗 Prometheus: http://localhost:9090")
		fmt.Println("🔗 Loki API:   http://localhost:3100/ready")
		fmt.Println("---------------------------------------------------------")
		fmt.Println("💡 Tip: Go to Grafana -> Dashboards -> New -> Import.")
		fmt.Println("   Use HashiCorp official dashboard ID: 12904 for Vault!")
		fmt.Println("---------------------------------------------------------")
	},
}

func init() {
	deployCmd.Flags().BoolVarP(&obsDestroy, "destroy", "d", false, "Destroy the Observability infrastructure")
	deployCmd.Flags().BoolVarP(&obsForce, "force", "f", false, "Force a clean redeployment")

	// Versioning Flags
	deployCmd.Flags().StringVar(&lokiVer, "loki-version", "3.7", "Tag for the grafana/loki image (default is safe v3.x)")
	deployCmd.Flags().StringVar(&grafanaVer, "grafana-version", "main", "Tag for the grafana/grafana image")
	deployCmd.Flags().StringVar(&promVer, "prom-version", "main", "Tag for the prom/prometheus image")
	deployCmd.Flags().StringVar(&promtailVer, "promtail-version", "3.6", "Tag for the grafana/promtail image")

	Cmd.AddCommand(deployCmd)
}
