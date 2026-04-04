package nomad

import (
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var nomadStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the status of the local Nomad cluster",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("🔍 Checking Nomad Cluster Status...")
		fmt.Println()
		fmt.Println("  [ Multipass Infrastructure ]")

		out, err := exec.Command("multipass", "info", "hal-nomad", "--format", "csv").Output()
		if err != nil {
			fmt.Println("  ⚪ Nomad VM : Down (hal nomad deploy to start)")
			return
		}

		lines := strings.Split(string(out), "\n")
		if len(lines) < 2 {
			return
		}
		cols := strings.Split(lines[1], ",")
		state := cols[1]
		ip := cols[2]

		if state != "Running" {
			fmt.Printf("  🟡 Nomad VM : %s (multipass start hal-nomad)\n", strings.ToUpper(state))
			return
		}

		fmt.Printf("  🟢 Nomad VM : Up (IP: %s)\n", ip)
		fmt.Println("\n  [ Nomad API Health ]")

		client := http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get(fmt.Sprintf("http://%s:4646/v1/status/leader", ip))

		if err != nil {
			fmt.Println("  🟡 Nomad API: Unreachable (Agent crashed or booting)")
		} else {
			defer resp.Body.Close()
			if resp.StatusCode == 200 {
				fmt.Printf("  🟢 Nomad API: Ready (http://%s:4646)\n", ip)
			} else {
				fmt.Printf("  🟡 Nomad API: Error (HTTP %d)\n", resp.StatusCode)
			}
		}

		fmt.Println("\n💡 Tip: Run 'hal nomad deploy' to start/recover, then 'hal nomad status' to verify.")
	},
}

func init() {
	Cmd.AddCommand(nomadStatusCmd)
}
