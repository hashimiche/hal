package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var forceDestroy bool

var destroyCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete all HAL infrastructure globally",
	Long:  "Completely tears down all Docker containers, KinD clusters, and Multipass VMs created by HAL.",
	Run: func(cmd *cobra.Command, args []string) {
		if global.DryRun {
			fmt.Println("[DRY RUN] Would delete HAL KinD clusters")
			fmt.Println("[DRY RUN] Would remove HAL containers on active Docker/Podman engines")
			fmt.Println("[DRY RUN] Would delete HAL Multipass VMs and purge")
			fmt.Println("[DRY RUN] Would remove local observability state")
			return
		}

		// 1. The Confirmation Prompt
		if !forceDestroy {
			fmt.Print("⚠️  WARNING: This will destroy ALL HAL containers, clusters, and VMs. Are you sure? [y/N]: ")
			reader := bufio.NewReader(os.Stdin)
			response, _ := reader.ReadString('\n')
			response = strings.ToLower(strings.TrimSpace(response))

			if response != "y" && response != "yes" {
				fmt.Println("🛑 Global destruction aborted.")
				return
			}
		}

		fmt.Println("\n🛑 Initiating global infrastructure teardown...")
		result := runGlobalTeardown()

		fmt.Println("\n✅ All HAL infrastructure has been successfully destroyed.")
		fmt.Printf("   - Docker containers removed: %d\n", result.DockerContainersRemoved)
		fmt.Printf("   - KinD clusters deleted:     %d\n", result.KindClustersDeleted)
		fmt.Printf("   - Multipass VMs deleted:     %d\n", result.MultipassVMsDeleted)
		fmt.Printf("   - Obs state cleaned:         %t\n", result.ObsStateCleaned)
		if len(result.Warnings) > 0 {
			fmt.Println("\n⚠️  Teardown warnings:")
			for _, warning := range result.Warnings {
				fmt.Printf("   - %s\n", warning)
			}
		}
	},
}

func init() {
	destroyCmd.Flags().BoolVarP(&forceDestroy, "force", "f", false, "Force destruction without confirmation prompt")
	rootCmd.AddCommand(destroyCmd)
}
