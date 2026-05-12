package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var autoApprove bool

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
			fmt.Println("[DRY RUN] Would remove HAL MCP config and managed binary artifacts")
			fmt.Println("[DRY RUN] Would remove hal-net Docker network")
			return
		}

		// 1. The Confirmation Prompt
		if !autoApprove {
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
		fmt.Printf("   - Obs state:                 %s\n", result.ObsStatus)
		fmt.Printf("   - MCP artifacts:             %s\n", result.MCPStatus)
		fmt.Printf("   - hal-net:                   %s\n", result.NetworkStatus)
		if len(result.Warnings) > 0 {
			fmt.Println("\n⚠️  Teardown warnings:")
			for _, warning := range result.Warnings {
				fmt.Printf("   - %s\n", warning)
			}
		}
		if len(result.NetworkBlockers) > 0 {
			fmt.Println("\n❌ hal-net could not be removed — the following containers are still attached:")
			for _, name := range result.NetworkBlockers {
				fmt.Printf("   - %s\n", name)
			}
			fmt.Println("   Stop or remove them, then re-run: hal delete")
			os.Exit(1)
		}
	},
}

func init() {
	destroyCmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt")
	rootCmd.AddCommand(destroyCmd)
}
