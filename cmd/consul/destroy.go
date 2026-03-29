package consul

import (
	"fmt"
	"os/exec"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var destroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Destroy the local Consul server",
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		fmt.Printf("⚙️  Destroying Consul via %s...\n", engine)

		out, err := exec.Command(engine, "rm", "-f", "hal-consul").Output()
		if err == nil && string(out) != "" {
			fmt.Println("  ✅ Destroyed container: hal-consul")
		}

		global.CleanNetworkIfEmpty(engine)
		fmt.Println("✅ Consul environment destroyed successfully!")
	},
}

func init() {
	Cmd.AddCommand(destroyCmd)
}
