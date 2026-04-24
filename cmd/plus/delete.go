package plus

import (
	"fmt"
	"os/exec"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete HAL Plus runtime containers",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		if global.DryRun {
			fmt.Printf("[DRY RUN] Would remove containers: %s, %s\n", halPlusContainerName, halMCPContainerName)
			fmt.Println("[DRY RUN] Would clean hal-net if unused")
			return
		}

		for _, c := range []string{halPlusContainerName, halMCPContainerName} {
			_ = exec.Command(engine, "rm", "-f", c).Run()
		}

		global.CleanNetworkIfEmpty(engine)
		fmt.Println("✅ HAL Plus runtime deleted.")
	},
}
