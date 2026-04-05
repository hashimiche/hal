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
		if global.DryRun {
			fmt.Printf("[DRY RUN] Would execute: %s rm -f hal-consul\n", engine)
			fmt.Println("[DRY RUN] Would clean hal-net if unused")
			fmt.Println("[DRY RUN] Would remove Consul observability target file")
			return
		}

		out, err := exec.Command(engine, "rm", "-f", "hal-consul").Output()
		if err == nil && string(out) != "" {
			fmt.Println("  ✅ Destroyed container: hal-consul")
		}

		global.CleanNetworkIfEmpty(engine)
		if err := global.RemoveObsPromTargetFile("consul"); err != nil {
			fmt.Printf("⚠️  Could not remove Consul observability target file: %v\n", err)
		}
		fmt.Println("✅ Consul environment destroyed successfully!")
	},
}

func init() {
	Cmd.AddCommand(destroyCmd)
}
