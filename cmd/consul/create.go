package consul

import (
	"fmt"
	"os/exec"
	"strings"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var (
	consulVersion string
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

		if consulUpdate {
			if global.Debug {
				fmt.Println("[DEBUG] --update detected. Reconciling existing standalone Consul...")
			}
			_ = exec.Command(engine, "rm", "-f", "hal-consul").Run()
		}

		fmt.Printf(" Deploying standalone Consul %s via %s...\n", consulVersion, engine)

		// Command: <engine> run -d --name hal-consul --network hal-net -p 8500:8500 hashicorp/consul:1.15.0 agent -server -ui -node=server-1 -bootstrap-expect=1 -client=0.0.0.0
		consulArgs := []string{
			"run", "-d",
			"--name", "hal-consul",
			"--network", "hal-net",
			"-p", "8500:8500", // The magic UI/API port
			fmt.Sprintf("hashicorp/consul:%s", consulVersion),
			"agent", "-server", "-ui", "-node=hal-server", "-bootstrap-expect=1", "-client=0.0.0.0",
		}

		if global.DryRun {
			fmt.Printf("[DRY RUN] Would execute: %s %s\n", engine, strings.Join(consulArgs, " "))
			return
		}

		out, err := exec.Command(engine, consulArgs...).CombinedOutput()
		if err != nil {
			if strings.Contains(string(out), "AlreadyExists") || strings.Contains(string(out), "already in use") {
				fmt.Println("⚠️  Consul already exists. Use '--update' to reconcile it.")
				return
			}
			fmt.Printf("❌ Failed to start Consul: %s\n", string(out))
			return
		}

		fmt.Println("✅ Standalone Consul Server is up!")
		global.RefreshHalStatus(engine)
		fmt.Println("   🔗 UI Address: http://consul.localhost:8500")
		fmt.Println("\n💡 Tip: Use this to test the KV store or learn the API.")
		fmt.Println("   (For real workloads, use 'hal nomad create --with-consul' instead!)")
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
	cmd.Flags().StringVarP(&consulVersion, "version", "v", "1.15.0", "Consul version to deploy")
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
