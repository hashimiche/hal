package nomad

import (
	"fmt"
	"os/exec"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var destroyCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete the Nomad VM",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("⚙️  Destroying Nomad VM via Multipass...")
		if global.DryRun {
			fmt.Println("[DRY RUN] Would execute: multipass delete hal-nomad")
			fmt.Println("[DRY RUN] Would execute: multipass purge")
			fmt.Println("[DRY RUN] Would remove Nomad observability target file")
			return
		}

		_ = exec.Command("multipass", "delete", "hal-nomad").Run()
		_ = exec.Command("multipass", "purge").Run()
		if err := global.RemoveObsPromTargetFile("nomad"); err != nil {
			fmt.Printf("⚠️  Could not remove Nomad observability target file: %v\n", err)
		}

		fmt.Println("✅ Nomad environment destroyed successfully!")
	},
}

func init() {
	Cmd.AddCommand(destroyCmd)
}
