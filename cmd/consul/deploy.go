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
	consulForce   bool
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy a standalone Consul server for learning/testing",
	Run: func(cmd *cobra.Command, args []string) {

		// 1. Detect Docker or Podman
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		// 2. Ensure the global grid exists
		global.EnsureNetwork(engine)

		if consulForce {
			if global.Debug {
				fmt.Println("[DEBUG] --force flag detected. Purging existing standalone Consul...")
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
				fmt.Println("⚠️  Consul is already deployed! Use '--force' to redeploy.")
				return
			}
			fmt.Printf("❌ Failed to start Consul: %s\n", string(out))
			return
		}

		fmt.Println("✅ Standalone Consul Server is up!")
		fmt.Println("   🔗 UI Address: http://consul.localhost:8500")
		for _, warning := range global.RegisterObsArtifacts("consul", []string{"hal-consul:8500"}) {
			fmt.Printf("⚠️  %s\n", warning)
		}
		fmt.Println("\n💡 Tip: Use this to test the KV store or learn the API.")
		fmt.Println("   (For real workloads, use 'hal nomad deploy --with-consul' instead!)")
	},
}

func init() {
	deployCmd.Flags().StringVarP(&consulVersion, "version", "v", "1.15.0", "Consul version to deploy")
	deployCmd.Flags().BoolVarP(&consulForce, "force", "f", false, "Force redeploy")

	Cmd.AddCommand(deployCmd)
}
