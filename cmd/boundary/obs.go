package boundary

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

const boundaryObsProduct = "boundary"
const boundaryObsTarget = "hal-boundary:9200"

var boundaryObsCmd = &cobra.Command{
	Use:   "obs",
	Short: "Manage Boundary observability artifacts",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		boundaryObsStatusCmd.Run(cmd, args)
	},
}

var boundaryObsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create Boundary observability artifacts",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}
		if !global.IsContainerRunning(engine, "hal-boundary") {
			fmt.Println("❌ Boundary is not running.")
			fmt.Println("   💡 Run 'hal boundary create' first.")
			return
		}
		if !global.IsObsReady(engine) {
			fmt.Printf("❌ Observability stack is not ready. Missing: %s\n", strings.Join(global.ObsMissingComponents(engine), ", "))
			fmt.Println("   💡 Run 'hal obs create' first.")
			return
		}

		fmt.Println("🩺 Configuring observability artifacts for Boundary...")
		for _, warning := range global.RegisterObsArtifacts(boundaryObsProduct, []string{boundaryObsTarget}) {
			fmt.Printf("⚠️  %s\n", warning)
		}
		fmt.Println("✅ Boundary observability artifacts configured.")
	},
}

var boundaryObsUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Refresh Boundary observability artifacts",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		boundaryObsCreateCmd.Run(cmd, args)
	},
}

var boundaryObsDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete Boundary observability artifacts",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if err := global.RemoveObsPromTargetFile(boundaryObsProduct); err != nil {
			fmt.Printf("❌ Failed to remove Boundary observability target file: %v\n", err)
			return
		}
		_ = os.Remove(filepath.Join(global.ObsDashboardsDir(), boundaryObsProduct+".json"))
		fmt.Println("✅ Boundary observability artifacts deleted.")
	},
}

var boundaryObsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Boundary observability artifact status",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		targetsPath := filepath.Join(global.ObsTargetsDir(), boundaryObsProduct+".json")
		targetConfigured := boundaryObsTargetFileContains(targetsPath, boundaryObsTarget)
		dashboardPath := filepath.Join(global.ObsDashboardsDir(), boundaryObsProduct+".json")
		_, dashboardErr := os.Stat(dashboardPath)

		fmt.Println("Boundary Observability Status")
		fmt.Println("=============================")
		fmt.Printf("Obs stack:         %v\n", global.IsObsReady(engine))
		fmt.Printf("Target configured: %v (%s)\n", targetConfigured, boundaryObsTarget)
		fmt.Printf("Dashboard file:    %v (optional)\n", dashboardErr == nil)
	},
}

func boundaryObsTargetFileContains(path, wanted string) bool {
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
	boundaryObsCmd.AddCommand(boundaryObsCreateCmd)
	boundaryObsCmd.AddCommand(boundaryObsUpdateCmd)
	boundaryObsCmd.AddCommand(boundaryObsDeleteCmd)
	boundaryObsCmd.AddCommand(boundaryObsStatusCmd)
	Cmd.AddCommand(boundaryObsCmd)
}
