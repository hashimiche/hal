package consul

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

const consulObsProduct = "consul"
const consulObsTarget = "hal-consul:8500"

var consulObsCmd = &cobra.Command{
	Use:   "obs",
	Short: "Manage Consul observability artifacts",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		consulObsStatusCmd.Run(cmd, args)
	},
}

var consulObsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create Consul observability artifacts",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}
		if !global.IsContainerRunning(engine, "hal-consul") {
			fmt.Println("❌ Consul is not running.")
			fmt.Println("   💡 Run 'hal consul create' first.")
			return
		}
		if !global.IsObsReady(engine) {
			fmt.Printf("❌ Observability stack is not ready. Missing: %s\n", strings.Join(global.ObsMissingComponents(engine), ", "))
			fmt.Println("   💡 Run 'hal obs create' first.")
			return
		}

		fmt.Println("🩺 Configuring observability artifacts for Consul...")
		for _, warning := range global.RegisterObsArtifacts(consulObsProduct, []string{consulObsTarget}) {
			fmt.Printf("⚠️  %s\n", warning)
		}
		fmt.Println("✅ Consul observability artifacts configured.")
	},
}

var consulObsUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Refresh Consul observability artifacts",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		consulObsCreateCmd.Run(cmd, args)
	},
}

var consulObsDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete Consul observability artifacts",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if err := global.RemoveObsPromTargetFile(consulObsProduct); err != nil {
			fmt.Printf("❌ Failed to remove Consul observability target file: %v\n", err)
			return
		}
		_ = os.Remove(filepath.Join(global.ObsDashboardsDir(), consulObsProduct+".json"))
		fmt.Println("✅ Consul observability artifacts deleted.")
	},
}

var consulObsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Consul observability artifact status",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		targetsPath := filepath.Join(global.ObsTargetsDir(), consulObsProduct+".json")
		targetConfigured := consulObsTargetFileContains(targetsPath, consulObsTarget)
		dashboardPath := filepath.Join(global.ObsDashboardsDir(), consulObsProduct+".json")
		_, dashboardErr := os.Stat(dashboardPath)

		fmt.Println("Consul Observability Status")
		fmt.Println("===========================")
		fmt.Printf("Obs stack:         %v\n", global.IsObsReady(engine))
		fmt.Printf("Target configured: %v (%s)\n", targetConfigured, consulObsTarget)
		fmt.Printf("Dashboard file:    %v\n", dashboardErr == nil)
	},
}

func consulObsTargetFileContains(path, wanted string) bool {
	body, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	type targetEntry struct {
		Targets []string `json:"targets"`
	}
	entries := []targetEntry{}
	if err := json.Unmarshal(body, &entries); err != nil {
		return false
	}
	for _, entry := range entries {
		for _, t := range entry.Targets {
			if t == wanted {
				return true
			}
		}
	}
	return false
}

func init() {
	consulObsCmd.AddCommand(consulObsCreateCmd)
	consulObsCmd.AddCommand(consulObsUpdateCmd)
	consulObsCmd.AddCommand(consulObsDeleteCmd)
	consulObsCmd.AddCommand(consulObsStatusCmd)
	Cmd.AddCommand(consulObsCmd)
}
