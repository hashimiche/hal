package observability

import (
	"fmt"
	"hal/internal/global"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	obsUpdate   bool
	lokiVer     string
	grafanaVer  string
	promVer     string
	promtailVer string
)

var deployCmd = &cobra.Command{
	Use:   "create",
	Short: "Create the PLG Stack (Prometheus, Loki, Grafana, Promtail)",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		if obsUpdate {
			fmt.Println("♻️  Update requested. Reconciling observability stack...")
			_ = exec.Command(engine, "rm", "-f", "hal-grafana", "hal-promtail", "hal-loki", "hal-prometheus").Run()
			homeDir, _ := os.UserHomeDir()
			_ = os.RemoveAll(filepath.Join(homeDir, ".hal", "obs"))
		}

		global.WarnIfEngineResourcesTight(engine, "obs-deploy")
		if !global.DryRun {
			proceed, err := global.ConfirmScenarioProceed(engine, "obs-deploy")
			if err != nil && global.Debug {
				fmt.Printf("[DEBUG] Capacity confirmation unavailable: %v\n", err)
			}
			if err == nil && !proceed {
				fmt.Printf("🛑 Observability deployment aborted to protect your %s engine.\n", engine)
				return
			}
		} else {
			fmt.Println("[DRY RUN] Would ensure hal-net exists")
			fmt.Println("[DRY RUN] Would pull Prometheus, Loki, Promtail, and Grafana images")
			fmt.Println("[DRY RUN] Would generate local PLG configuration under ~/.hal/obs")
			fmt.Println("[DRY RUN] Would boot Prometheus, Loki, Promtail, and Grafana containers")
			return
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

		promConfig := strings.Join([]string{
			"global:",
			"  scrape_interval: 15s",
			"scrape_configs:",
			"  - job_name: 'vault'",
			"    metrics_path: '/v1/sys/metrics'",
			"    params:",
			"      format: ['prometheus']",
			"    file_sd_configs:",
			"      - files: ['/etc/prometheus/targets/vault.json']",
			"  - job_name: 'consul'",
			"    metrics_path: '/v1/agent/metrics'",
			"    params:",
			"      format: ['prometheus']",
			"    file_sd_configs:",
			"      - files: ['/etc/prometheus/targets/consul.json']",
			"  - job_name: 'nomad'",
			"    metrics_path: '/v1/metrics'",
			"    params:",
			"      format: ['prometheus']",
			"    file_sd_configs:",
			"      - files: ['/etc/prometheus/targets/nomad.json']",
			"  - job_name: 'boundary'",
			"    metrics_path: '/v1/metrics'",
			"    file_sd_configs:",
			"      - files: ['/etc/prometheus/targets/boundary.json']",
			"  - job_name: 'terraform-enterprise'",
			"    metrics_path: '/metrics'",
			"    params:",
			"      format: ['prometheus']",
			"    file_sd_configs:",
			"      - files: ['/etc/prometheus/targets/terraform.json']",
		}, "\n") + "\n"
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

		grafanaDatasources := strings.Join([]string{
			"apiVersion: 1",
			"datasources:",
			"  - name: Prometheus",
			"    uid: hal-prometheus",
			"    type: prometheus",
			"    access: proxy",
			"    url: http://hal-prometheus:9090",
			"    isDefault: true",
			"  - name: Loki",
			"    uid: hal-loki",
			"    type: loki",
			"    access: proxy",
			"    url: http://hal-loki:3100",
		}, "\n") + "\n"
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

		fmt.Println("⏳ Waiting for Prometheus, Loki, and Grafana health checks...")
		if err := waitForObsHealth(engine); err != nil {
			fmt.Printf("⚠️  Stack started but health checks are not fully ready yet: %v\n", err)
			fmt.Println("   You can still check logs with: hal obs status")
			return
		}

		fmt.Println()
		fmt.Println("✅ Observability Stack Deployed Successfully!")
		global.RefreshHalStatus(engine)
		fmt.Println("---------------------------------------------------------")
		fmt.Println("🔗 Grafana:    http://grafana.localhost:3000 (Auto-logged in as Admin)")
		fmt.Println("🔗 Prometheus: http://prometheus.localhost:9090")
		fmt.Println("🔗 Loki API:   http://loki.localhost:3100/ready")
		fmt.Println("---------------------------------------------------------")
	},
}

func waitForObsHealth(engine string) error {
	timeout := time.After(90 * time.Second)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		promReady := probeHTTP("http://127.0.0.1:9090/-/ready")
		lokiReady := probeHTTP("http://127.0.0.1:3100/ready")
		grafanaReady := probeHTTP("http://127.0.0.1:3000/api/health")
		allReady := promReady && lokiReady && grafanaReady

		fmt.Printf("\r   readiness: Prometheus %s | Loki %s | Grafana %s   ", readinessLabel(promReady), readinessLabel(lokiReady), readinessLabel(grafanaReady))
		if allReady {
			fmt.Print("\n")
			return nil
		}

		exitedContainer, stateErr := firstNonRunningObsContainer(engine)
		if stateErr == nil && exitedContainer != "" {
			fmt.Print("\n")
			return fmt.Errorf("%s is not running", exitedContainer)
		}

		select {
		case <-timeout:
			fmt.Print("\n")
			return fmt.Errorf("timeout while waiting for endpoints to report ready")
		case <-ticker.C:
		}
	}
}

func readinessLabel(ok bool) string {
	if ok {
		return "ready"
	}
	return "starting"
}

func firstNonRunningObsContainer(engine string) (string, error) {
	containers := []string{"hal-prometheus", "hal-loki", "hal-promtail", "hal-grafana"}
	for _, c := range containers {
		out, err := exec.Command(engine, "inspect", "-f", "{{.State.Status}}", c).CombinedOutput()
		if err != nil {
			return c, nil
		}
		if strings.TrimSpace(string(out)) != "running" {
			return c, nil
		}
	}
	return "", nil
}

func probeHTTP(url string) bool {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Reconcile the PLG observability stack",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		obsUpdate = true
		deployCmd.Run(cmd, args)
	},
}

func bindLifecycleFlags(cmd *cobra.Command, includeUpdate bool) {
	if includeUpdate {
		cmd.Flags().BoolVarP(&obsUpdate, "update", "u", false, "Reconcile an existing observability stack in place")
	}
	cmd.Flags().StringVar(&lokiVer, "loki-version", "3.7", "Tag for the grafana/loki image")
	cmd.Flags().StringVar(&grafanaVer, "grafana-version", "main", "Tag for the grafana/grafana image")
	cmd.Flags().StringVar(&promVer, "prom-version", "main", "Tag for the prom/prometheus image")
	cmd.Flags().StringVar(&promtailVer, "promtail-version", "3.6", "Tag for the grafana/promtail image")
}

func init() {
	bindLifecycleFlags(deployCmd, true)
	bindLifecycleFlags(updateCmd, false)
	Cmd.AddCommand(deployCmd)
	Cmd.AddCommand(updateCmd)
}
