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
	obsForce    bool
	lokiVer     string
	grafanaVer  string
	promVer     string
	promtailVer string
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy the PLG Stack (Prometheus, Loki, Grafana, Promtail)",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		if obsForce {
			fmt.Println("♻️  Force cleanup detected. Purging stack...")
			_ = exec.Command(engine, "rm", "-f", "hal-grafana", "hal-promtail", "hal-loki", "hal-prometheus").Run()
			homeDir, _ := os.UserHomeDir()
			_ = os.RemoveAll(filepath.Join(homeDir, ".hal", "obs"))
		}

		global.EnsureNetwork(engine)

		fmt.Println("📥 Pulling Observability images (this might take a minute)...")
		images := []string{
			"prom/prometheus:" + promVer,
			"grafana/loki:" + lokiVer,
			"grafana/promtail:" + promtailVer,
			"grafana/grafana:" + grafanaVer,
		}

		for _, img := range images {
			pullCmd := exec.Command(engine, "pull", img)
			_ = pullCmd.Run() // Silent pull
		}

		fmt.Println("⚙️  Generating PLG stack configurations...")
		homeDir, _ := os.UserHomeDir()
		configDir := filepath.Join(homeDir, ".hal", "obs")
		targetsDir := filepath.Join(configDir, "targets")
		dashboardsDir := filepath.Join(configDir, "dashboards")
		_ = os.MkdirAll(configDir, 0755)
		_ = os.MkdirAll(targetsDir, 0755)
		_ = os.MkdirAll(dashboardsDir, 0755)

		promConfig := `global:
  scrape_interval: 15s
scrape_configs:
  - job_name: 'vault'
    metrics_path: '/v1/sys/metrics'
    params:
      format: ['prometheus']
		file_sd_configs:
			- files: ['/etc/prometheus/targets/vault.json']
	- job_name: 'consul'
		metrics_path: '/v1/agent/metrics'
		params:
			format: ['prometheus']
		file_sd_configs:
			- files: ['/etc/prometheus/targets/consul.json']
	- job_name: 'nomad'
		metrics_path: '/v1/metrics'
		params:
			format: ['prometheus']
		file_sd_configs:
			- files: ['/etc/prometheus/targets/nomad.json']
	- job_name: 'boundary'
		metrics_path: '/v1/metrics'
		file_sd_configs:
			- files: ['/etc/prometheus/targets/boundary.json']
	- job_name: 'terraform-enterprise'
		metrics_path: '/metrics'
		file_sd_configs:
			- files: ['/etc/prometheus/targets/terraform.json']
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

		grafanaDashboardsProvisioning := `apiVersion: 1
providers:
	- name: 'hal'
		orgId: 1
		folder: 'HAL'
		type: file
		disableDeletion: false
		editable: true
		options:
			path: /var/lib/grafana/dashboards
`
		_ = os.WriteFile(filepath.Join(configDir, "dashboards.yml"), []byte(grafanaDashboardsProvisioning), 0644)

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

		bootContainer("Prometheus", "run", "-d", "--name", "hal-prometheus", "--network", "hal-net", "-p", "9090:9090", "-v", filepath.Join(configDir, "prometheus.yml")+":/etc/prometheus/prometheus.yml", "-v", targetsDir+":/etc/prometheus/targets", "prom/prometheus:"+promVer)
		bootContainer("Loki", "run", "-d", "--name", "hal-loki", "--network", "hal-net", "-p", "3100:3100", "-v", filepath.Join(configDir, "loki-config.yaml")+":/etc/loki/local-config.yaml", "grafana/loki:"+lokiVer, "-config.file=/etc/loki/local-config.yaml")
		bootContainer("Promtail", "run", "-d", "--name", "hal-promtail", "--network", "hal-net", "-v", "hal-vault-logs:/vault/logs:ro", "-v", filepath.Join(configDir, "promtail-config.yaml")+":/etc/promtail/config.yml", "grafana/promtail:"+promtailVer, "-config.file=/etc/promtail/config.yml")
		bootContainer("Grafana", "run", "-d", "--name", "hal-grafana", "--network", "hal-net", "-p", "3000:3000", "-v", filepath.Join(configDir, "datasources.yml")+":/etc/grafana/provisioning/datasources/datasources.yml", "-v", filepath.Join(configDir, "dashboards.yml")+":/etc/grafana/provisioning/dashboards/dashboards.yml", "-v", dashboardsDir+":/var/lib/grafana/dashboards", "-e", "GF_AUTH_ANONYMOUS_ENABLED=true", "-e", "GF_AUTH_ANONYMOUS_ORG_ROLE=Admin", "grafana/grafana:"+grafanaVer)

		fmt.Println()
		fmt.Println("✅ Observability Stack Deployed Successfully!")
		fmt.Println("---------------------------------------------------------")
		fmt.Println("🔗 Grafana:    http://grafana.localhost:3000 (Auto-logged in as Admin)")
		fmt.Println("🔗 Prometheus: http://prometheus.localhost:9090")
		fmt.Println("🔗 Loki API:   http://loki.localhost:3100/ready")
		fmt.Println("---------------------------------------------------------")
		fmt.Println("💡 Tip: Go to Grafana -> Dashboards -> New -> Import.")
		fmt.Println("   Use HashiCorp official dashboard ID: 12904 for Vault!")
	},
}

func init() {
	deployCmd.Flags().BoolVarP(&obsForce, "force", "f", false, "Force a clean redeployment")
	deployCmd.Flags().StringVar(&lokiVer, "loki-version", "3.7", "Tag for the grafana/loki image")
	deployCmd.Flags().StringVar(&grafanaVer, "grafana-version", "main", "Tag for the grafana/grafana image")
	deployCmd.Flags().StringVar(&promVer, "prom-version", "main", "Tag for the prom/prometheus image")
	deployCmd.Flags().StringVar(&promtailVer, "promtail-version", "3.6", "Tag for the grafana/promtail image")
	Cmd.AddCommand(deployCmd)
}
