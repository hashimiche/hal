package boundary

import (
	"fmt"
	"os/exec"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var destroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Destroy Boundary and any associated backend/target databases or VMs",
	Run: func(cmd *cobra.Command, args []string) {

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		fmt.Println("💥 Destroying Boundary environment...")

		if global.DryRun {
			fmt.Println("[DRY RUN] Would remove Docker containers and Multipass VMs")
			return
		}

		// Nuke Boundary Core and BOTH Postgres databases in one swift command
		_ = exec.Command(engine, "rm", "-f", "hal-boundary", "hal-boundary-backend", "hal-boundary-target-db").Run()

		// Nuke SSH Target
		_ = exec.Command("multipass", "delete", "hal-boundary-ssh").Run()
		_ = exec.Command("multipass", "purge").Run()

		// Attempt to clean up the global grid
		global.CleanNetworkIfEmpty(engine)

		fmt.Println("✅ Boundary and all associated targets destroyed successfully!")
	},
}

func init() {
	Cmd.AddCommand(destroyCmd)
}
