package consul

import (
	"fmt"
	"os/exec"
	"strings"

	"hal/internal/global"
	"hal/internal/ui"

	"github.com/spf13/cobra"
)

var (
	consulVersion string
	consulImage   string
	consulUpdate  bool
)

var deployCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a standalone Consul server for learning/testing",
	Run: func(cmd *cobra.Command, args []string) {

		// 1. Detect Docker or Podman
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		// 2. Ensure the global grid exists
		global.EnsureNetwork(engine)

		ui.LogoStart("consul")
		defer ui.LogoStop()

		if consulUpdate {
			if global.Debug {
				fmt.Println("[DEBUG] --update detected. Reconciling existing standalone Consul...")
			}
			ui.LogoStep("Reconciling existing Consul (update)")
			_ = exec.Command(engine, "rm", "-f", consulContainer).Run()
		}

		ui.LogoStep("Deploying standalone Consul %s", consulVersion)

		// Command: <engine> run -d --name hal-consul --network hal-net -p 8500:8500 hashicorp/consul:1.15.0 agent -server -ui -node=server-1 -bootstrap-expect=1 -client=0.0.0.0
		consulArgs := []string{
			"run", "-d",
			"--name", consulContainer,
			"--network", global.HalNetName,
			"-p", fmt.Sprintf("%d:%d", consulHTTPPort, consulHTTPPort), // The magic UI/API port
			fmt.Sprintf("%s:%s", consulImage, consulVersion),
			"agent", "-server", "-ui", "-node=hal-server", "-bootstrap-expect=1", "-client=0.0.0.0",
		}

		if global.DryRun {
			ui.LogoStop()
			fmt.Printf("[DRY RUN] Would execute: %s %s\n", engine, strings.Join(consulArgs, " "))
			return
		}

		ui.LogoStep("Starting Consul server")
		out, err := exec.Command(engine, consulArgs...).CombinedOutput()
		if err != nil {
			ui.LogoStop()
			if strings.Contains(string(out), "AlreadyExists") || strings.Contains(string(out), "already in use") {
				fmt.Println("⚠️  Consul already exists. Use '--update' to reconcile it.")
				return
			}
			fmt.Printf("❌ Failed to start Consul: %s\n", string(out))
			return
		}

		ui.LogoStop()
		global.RefreshHalHealth(engine)
		ui.Success("Standalone Consul server is up!")
		ui.Section("Access")
		ui.Field("UI", consulBaseURL)
		ui.Hint("Test the KV store / API. For real workloads: hal nomad create --join-consul")
	},
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Reconcile an existing Consul server",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		consulUpdate = true
		deployCmd.Run(cmd, args)
	},
}

func bindLifecycleFlags(cmd *cobra.Command, includeUpdate bool) {
	cmd.Flags().StringVarP(&consulVersion, "consul-tag", "v", defaultConsulTag, "Consul container image tag")
	cmd.Flags().StringVar(&consulImage, "consul-image", defaultConsulImage, "Consul container image name")
	if includeUpdate {
		cmd.Flags().BoolVarP(&consulUpdate, "update", "u", false, "Reconcile an existing Consul deployment in place")
	}
}

func init() {
	bindLifecycleFlags(deployCmd, true)
	bindLifecycleFlags(updateCmd, false)
	Cmd.AddCommand(deployCmd)
	Cmd.AddCommand(updateCmd)
}
