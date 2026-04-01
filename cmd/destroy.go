package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var forceDestroy bool

var destroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Destroy all HAL infrastructure globally",
	Long:  "Completely tears down all Docker containers, KinD clusters, and Multipass VMs created by HAL.",
	Run: func(cmd *cobra.Command, args []string) {
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

		// 2. Synchronous Destruction Logging
		fmt.Println("♻️  Purging HAL Docker containers...")
		_ = exec.Command("sh", "-c", "docker ps -a -q --filter name=hal- | xargs -r docker rm -f").Run()

		fmt.Println("♻️  Purging HAL KinD clusters...")
		_ = exec.Command("kind", "delete", "cluster", "--name", "hal-k8s").Run()

		fmt.Println("♻️  Purging HAL Multipass VMs...")
		_ = exec.Command("sh", "-c", "multipass list --format csv | grep hal- | cut -d, -f1 | xargs -I {} multipass delete {} --purge").Run()

		fmt.Println("\n✅ All HAL infrastructure has been successfully destroyed.")
	},
}

func init() {
	destroyCmd.Flags().BoolVarP(&forceDestroy, "force", "f", false, "Force destruction without confirmation prompt")
	rootCmd.AddCommand(destroyCmd)
}
