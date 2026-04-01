package consul

import (
	"fmt"
	"os/exec"
	"strings"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var consulStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the status of the Consul deployment",
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		fmt.Println("🔍 Checking Consul Control Plane Status...")
		fmt.Println()

		out, err := exec.Command(engine, "inspect", "-f", "{{.State.Status}}", "hal-consul").Output()
		status := strings.TrimSpace(string(out))

		if err != nil {
			fmt.Println("  ⚪ hal-consul : Down (hal consul deploy to start)")
		} else if status == "running" {
			fmt.Println("  ✅ hal-consul : Up   (http://consul.localhost:8500)")
		} else {
			fmt.Printf("  ⚠️  hal-consul : %s\n", strings.ToUpper(status))
		}
	},
}

func init() {
	Cmd.AddCommand(consulStatusCmd)
}
