package aap

import (
	"fmt"
	"os/exec"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var aapDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete the local AAP runtime",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		if global.DryRun {
			fmt.Printf("[DRY RUN] Would execute: %s rm -f %s\n", engine, aapContainerName)
			fmt.Println("[DRY RUN] Would clean hal-net if unused")
			return
		}

		_ = exec.Command(engine, "rm", "-f", aapContainerName).Run()
		global.CleanNetworkIfEmpty(engine)
		global.RefreshHalHealth(engine)
		fmt.Println("✅ AAP runtime deleted.")
	},
}

func init() {
	Cmd.AddCommand(aapDeleteCmd)
}
