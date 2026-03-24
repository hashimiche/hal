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
		// 1. Check if VM exists and is running
		out, err := exec.Command("multipass", "info", "hal-nomad", "--format", "csv").Output()
		if err != nil {
			fmt.Println("🔴 Nomad VM is not running. (No VM named 'hal-nomad' found)")
			return
		}

		// Extract IP and State from CSV
		lines := strings.Split(string(out), "\n")
		if len(lines) < 2 {
			return
		}
		cols := strings.Split(lines[1], ",")
		state := cols[1]
		ip := cols[2]

		if state != "Running" {
			fmt.Printf("🔴 Nomad VM exists, but is currently: %s\n", state)
			fmt.Println("   Try running: multipass start hal-nomad")
			return
		}

		fmt.Printf("🟢 VM 'hal-nomad' is Running (IP: %s).\n", ip)
		fmt.Println("🔍 Pinging Nomad API (/v1/status/leader)...")

		// 2. Ping the Nomad API
		client := http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get(fmt.Sprintf("http://%s:4646/v1/status/leader", ip))
		if err != nil {
			fmt.Printf("⚠️  VM is up, but Nomad API is unreachable: %v\n", err)
			fmt.Println("   (The Nomad agent might still be booting up or crashed.)")
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode == 200 {
			fmt.Println("\n✅ Nomad API is awake and has a cluster leader!")
			fmt.Printf("   🔗 Address: http://%s:4646\n", ip)
			fmt.Println("   Status:      Ready for workloads ")
		} else {
			fmt.Printf("⚠️  Nomad API responded with status code: %d\n", resp.StatusCode)
		}
	},
}

func init() {
	Cmd.AddCommand(nomadStatusCmd)
}
