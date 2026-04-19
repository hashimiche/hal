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

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("⚪ Error: %v\n", err)
			return
		}

		fmt.Println("🔍 Checking Terraform Enterprise (FDO) Status...")
		fmt.Printf("Engine: %s\n", engine)

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
			} else {
				someExist = true
				if status == "running" {
					fmt.Printf("  🟢 %-23s : Active (%s)\n", c.Name, c.Container)
				} else {
					fmt.Printf("  🟡 %-23s : %s\n", c.Name, strings.ToUpper(status))
					allRunning = false
				}
			}
		}

		workspaceReady := checkWorkspaceAutomationReady(engine)
		fmt.Println("\n  [ Workspace Automation ]")
		if workspaceReady {
			fmt.Println("  🟢 workspace           : Ready (TFE + shared GitLab running)")
		} else {
			fmt.Println("  ⚪ workspace           : Not ready (run: hal terraform workspace enable)")
		}

		// Smart assistant guidance
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
				fmt.Println("   hal terraform workspace enable")
			}
		} else {
			fmt.Println("   Environment is partially degraded or stopped. To safely reset, run:")
			fmt.Println("   hal terraform create --force")
			fmt.Println("\n   Or to tear everything down completely, run:")
			fmt.Println("   hal terraform delete")
		}
	},
}

func checkWorkspaceAutomationReady(engine string) bool {
	tfeOut, tfeErr := exec.Command(engine, "inspect", "-f", "{{.State.Status}}", "hal-tfe").Output()
	gitlabOut, gitlabErr := exec.Command(engine, "inspect", "-f", "{{.State.Status}}", "hal-gitlab").Output()

	if tfeErr != nil || gitlabErr != nil {
		return false
	}

	return strings.TrimSpace(string(tfeOut)) == "running" && strings.TrimSpace(string(gitlabOut)) == "running"
}

func init() {
	Cmd.AddCommand(statusCmd)
}
