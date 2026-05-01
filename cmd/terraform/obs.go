package terraform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

const terraformObsProduct = "terraform"

// syncTerraformObsTargets keeps the canonical Terraform Prometheus target file in sync
// with whichever TFE instances are currently running.
func syncTerraformObsTargets(engine string) error {
	targets := terraformObsTargets(engine)
	if len(targets) == 0 {
		if err := global.RemoveObsPromTargetFile(terraformObsProduct); err != nil {
			return err
		}
		_ = global.RemoveObsPromTargetFile("terraform-bis")
		return nil
	}

	if err := global.UpsertObsPromTargetIfRunning(engine, terraformObsProduct, targets); err != nil {
		return err
	}

	// Remove legacy twin-specific target files once the canonical file is updated.
	_ = global.RemoveObsPromTargetFile("terraform-bis")
	return nil
}

func terraformObsTargets(engine string) []string {
	targets := []string{}

	if global.IsContainerRunning(engine, "hal-tfe") {
		targets = append(targets, "hal-tfe:9090")
	}

	if layout, err := buildTFETwinLayout(); err == nil {
		if core := strings.TrimSpace(layout.CoreContainer); core != "" && global.IsContainerRunning(engine, core) {
			targets = append(targets, core+":9090")
		}
	}

	return targets
}

func terraformObsTargetsForScope(engine, scope string) []string {
	targets := []string{}
	if (scope == tfeTargetPrimary || scope == tfeTargetBoth) && global.IsContainerRunning(engine, "hal-tfe") {
		targets = append(targets, "hal-tfe:9090")
	}

	if scope == tfeTargetTwin || scope == tfeTargetBoth {
		if layout, err := buildTFETwinLayout(); err == nil {
			if core := strings.TrimSpace(layout.CoreContainer); core != "" && global.IsContainerRunning(engine, core) {
				targets = append(targets, core+":9090")
			}
		}
	}

	return targets
}

func ensureObsScopeRunning(engine, scope string) error {
	primaryRunning := global.IsContainerRunning(engine, "hal-tfe")
	twinRunning := false
	if layout, err := buildTFETwinLayout(); err == nil {
		twinRunning = global.IsContainerRunning(engine, strings.TrimSpace(layout.CoreContainer))
	}

	switch scope {
	case tfeTargetPrimary:
		if !primaryRunning {
			return fmt.Errorf("Terraform Enterprise primary instance is not running")
		}
	case tfeTargetTwin:
		if !twinRunning {
			return fmt.Errorf("Terraform Enterprise twin instance is not running")
		}
	case tfeTargetBoth:
		if !primaryRunning || !twinRunning {
			missing := []string{}
			if !primaryRunning {
				missing = append(missing, "primary")
			}
			if !twinRunning {
				missing = append(missing, "twin")
			}
			return fmt.Errorf("Terraform Enterprise target(s) not running: %s", strings.Join(missing, ", "))
		}
	}

	return nil
}

func terraformObsTargetFilePath() (string, error) {
	targetsDir := global.ObsTargetsDir()
	if targetsDir == "" {
		return "", fmt.Errorf("failed to resolve observability targets directory")
	}
	return filepath.Join(targetsDir, terraformObsProduct+".json"), nil
}

func removeTerraformObsTargetsForScope(scope string) error {
	if scope == tfeTargetBoth {
		if err := global.RemoveObsPromTargetFile(terraformObsProduct); err != nil {
			return err
		}
		_ = global.RemoveObsPromTargetFile("terraform-bis")
		return nil
	}

	targetPath, err := terraformObsTargetFilePath()
	if err != nil {
		return err
	}
	body, err := os.ReadFile(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	type promTargetFile struct {
		Targets []string          `json:"targets"`
		Labels  map[string]string `json:"labels,omitempty"`
	}

	entries := []promTargetFile{}
	if err := json.Unmarshal(body, &entries); err != nil {
		return err
	}

	primaryTarget := "hal-tfe:9090"
	twinTarget := ""
	if layout, layoutErr := buildTFETwinLayout(); layoutErr == nil {
		if core := strings.TrimSpace(layout.CoreContainer); core != "" {
			twinTarget = core + ":9090"
		}
	}

	removeSet := map[string]bool{}
	if scope == tfeTargetPrimary {
		removeSet[primaryTarget] = true
	}
	if scope == tfeTargetTwin && twinTarget != "" {
		removeSet[twinTarget] = true
	}

	filtered := []promTargetFile{}
	for _, entry := range entries {
		remaining := []string{}
		for _, target := range entry.Targets {
			if !removeSet[target] {
				remaining = append(remaining, target)
			}
		}
		if len(remaining) > 0 {
			entry.Targets = remaining
			filtered = append(filtered, entry)
		}
	}

	if len(filtered) == 0 {
		if err := global.RemoveObsPromTargetFile(terraformObsProduct); err != nil {
			return err
		}
		return nil
	}

	buf, err := json.MarshalIndent(filtered, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(targetPath, buf, 0o644)
}

func readTerraformObsTargets() ([]string, error) {
	targetPath, err := terraformObsTargetFilePath()
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	type promTargetFile struct {
		Targets []string `json:"targets"`
	}

	entries := []promTargetFile{}
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, err
	}

	flat := []string{}
	for _, entry := range entries {
		flat = append(flat, entry.Targets...)
	}
	return flat, nil
}

var terraformObsCmd = &cobra.Command{
	Use:   "obs",
	Short: "Manage Terraform observability artifacts",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		terraformObsStatusCmd.Run(cmd, args)
	},
}

var terraformObsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create Terraform observability artifacts (Prometheus targets and Grafana dashboard)",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		target, err := normalizeTFETarget(tfeLifecycleTarget)
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		if err := ensureObsScopeRunning(engine, target); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		if !global.IsObsReady(engine) {
			fmt.Printf("❌ Observability stack is not ready. Missing: %s\n", strings.Join(global.ObsMissingComponents(engine), ", "))
			fmt.Println("   💡 Run 'hal obs create' first, then retry this command.")
			return
		}

		fmt.Println("🩺 Configuring Terraform observability artifacts...")
		for _, warning := range global.RegisterObsArtifacts(terraformObsProduct, terraformObsTargetsForScope(engine, target)) {
			fmt.Printf("⚠️  %s\n", warning)
		}
		_ = global.RemoveObsPromTargetFile("terraform-bis")
		fmt.Println("✅ Terraform observability artifacts configured.")
	},
}

var terraformObsUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Refresh Terraform observability artifacts",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		terraformObsCreateCmd.Run(cmd, args)
	},
}

var terraformObsDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete Terraform observability artifacts",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		target, err := normalizeTFETarget(tfeLifecycleTarget)
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		if err := removeTerraformObsTargetsForScope(target); err != nil {
			fmt.Printf("❌ Failed to remove Terraform observability artifacts: %v\n", err)
			return
		}

		fmt.Printf("✅ Terraform observability artifacts deleted for target: %s\n", target)
	},
}

var terraformObsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Terraform observability artifact status",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		target, err := normalizeTFETarget(tfeLifecycleTarget)
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		targets, err := readTerraformObsTargets()
		if err != nil {
			fmt.Printf("❌ Failed to read Terraform observability targets: %v\n", err)
			return
		}

		dashboardPath := filepath.Join(global.ObsDashboardsDir(), terraformObsProduct+".json")
		_, dashErr := os.Stat(dashboardPath)
		dashboardReady := dashErr == nil

		primaryTarget := "hal-tfe:9090"
		twinTarget := ""
		if layout, layoutErr := buildTFETwinLayout(); layoutErr == nil {
			if core := strings.TrimSpace(layout.CoreContainer); core != "" {
				twinTarget = core + ":9090"
			}
		}

		hasPrimary := false
		hasTwin := false
		for _, t := range targets {
			if t == primaryTarget {
				hasPrimary = true
			}
			if twinTarget != "" && t == twinTarget {
				hasTwin = true
			}
		}

		fmt.Println("Terraform Observability Status")
		fmt.Println("=============================")
		fmt.Printf("Target scope: %s\n", target)
		fmt.Printf("Obs stack:    %v\n", global.IsObsReady(engine))
		fmt.Printf("Dashboard:    %v\n", dashboardReady)

		scopeState := "not configured"
		switch target {
		case tfeTargetPrimary:
			if hasPrimary {
				scopeState = "configured"
			}
		case tfeTargetTwin:
			if hasTwin {
				scopeState = "configured"
			}
		case tfeTargetBoth:
			if hasPrimary && hasTwin {
				scopeState = "configured"
			} else if hasPrimary || hasTwin {
				scopeState = "partially configured"
			}
		}
		fmt.Printf("Targets:      %s\n", scopeState)
		if len(targets) == 0 {
			fmt.Println("Resolved target file entries: (none)")
			return
		}
		fmt.Printf("Resolved target file entries: %s\n", strings.Join(targets, ", "))
	},
}

func init() {
	bindTFETargetFlag(terraformObsCreateCmd)
	bindTFETargetFlag(terraformObsUpdateCmd)
	bindTFETargetFlag(terraformObsDeleteCmd)
	bindTFETargetFlag(terraformObsStatusCmd)

	terraformObsCmd.AddCommand(terraformObsCreateCmd)
	terraformObsCmd.AddCommand(terraformObsUpdateCmd)
	terraformObsCmd.AddCommand(terraformObsDeleteCmd)
	terraformObsCmd.AddCommand(terraformObsStatusCmd)

	Cmd.AddCommand(terraformObsCmd)
}
