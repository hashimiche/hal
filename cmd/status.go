package cmd

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the global status of all HAL deployments",
	Long:  `Displays a high-level overview of which HashiCorp products are currently running locally.`,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		if debug {
			fmt.Println("[DEBUG] Reading global state to determine status...")
		}

		obsRunning := checkContainer(engine, "hal-grafana")
		obsEndpoint := "-"
		if obsRunning {
			obsEndpoint = "multiple (see components)"
		}

		type svcStatus struct {
			name     string
			container string
			running  bool
			endpoint string
			version  string
		}

		services := []svcStatus{
			{name: "Consul", container: "hal-consul", running: checkContainer(engine, "hal-consul"), endpoint: "http://consul.localhost:8500"},
			{name: "Vault", container: "hal-vault", running: checkContainer(engine, "hal-vault"), endpoint: "http://vault.localhost:8200"},
			{name: "Nomad", container: "hal-nomad", running: checkMultipass("hal-nomad"), endpoint: "Multipass VM"},
			{name: "Boundary", container: "hal-boundary", running: checkContainer(engine, "hal-boundary"), endpoint: "http://boundary.localhost:9200"},
			{name: "TFE", container: "hal-tfe", running: checkContainer(engine, "hal-tfe"), endpoint: "https://tfe.localhost:8443"},
			{name: "Observability", container: "hal-grafana", running: obsRunning, endpoint: obsEndpoint},
		}

		for i := range services {
			if services[i].running {
				services[i].version = resolveProductVersion(engine, services[i].name, services[i].container)
			} else {
				services[i].version = "-"
			}
		}

		runningCount := 0
		fmt.Println("HAL Global Deployment Status")
		fmt.Println("===============================")
		fmt.Printf("Engine: %s\n", engine)
		fmt.Printf("Updated: %s\n", time.Now().Format("2006-01-02 15:04:05"))
		fmt.Println("-------------------------------")
		fmt.Printf("%-13s %-13s %-31s %s\n", "Product", "State", "Endpoint", "Version")
		fmt.Println("-------------------------------")
		for _, svc := range services {
			icon := "⚪"
			state := "Not Deployed"
			if svc.running {
				icon = "🟢"
				state = "Running"
				runningCount++
			}
			fmt.Printf("%s %-13s %-13s %-31s %s\n", icon, svc.name, state, svc.endpoint, svc.version)
			printProductFeatureStatus(engine, svc.name, svc.running)
		}

		fmt.Println("-------------------------------")
		fmt.Printf("Summary: %d/%d products running\n", runningCount, len(services))
		fmt.Println("Tip:     Run 'hal <product> deploy' to start a stack, or 'hal <product> status' for deeper health.")
	},
}

func printVaultFeatureStatus(engine string) {
	featureStates := []struct {
		name   string
		status string
	}{
		{name: "audit", status: resolveVaultAuditStatus(engine)},
		{name: "k8s", status: boolState(checkContainer(engine, "kind-control-plane"))},
		{name: "jwt", status: boolState(checkContainer(engine, "hal-gitlab"))},
		{name: "ldap", status: boolState(checkContainer(engine, "hal-openldap"))},
		{name: "mariadb", status: boolState(checkContainer(engine, "hal-mariadb"))},
		{name: "oidc", status: boolState(checkContainer(engine, "hal-keycloak"))},
	}

	for _, f := range featureStates {
		fmt.Printf("   ↳ %-8s %s\n", f.name, f.status)
	}
}

func printProductFeatureStatus(engine, productName string, running bool) {
	switch productName {
	case "Vault":
		printVaultFeatureStatus(engine)
	case "Boundary":
		fmt.Printf("   ↳ %-8s %s\n", "mariadb", boolState(checkContainer(engine, "hal-boundary-target-mariadb")))
		fmt.Printf("   ↳ %-8s %s\n", "ssh", boolState(checkMultipass("hal-boundary-ssh")))
	case "TFE":
		tfeUp := checkContainer(engine, "hal-tfe")
		fmt.Printf("   ↳ %-8s %s\n", "workspace", boolState(tfeUp && checkContainer(engine, "hal-gitlab")))
	case "Nomad":
		fmt.Printf("   ↳ %-8s %s\n", "job", boolState(checkMultipass("hal-nomad")))
	case "Consul":
		fmt.Printf("   ↳ %-8s %s\n", "core", boolState(running))
	case "Observability":
		printObsFeatureStatus(engine)
	}
}

func printObsFeatureStatus(engine string) {
	features := []struct {
		name      string
		container string
		endpoint  string
	}{
		{name: "grafana", container: "hal-grafana", endpoint: "http://grafana.localhost:3000"},
		{name: "prometheus", container: "hal-prometheus", endpoint: "http://prometheus.localhost:9090"},
		{name: "loki", container: "hal-loki", endpoint: "http://loki.localhost:3100/ready"},
	}

	for _, f := range features {
		enabled := checkContainer(engine, f.container)
		if enabled {
			fmt.Printf("   ↳ %-10s enabled (%s)\n", f.name, f.endpoint)
		} else {
			fmt.Printf("   ↳ %-10s disabled\n", f.name)
		}
	}
}

func boolState(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func resolveVaultAuditStatus(engine string) string {
	if !checkContainer(engine, "hal-vault") {
		return "disabled"
	}

	out, err := exec.Command(
		engine,
		"exec",
		"-e",
		"VAULT_ADDR=http://127.0.0.1:8200",
		"-e",
		"VAULT_TOKEN=root",
		"hal-vault",
		"vault",
		"audit",
		"list",
		"-format=json",
	).Output()
	if err != nil {
		return "unknown"
	}

	trimmed := strings.TrimSpace(string(out))
	if trimmed == "{}" || trimmed == "" {
		return "disabled"
	}

	return "enabled"
}

func resolveProductVersion(engine, productName, container string) string {
	if productName == "Nomad" {
		return "Multipass"
	}

	imageRef := getContainerImageRef(engine, container)
	if imageRef == "" {
		return "unknown"
	}

	version := imageRef
	if strings.Contains(imageRef, ":") {
		parts := strings.Split(imageRef, ":")
		version = parts[len(parts)-1]
	}

	if productName == "Vault" {
		edition := "CE"
		lower := strings.ToLower(imageRef)
		if strings.Contains(lower, "enterprise") || strings.Contains(lower, "-ent") || strings.Contains(lower, ":ent") {
			edition = "Enterprise"
		}
		return fmt.Sprintf("%s (%s)", version, edition)
	}

	return version
}

func getContainerImageRef(engine, name string) string {
	out, err := exec.Command(engine, "inspect", "-f", "{{.Config.Image}}", name).Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(out))
}

// Helper to silently check if a container exists and is running
func checkContainer(engine, name string) bool {
	out, err := exec.Command(engine, "ps", "-q", "-f", fmt.Sprintf("name=^%s$", name)).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

// Helper to silently check if a Multipass VM is running
func checkMultipass(name string) bool {
	out, err := exec.Command("multipass", "info", name, "--format", "csv").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Running")
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
