package observability

import (
	"fmt"
	"hal/internal/global"
	"hal/internal/ui"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	obsUpdate       bool
	lokiVer         string
	lokiImage       string
	grafanaVer      string
	grafanaImage    string
	promVer         string
	promImage       string
	promtailVer     string
	promtailImage   string
	promConfigPath  string
	obsJobName      string
	obsMetricsPath  string
	obsMetricsToken string
	obsScrapeConfig string
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
			_ = exec.Command(engine, "rm", "-f", obsGrafanaContainer, obsPromtailContainer, obsLokiContainer, obsPrometheusContainer).Run()
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

		ui.LogoStart("observability")
		defer ui.LogoStop()
		step := func(cols int, format string, args ...any) {
			ui.LogoStep(format, args...)
			ui.LogoAdvance(cols)
		}

		// Anchor the fill low, then creep it forward across the (possibly long) pull.
		step(1, "Pulling observability images (Prometheus, Loki, Promtail, Grafana)")
		ui.LogoCreep(1500 * time.Millisecond)

		global.EnsureNetwork(engine)

		images := []string{
			promImage + ":" + promVer,
			lokiImage + ":" + lokiVer,
			promtailImage + ":" + promtailVer,
			grafanaImage + ":" + grafanaVer,
		}

		for _, img := range images {
			pullCmd := exec.Command(engine, "pull", img)
			_ = pullCmd.Run() // Silent pull
		}

		step(6, "Generating PLG stack configuration")
		homeDir, _ := os.UserHomeDir()
		configDir := filepath.Join(homeDir, ".hal", "obs")
		targetsDir := filepath.Join(configDir, "targets")
		dashboardsDir := filepath.Join(configDir, "dashboards")
		_ = os.MkdirAll(configDir, 0755)
		_ = os.MkdirAll(targetsDir, 0755)
		_ = os.MkdirAll(dashboardsDir, 0755)

		if promConfigPath != "" {
			src, err := os.ReadFile(promConfigPath)
			if err != nil {
				ui.LogoStop()
				fmt.Printf("❌ Cannot read --prom-config-path %q: %v\n", promConfigPath, err)
				return
			}
			if err := os.WriteFile(filepath.Join(configDir, "prometheus.yml"), src, 0644); err != nil {
				ui.LogoStop()
				fmt.Printf("❌ Failed to write prometheus.yml: %v\n", err)
				return
			}
			fmt.Printf("📄 Using custom Prometheus config: %s\n", promConfigPath)
		} else {

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

		} // end promConfigPath else

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
			step(6, "Booting %s", name)
			out, err := exec.Command(engine, args...).CombinedOutput()
			if err != nil {
				ui.LogoStop()
				fmt.Printf("❌ Failed to boot %s!\n", name)
				fmt.Printf("   Error: %v\n", err)
				fmt.Printf("   Docker Output: %s\n", string(out))
				os.Exit(1) // Stop the CLI if a core component fails
			}
		}

		bootContainer("Prometheus", "run", "-d", "--name", obsPrometheusContainer, "--network", global.HalNetName, "-p", "9090:9090", "-v", filepath.Join(configDir, "prometheus.yml")+":/etc/prometheus/prometheus.yml", "-v", targetsDir+":/etc/prometheus/targets", promImage+":"+promVer)
		bootContainer("Loki", "run", "-d", "--name", obsLokiContainer, "--network", global.HalNetName, "-p", "3100:3100", "-v", filepath.Join(configDir, "loki-config.yaml")+":/etc/loki/local-config.yaml", lokiImage+":"+lokiVer, "-config.file=/etc/loki/local-config.yaml")
		bootContainer("Promtail", "run", "-d", "--name", obsPromtailContainer, "--network", global.HalNetName, "-v", "hal-vault-logs:/vault/logs:ro", "-v", filepath.Join(configDir, "promtail-config.yaml")+":/etc/promtail/config.yml", promtailImage+":"+promtailVer, "-config.file=/etc/promtail/config.yml")
		bootContainer("Grafana", "run", "-d", "--name", obsGrafanaContainer, "--network", global.HalNetName, "-p", "3000:3000", "-v", filepath.Join(configDir, "datasources.yml")+":/etc/grafana/provisioning/datasources/datasources.yml", "-v", filepath.Join(configDir, "dashboards.yml")+":/etc/grafana/provisioning/dashboards/dashboards.yml", "-v", dashboardsDir+":/var/lib/grafana/dashboards", "-e", "GF_AUTH_ANONYMOUS_ENABLED=true", "-e", "GF_AUTH_ANONYMOUS_ORG_ROLE=Admin", grafanaImage+":"+grafanaVer)

		ui.LogoStep("Waiting for Prometheus, Loki, Grafana to become healthy")
		ui.LogoCreep(1500 * time.Millisecond)
		if err := waitForObsHealth(engine); err != nil {
			ui.LogoStop()
			fmt.Printf("⚠️  Stack started but health checks are not fully ready yet: %v\n", err)
			fmt.Println("   You can still check logs with: hal obs status")
			return
		}

		ui.LogoStop()
		global.RefreshHalHealth(engine)
		ui.Success("Observability stack deployed!")
		ui.Section("Endpoints")
		ui.Field("Grafana", fmt.Sprintf("%s  (auto-login as Admin)", obsGrafanaURL))
		ui.Field("Prometheus", obsPrometheusURL)
		ui.Field("Loki API", obsLokiReadyURL)

		if obsJobName != "" {
			configPath := filepath.Join(filepath.Join(homeDir, ".hal", "obs"), "prometheus.yml")
			targetFile := obsJobName + ".json"
			if err := global.UpsertObsPromJob(configPath, obsJobName, obsMetricsPath, obsMetricsToken, targetFile); err != nil {
				fmt.Printf("⚠️  Custom job registration failed: %v\n", err)
			} else if err := global.ReloadPrometheus(engine); err != nil {
				fmt.Printf("⚠️  Prometheus reload failed: %v\n", err)
			} else {
				targetPath := filepath.Join(targetsDir, targetFile)
				placeholder := fmt.Sprintf(`[{"targets":["host:port"],"labels":{"job":%q}}]\n`, obsJobName)
				if _, statErr := os.Stat(targetPath); os.IsNotExist(statErr) {
					_ = os.WriteFile(targetPath, []byte(placeholder), 0o644)
				}
				fmt.Printf("✅ Custom Prometheus job '%s' registered (path: %s).\n", obsJobName, obsMetricsPath)
				fmt.Printf("   📄 Edit target file: %s\n", targetPath)
				fmt.Println("   Replace \"host:port\" with your real endpoint.")
			}
		}

		if obsJobName == "" && obsScrapeConfig != "" {
			configPath := filepath.Join(filepath.Join(homeDir, ".hal", "obs"), "prometheus.yml")
			if err := global.UpsertObsPromJobFromFile(configPath, obsScrapeConfig); err != nil {
				fmt.Printf("⚠️  Scrape config merge failed: %v\n", err)
			} else if err := global.ReloadPrometheus(engine); err != nil {
				fmt.Printf("⚠️  Prometheus reload failed: %v\n", err)
			} else {
				fmt.Printf("✅ Scrape config from %s merged into prometheus.yml.\n", obsScrapeConfig)
			}
		}
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

		ui.LogoStep("Health: Prometheus %s · Loki %s · Grafana %s", readinessLabel(promReady), readinessLabel(lokiReady), readinessLabel(grafanaReady))
		if allReady {
			return nil
		}

		exitedContainer, stateErr := firstNonRunningObsContainer(engine)
		if stateErr == nil && exitedContainer != "" {
			return fmt.Errorf("%s is not running", exitedContainer)
		}

		select {
		case <-timeout:
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
	containers := []string{obsPrometheusContainer, obsLokiContainer, obsPromtailContainer, obsGrafanaContainer}
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
		// If only a custom job flag is passed, just edit prometheus.yml and
		// reload — do NOT tear down or recreate the stack.
		if obsJobName != "" || obsScrapeConfig != "" {
			engine, err := global.DetectEngine()
			if err != nil {
				fmt.Printf("❌ Error: %v\n", err)
				return
			}
			homeDir, _ := os.UserHomeDir()
			configPath := filepath.Join(homeDir, ".hal", "obs", "prometheus.yml")
			targetsDir := filepath.Join(homeDir, ".hal", "obs", "targets")

			if obsJobName != "" {
				targetFile := obsJobName + ".json"
				if err := global.UpsertObsPromJob(configPath, obsJobName, obsMetricsPath, obsMetricsToken, targetFile); err != nil {
					fmt.Printf("⚠️  Custom job registration failed: %v\n", err)
					return
				}
				targetPath := filepath.Join(targetsDir, targetFile)
				placeholder := fmt.Sprintf("[{\"targets\":[\"host:port\"],\"labels\":{\"job\":%q}}]\n", obsJobName)
				if _, statErr := os.Stat(targetPath); os.IsNotExist(statErr) {
					_ = os.WriteFile(targetPath, []byte(placeholder), 0o644)
				}
				fmt.Printf("✅ Custom Prometheus job '%s' registered (path: %s).\n", obsJobName, obsMetricsPath)
				fmt.Printf("   📄 Edit target file: %s\n", targetPath)
				fmt.Println("   Replace \"host:port\" with your real endpoint.")
			}

			if obsScrapeConfig != "" {
				if err := global.UpsertObsPromJobFromFile(configPath, obsScrapeConfig); err != nil {
					fmt.Printf("⚠️  Scrape config merge failed: %v\n", err)
					return
				}
				fmt.Printf("✅ Scrape config from %s merged into prometheus.yml.\n", obsScrapeConfig)
			}

			if err := global.ReloadPrometheus(engine); err != nil {
				fmt.Printf("⚠️  Prometheus reload failed: %v\n", err)
			}
			return
		}

		obsUpdate = true
		deployCmd.Run(cmd, args)
	},
}

func bindLifecycleFlags(cmd *cobra.Command, includeUpdate bool) {
	if includeUpdate {
		cmd.Flags().BoolVarP(&obsUpdate, "update", "u", false, "Reconcile an existing observability stack in place")
	}
	cmd.Flags().StringVar(&lokiVer, "loki-tag", defaultLokiTag, "Tag for the Loki image")
	cmd.Flags().StringVar(&lokiImage, "loki-image", defaultLokiImage, "Loki container image name")
	cmd.Flags().StringVar(&grafanaVer, "grafana-tag", defaultGrafanaTag, "Tag for the Grafana image")
	cmd.Flags().StringVar(&grafanaImage, "grafana-image", defaultGrafanaImage, "Grafana container image name")
	cmd.Flags().StringVar(&promVer, "prometheus-tag", defaultPromTag, "Tag for the Prometheus image")
	cmd.Flags().StringVar(&promImage, "prometheus-image", defaultPromImage, "Prometheus container image name")
	cmd.Flags().StringVar(&promtailVer, "promtail-tag", defaultPromtailTag, "Tag for the Promtail image")
	cmd.Flags().StringVar(&promtailImage, "promtail-image", defaultPromtailImage, "Promtail container image name")
	cmd.Flags().StringVar(&promConfigPath, "prom-config-path", "", "Path to a custom prometheus.yml; skips the generated config when set")
	cmd.Flags().StringVar(&obsJobName, "job-name", "", "Register a custom Prometheus scrape job with this name (no local TFE required)")
	cmd.Flags().StringVar(&obsMetricsPath, "metrics-path", "/metrics", "Metrics path for the custom job (only used when --job-name is set)")
	cmd.Flags().StringVar(&obsMetricsToken, "metrics-token", "", "Bearer token for authenticated metrics endpoints (only used when --job-name is set)")
	cmd.Flags().StringVar(&obsScrapeConfig, "scrape-config-path", "", "Path to a JSON/YAML file with a single scrape job config to merge into prometheus.yml")
}

func init() {
	bindLifecycleFlags(deployCmd, true)
	bindLifecycleFlags(updateCmd, false)
	Cmd.AddCommand(deployCmd)
	Cmd.AddCommand(updateCmd)
}
