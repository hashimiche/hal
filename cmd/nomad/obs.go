package nomad

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

const nomadObsProduct = "nomad"

var nomadObsCmd = &cobra.Command{
	Use:   "obs",
	Short: "Manage Nomad observability artifacts",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		nomadObsStatusCmd.Run(cmd, args)
	},
}

var nomadObsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create Nomad observability artifacts",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if !global.MultipassInstanceExists("hal-nomad") {
			fmt.Println("❌ Nomad VM is not present.")
			fmt.Println("   💡 Run 'hal nomad create' first.")
			return
		}

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}
		if !global.IsObsReady(engine) {
			fmt.Printf("❌ Observability stack is not ready. Missing: %s\n", strings.Join(global.ObsMissingComponents(engine), ", "))
			fmt.Println("   💡 Run 'hal obs create' first.")
			return
		}

		nomadTarget, err := resolveNomadObsTarget()
		if err != nil {
			fmt.Printf("❌ Unable to resolve Nomad target: %v\n", err)
			return
		}

		fmt.Println("🩺 Configuring observability artifacts for Nomad...")
		for _, warning := range global.RegisterObsArtifacts(nomadObsProduct, []string{nomadTarget}) {
			fmt.Printf("⚠️  %s\n", warning)
		}
		fmt.Println("✅ Nomad observability artifacts configured.")
	},
}

var nomadObsUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Refresh Nomad observability artifacts",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		nomadObsCreateCmd.Run(cmd, args)
	},
}

var nomadObsDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete Nomad observability artifacts",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if err := global.RemoveObsPromTargetFile(nomadObsProduct); err != nil {
			fmt.Printf("❌ Failed to remove Nomad observability target file: %v\n", err)
			return
		}
		_ = os.Remove(filepath.Join(global.ObsDashboardsDir(), nomadObsProduct+".json"))
		fmt.Println("✅ Nomad observability artifacts deleted.")
	},
}

var nomadObsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Nomad observability artifact status",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		nomadTarget, targetErr := resolveNomadObsTarget()
		targetsPath := filepath.Join(global.ObsTargetsDir(), nomadObsProduct+".json")
		targetConfigured := false
		if targetErr == nil {
			targetConfigured = nomadObsTargetFileContains(targetsPath, nomadTarget)
		}

		dashboardPath := filepath.Join(global.ObsDashboardsDir(), nomadObsProduct+".json")
		_, dashboardErr := os.Stat(dashboardPath)

		fmt.Println("Nomad Observability Status")
		fmt.Println("==========================")
		fmt.Printf("Obs stack:         %v\n", global.IsObsReady(engine))
		if targetErr != nil {
			fmt.Printf("Target configured: false (%v)\n", targetErr)
		} else {
			fmt.Printf("Target configured: %v (%s)\n", targetConfigured, nomadTarget)
		}
		fmt.Printf("Dashboard file:    %v\n", dashboardErr == nil)
	},
}

func resolveNomadObsTarget() (string, error) {
	ipOut, err := exec.Command("multipass", "info", "hal-nomad", "--format", "csv").Output()
	if err != nil {
		return "", err
	}
	ip := extractMultipassIP(string(ipOut))
	if strings.TrimSpace(ip) == "" || ip == "127.0.0.1" {
		return "", fmt.Errorf("hal-nomad IP not available")
	}
	return fmt.Sprintf("%s:4646", ip), nil
}

func nomadObsTargetFileContains(path, wanted string) bool {
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
	nomadObsCmd.AddCommand(nomadObsCreateCmd)
	nomadObsCmd.AddCommand(nomadObsUpdateCmd)
	nomadObsCmd.AddCommand(nomadObsDeleteCmd)
	nomadObsCmd.AddCommand(nomadObsStatusCmd)
	Cmd.AddCommand(nomadObsCmd)
}
