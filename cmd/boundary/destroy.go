package boundary

import (
	"fmt"
	"os/exec"
	"strings"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var boundaryEcosystem = []string{
	"hal-boundary",
	"hal-boundary-backend",
	"hal-boundary-target-mariadb",
}

var destroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Destroy Boundary and all associated target resources",
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		fmt.Printf("⚙️  Destroying Boundary ecosystem via %s...\n", engine)
		if global.DryRun {
			fmt.Printf("[DRY RUN] Would remove containers: %s\n", strings.Join(boundaryEcosystem, ", "))
			fmt.Println("[DRY RUN] Would delete and purge Multipass VM hal-boundary-ssh")
			fmt.Println("[DRY RUN] Would clean hal-net if unused")
			fmt.Println("[DRY RUN] Would remove Boundary observability target file")
			return
		}

		for _, container := range boundaryEcosystem {
			out, err := exec.Command(engine, "rm", "-f", container).Output()
			if err == nil && string(out) != "" {
				fmt.Printf("  ✅ Destroyed container: %s\n", container)
			}
		}

		// Handle Multipass target cleanup gracefully
		_ = exec.Command("multipass", "delete", "hal-boundary-ssh").Run()
		_ = exec.Command("multipass", "purge").Run()
		fmt.Println("  ✅ Destroyed SSH VM (if it existed)")

		global.CleanNetworkIfEmpty(engine)
		if err := global.RemoveObsPromTargetFile("boundary"); err != nil {
			fmt.Printf("⚠️  Could not remove Boundary observability target file: %v\n", err)
		}
		fmt.Println("✅ Boundary environment destroyed successfully!")
	},
}

func init() {
	Cmd.AddCommand(destroyCmd)
}
