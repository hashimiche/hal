package terraform

import (
	"fmt"
	"os/exec"
	"strings"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Display the health and status of the local Terraform Enterprise environment",
	Run: func(cmd *cobra.Command, args []string) {
		target, err := normalizeTFETarget(tfeLifecycleTarget)
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("⚪ Error: %v\n", err)
			return
		}

		fmt.Println("Terraform Deployment Status")
		fmt.Println("==============================")
		fmt.Printf("Engine: %s\n", engine)
		fmt.Println("------------------------------")
		fmt.Printf("%-14s %-13s %-31s %s\n", "Product", "State", "Endpoint", "Version")
		fmt.Println("------------------------------")

		switch target {
		case tfeTargetPrimary:
			printTFETargetStatus(engine, tfeTargetPrimary)
			printTFETargetDetailedStatus(engine, tfeTargetPrimary)
		case tfeTargetTwin:
			printTFETargetStatus(engine, tfeTargetTwin)
			printTFETargetDetailedStatus(engine, tfeTargetTwin)
		case tfeTargetBoth:
			printTFETargetStatus(engine, tfeTargetPrimary)
			printTFETargetDetailedStatus(engine, tfeTargetPrimary)
			fmt.Println("------------------------------")
			printTFETargetStatus(engine, tfeTargetTwin)
			printTFETargetDetailedStatus(engine, tfeTargetTwin)
		}
	},
}

func printTFETargetDetailedStatus(engine, target string) {
	if target == tfeTargetTwin {
		printTFETwinDetailedStatus(engine)
		return
	}

	components := []struct {
		Name      string
		Container string
	}{
		{"Database (Postgres)", "hal-tfe-db"},
		{"Cache (Redis)", "hal-tfe-redis"},
		{"Object Storage (MinIO)", "hal-tfe-minio"},
		{"TFE Core (Application)", "hal-tfe"},
	}

	allRunning := true
	someExist := false

	for _, c := range components {
		out, err := exec.Command(engine, "inspect", "-f", "{{.State.Status}}", c.Container).CombinedOutput()
		status := strings.TrimSpace(string(out))

		if err != nil || strings.Contains(status, "No such object") || strings.Contains(status, "no such container") {
			fmt.Printf("  ⚪ %-23s : Not deployed\n", c.Name)
			allRunning = false
			continue
		}

		someExist = true
		if status == "running" {
			fmt.Printf("  🟢 %-23s : Active (%s)\n", c.Name, c.Container)
		} else {
			fmt.Printf("  🟡 %-23s : %s\n", c.Name, strings.ToUpper(status))
			allRunning = false
		}
	}

	workspaceReady := checkTargetWorkspaceAutomationReady(engine, tfeTargetPrimary)

	fmt.Println("\n💡 Tips:")
	if !someExist {
		fmt.Println("   To deploy a fresh Terraform Enterprise environment, run:")
		fmt.Println("   export TFE_LICENSE='<your_license_string>'")
		fmt.Println("   hal terraform create")
	} else if allRunning {
		fmt.Println("   All systems green. TFE is operational.")
		fmt.Println("   🔗 UI Address: https://tfe.localhost:8443")
		if workspaceReady {
			fmt.Println("   Workspace automation is enabled and ready for VCS-triggered runs.")
		} else {
			fmt.Println("   Enable the full VCS automation workflow with:")
			fmt.Println("   hal terraform vcs-workflow enable")
		}
	} else {
		fmt.Println("   Environment is partially degraded or stopped. To safely reset, run:")
		fmt.Println("   hal terraform create --update")
		fmt.Println("\n   Or to tear everything down completely, run:")
		fmt.Println("   hal terraform delete")
	}
}

func printTFETwinDetailedStatus(engine string) {
	layout, layoutErr := buildTFETwinLayout()
	if layoutErr != nil {
		fmt.Printf("❌ Invalid twin configuration: %v\n", layoutErr)
		return
	}

	components := []struct {
		Name      string
		Container string
	}{
		{"Shared Database (Postgres)", "hal-tfe-db"},
		{"Shared Cache (Redis)", "hal-tfe-redis"},
		{"Shared Object Storage (MinIO)", "hal-tfe-minio"},
		{"Twin TFE Core", layout.CoreContainer},
		{"Twin Ingress Proxy", layout.ProxyContainer},
	}

	allRunning := true
	someExist := false

	for _, c := range components {
		out, err := exec.Command(engine, "inspect", "-f", "{{.State.Status}}", c.Container).CombinedOutput()
		status := strings.TrimSpace(string(out))

		if err != nil || strings.Contains(status, "No such object") || strings.Contains(status, "no such container") {
			fmt.Printf("  ⚪ %-23s : Not deployed\n", c.Name)
			allRunning = false
			continue
		}

		someExist = true
		if status == "running" {
			fmt.Printf("  🟢 %-23s : Active (%s)\n", c.Name, c.Container)
		} else {
			fmt.Printf("  🟡 %-23s : %s\n", c.Name, strings.ToUpper(status))
			allRunning = false
		}
	}

	workspaceReady := checkTargetWorkspaceAutomationReady(engine, tfeTargetTwin)

	fmt.Println("\n💡 Tips:")
	if !someExist {
		fmt.Println("   To deploy the twin Terraform Enterprise environment, run:")
		fmt.Println("   export TFE_LICENSE='<your_license_string>'")
		fmt.Println("   hal terraform create --target twin")
	} else if allRunning {
		fmt.Println("   Twin systems are operational.")
		fmt.Printf("   🔗 UI Address: %s\n", layout.UIURL)
		if workspaceReady {
			fmt.Println("   Twin workspace automation is enabled and ready for VCS-triggered runs.")
		} else {
			fmt.Println("   Enable twin VCS automation workflow with:")
			fmt.Println("   hal terraform vcs-workflow enable --target twin")
		}
	} else {
		fmt.Println("   Twin environment is partially degraded or stopped. To safely reset, run:")
		fmt.Println("   hal terraform create --target twin --update")
		fmt.Println("\n   Or to tear twin resources down, run:")
		fmt.Println("   hal terraform delete --target twin")
	}
}

func printTFETargetStatus(engine, target string) {
	coreContainer, coreErr := tfeCoreContainerForTarget(target)
	if coreErr != nil {
		fmt.Printf("❌ %v\n", coreErr)
		return
	}

	endpoint := "https://tfe.localhost:8443"
	productName := "TFE"
	if target == tfeTargetTwin {
		layout, layoutErr := buildTFETwinLayout()
		if layoutErr != nil {
			fmt.Printf("❌ Invalid twin configuration: %v\n", layoutErr)
			return
		}
		endpoint = layout.UIURL
		productName = "TFE (twin)"
	}

	running := global.IsContainerRunning(engine, coreContainer)
	icon := "⚪"
	state := "Not Deployed"
	if running {
		icon = "🟢"
		state = "Running"
	}

	fmt.Printf("%s %-14s %-13s %-31s %s\n", icon, productName, state, endpoint, resolveContainerVersion(engine, coreContainer, running))
	fmt.Printf("   ↳ api-workflow %s\n", featureStateForTarget(engine, target, running, "api"))
	fmt.Printf("   ↳ vcs-workflow %s\n", featureStateForTarget(engine, target, running, "vcs"))
	fmt.Printf("   ↳ agent %s\n", featureStateForTarget(engine, target, running, "agent"))
}

func checkTargetWorkspaceAutomationReady(engine, target string) bool {
	coreContainer, err := tfeCoreContainerForTarget(target)
	if err != nil {
		return false
	}
	return global.IsContainerRunning(engine, coreContainer) && global.IsContainerRunning(engine, "hal-gitlab")
}

func featureStateForTarget(engine, target string, tfeRunning bool, feature string) string {
	if !tfeRunning {
		return "disabled"
	}

	switch feature {
	case "api":
		if target == tfeTargetTwin {
			return boolToFeatureState(global.IsContainerRunning(engine, tfeAPITwinContainer))
		}
		return boolToFeatureState(global.IsContainerRunning(engine, tfeAPIPrimaryContainer))
	case "vcs":
		return boolToFeatureState(global.IsContainerRunning(engine, "hal-gitlab"))
	case "agent":
		return boolToFeatureState(global.IsContainerRunning(engine, tfeAgentContainerNameForTarget(target)))
	default:
		return "disabled"
	}
}

func boolToFeatureState(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func resolveContainerVersion(engine, container string, running bool) string {
	if !running {
		return "-"
	}

	out, err := exec.Command(engine, "inspect", "-f", "{{.Config.Image}}", container).Output()
	if err != nil {
		return "unknown"
	}

	imageRef := strings.TrimSpace(string(out))
	if imageRef == "" {
		return "unknown"
	}
	if strings.Contains(imageRef, ":") {
		parts := strings.Split(imageRef, ":")
		return parts[len(parts)-1]
	}

	return imageRef
}

func init() {
	bindTFETargetFlag(statusCmd)
	bindTwinFlags(statusCmd)
	Cmd.AddCommand(statusCmd)
}
