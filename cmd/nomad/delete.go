package nomad

import (
	"fmt"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var destroyCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete the Nomad VM",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("⚙️  Destroying Nomad VM via Multipass...")
		if global.DryRun {
			fmt.Printf("[DRY RUN] Would execute: %s\n", global.MultipassDeleteCommand(nomadInstance))
			fmt.Println("[DRY RUN] Would remove Nomad observability target file")
			return
		}

		_ = global.DeleteMultipassVM(nomadInstance)
		if err := global.RemoveObsPromTargetFile("nomad"); err != nil {
			fmt.Printf("⚠️  Could not remove Nomad observability target file: %v\n", err)
		}

		fmt.Println("✅ Nomad environment destroyed successfully!")
	},
}

func init() {
	Cmd.AddCommand(destroyCmd)
}
