package vault

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

const vaultObsProduct = "vault"
const vaultObsTarget = "hal-vault:8200"

var vaultObsCmd = &cobra.Command{
	Use:   "obs",
	Short: "Manage Vault observability artifacts",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		vaultObsStatusCmd.Run(cmd, args)
	},
}

var vaultObsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create Vault observability artifacts",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}
		if !global.IsContainerRunning(engine, "hal-vault") {
			fmt.Println("❌ Vault is not running.")
			fmt.Println("   💡 Run 'hal vault create' first.")
			return
		}
		if !global.IsObsReady(engine) {
			fmt.Printf("❌ Observability stack is not ready. Missing: %s\n", strings.Join(global.ObsMissingComponents(engine), ", "))
			fmt.Println("   💡 Run 'hal obs create' first.")
			return
		}

		fmt.Println("🩺 Configuring observability artifacts for Vault...")
		for _, warning := range global.RegisterObsArtifacts(vaultObsProduct, []string{vaultObsTarget}) {
			fmt.Printf("⚠️  %s\n", warning)
		}
		fmt.Println("✅ Vault observability artifacts configured.")
	},
}

var vaultObsUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Refresh Vault observability artifacts",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		vaultObsCreateCmd.Run(cmd, args)
	},
}

var vaultObsDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete Vault observability artifacts",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if err := global.RemoveObsPromTargetFile(vaultObsProduct); err != nil {
			fmt.Printf("❌ Failed to remove Vault observability target file: %v\n", err)
			return
		}
		_ = os.Remove(filepath.Join(global.ObsDashboardsDir(), vaultObsProduct+".json"))
		fmt.Println("✅ Vault observability artifacts deleted.")
	},
}

var vaultObsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Vault observability artifact status",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		targetsPath := filepath.Join(global.ObsTargetsDir(), vaultObsProduct+".json")
		targetConfigured := obsTargetFileContains(targetsPath, vaultObsTarget)
		dashboardPath := filepath.Join(global.ObsDashboardsDir(), vaultObsProduct+".json")
		_, dashboardErr := os.Stat(dashboardPath)

		fmt.Println("Vault Observability Status")
		fmt.Println("==========================")
		fmt.Printf("Obs stack:        %v\n", global.IsObsReady(engine))
		fmt.Printf("Target configured: %v (%s)\n", targetConfigured, vaultObsTarget)
		fmt.Printf("Dashboard file:   %v\n", dashboardErr == nil)
	},
}

func obsTargetFileContains(path, wanted string) bool {
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
	vaultObsCmd.AddCommand(vaultObsCreateCmd)
	vaultObsCmd.AddCommand(vaultObsUpdateCmd)
	vaultObsCmd.AddCommand(vaultObsDeleteCmd)
	vaultObsCmd.AddCommand(vaultObsStatusCmd)
	Cmd.AddCommand(vaultObsCmd)
}
