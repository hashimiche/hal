package boundary

import (
	"fmt"
	"os/exec"
	"strings"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the status of Boundary and its targets",
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("⚪ Error: %v\n", err)
			return
		}

		fmt.Println("🔍 Analyzing HashiCorp Boundary Ecosystem...")
		fmt.Printf("Engine: %s\n", engine)
		fmt.Println()

		// 1. Core Services
		fmt.Println("  [ Control Plane ]")
		cores := []struct {
			Name      string
			Container string
		}{
			{"Backend DB", "hal-boundary-backend"},
			{"Controller/Worker", "hal-boundary"},
		}

		allCoresRunning := true
		for _, c := range cores {
			out, err := exec.Command(engine, "inspect", "-f", "{{.State.Status}}", c.Container).Output()
			status := strings.TrimSpace(string(out))

			if err != nil {
				fmt.Printf("  ⚪ %-18s : Down\n", c.Name)
				allCoresRunning = false
			} else if status == "running" {
				fmt.Printf("  🟢 %-18s : Up\n", c.Name)
			} else {
				fmt.Printf("  🟡 %-18s : %s\n", c.Name, strings.ToUpper(status))
				allCoresRunning = false
			}
		}

		// 2. Targets
		fmt.Println("\n  [ Target Ecosystem ]")

		// DB Target
		dbOut, err := exec.Command(engine, "inspect", "-f", "{{.State.Status}}", "hal-boundary-target-mariadb").Output()
		if err == nil && strings.TrimSpace(string(dbOut)) == "running" {
			fmt.Println("  🟢 MariaDB Target   : Up (hal boundary mariadb disable)")
		} else {
			fmt.Println("  ⚪ MariaDB Target   : Down (hal boundary mariadb enable)")
		}

		vmOut, vmErr := exec.Command("multipass", "info", "hal-boundary-ssh", "--format", "csv").Output()
		if vmErr == nil && strings.Contains(string(vmOut), "Running") {
			ip := extractMultipassIP(string(vmOut))
			fmt.Printf("  🟢 Ubuntu SSH Target: Up (IP: %s) (hal boundary ssh disable)\n", ip)
		} else {
			fmt.Println("  ⚪ Ubuntu SSH Target: Down (hal boundary ssh enable)")
		}

		fmt.Println("\n💡 Tips:")
		if !allCoresRunning {
			fmt.Println("   Run 'hal boundary create' to bring the Control Plane online.")
		} else {
			fmt.Println("   Control Plane is ready. Manage targets with 'hal boundary <target> enable|disable|update'.")
		}
		fmt.Println("   Run 'hal boundary status' after changes to verify target readiness.")
	},
}

// Left here package-wide so ssh.go can safely use it!
func extractMultipassIP(csvData string) string {
	lines := strings.Split(csvData, "\n")
	if len(lines) > 1 {
		cols := strings.Split(lines[1], ",")
		if len(cols) > 2 {
			return cols[2]
		}
	}
	return "127.0.0.1"
}

func init() {
	Cmd.AddCommand(statusCmd)
}
