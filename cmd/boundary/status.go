package boundary

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the status of Boundary and targets",
	Run: func(cmd *cobra.Command, args []string) {

		fmt.Println("  Boundary Environment Status:")
		fmt.Println("==============================")

		// Check Boundary Core
		out, err := exec.Command("docker", "ps", "-q", "-f", "name=hal-boundary$").Output()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			fmt.Println("🟢 Boundary Core: Running (http://127.0.0.1:9200)")
		} else {
			fmt.Println("🔴 Boundary Core: Offline")
		}

		// Check DB Target
		dbOut, dbErr := exec.Command("docker", "ps", "-q", "-f", "name=hal-boundary-db").Output()
		if dbErr == nil && strings.TrimSpace(string(dbOut)) != "" {
			fmt.Println("🟢 DB Target:     Running (Postgres on 5432)")
		} else {
			fmt.Println("🔴 DB Target:     Offline")
		}

		// Check SSH Target
		vmOut, vmErr := exec.Command("multipass", "info", "hal-boundary-ssh", "--format", "csv").Output()
		if vmErr == nil && strings.Contains(string(vmOut), "Running") {
			ip := extractMultipassIP(string(vmOut))
			fmt.Printf("🟢 SSH Target:    Running (VM IP: %s)\n", ip)
		} else {
			fmt.Println("🔴 SSH Target:    Offline")
		}
		fmt.Println("==============================")
	},
}

func init() {
	Cmd.AddCommand(statusCmd)
}
