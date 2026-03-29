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
		_ = os.MkdirAll(configDir, 0755)

		// [ ... Keep your exact Prom/Loki/Promtail/Grafana config string writing here ... ]
		// (Omitted the raw strings for brevity, keep your exact strings from your provided file)

		bootContainer := func(name string, args ...string) {
			fmt.Printf("⚙️  Booting %s...\n", name)
			out, err := exec.Command(engine, args...).CombinedOutput()
			if err != nil {
				fmt.Printf("❌ Failed to boot %s!\n   Error: %v\n   Docker Output: %s\n", name, err, string(out))
				os.Exit(1)
			}
		}

		bootContainer("Prometheus", "run", "-d", "--name", "hal-prometheus", "--network", "hal-net", "-p", "9090:9090", "-v", filepath.Join(configDir, "prometheus.yml")+":/etc/prometheus/prometheus.yml", "prom/prometheus:"+promVer)
		bootContainer("Loki", "run", "-d", "--name", "hal-loki", "--network", "hal-net", "-p", "3100:3100", "-v", filepath.Join(configDir, "loki-config.yaml")+":/etc/loki/local-config.yaml", "grafana/loki:"+lokiVer, "-config.file=/etc/loki/local-config.yaml")
		bootContainer("Promtail", "run", "-d", "--name", "hal-promtail", "--network", "hal-net", "-v", "hal-vault-logs:/vault/logs:ro", "-v", filepath.Join(configDir, "promtail-config.yaml")+":/etc/promtail/config.yml", "grafana/promtail:"+promtailVer, "-config.file=/etc/promtail/config.yml")
		bootContainer("Grafana", "run", "-d", "--name", "hal-grafana", "--network", "hal-net", "-p", "3000:3000", "-v", filepath.Join(configDir, "datasources.yml")+":/etc/grafana/provisioning/datasources/datasources.yml", "-e", "GF_AUTH_ANONYMOUS_ENABLED=true", "-e", "GF_AUTH_ANONYMOUS_ORG_ROLE=Admin", "grafana/grafana:"+grafanaVer)

		fmt.Println()
		fmt.Println("✅ Observability Stack Deployed Successfully!")
		fmt.Println("---------------------------------------------------------")
		fmt.Println("🔗 Grafana:    http://localhost:3000 (Auto-logged in as Admin)")
		fmt.Println("🔗 Prometheus: http://localhost:9090")
		fmt.Println("🔗 Loki API:   http://localhost:3100/ready")
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
