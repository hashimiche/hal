package nomad

import (
	"fmt"
	"hal/internal/global"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var nomadDestroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Destroy the Nomad Multipass VM",
	Run: func(cmd *cobra.Command, args []string) {
		if err := exec.Command("multipass", "version").Run(); err != nil {
			fmt.Println("❌ Error: Multipass is not installed.")
			return
		}

		fmt.Println("💥 Destroying Nomad VM...")

		if global.DryRun {
			fmt.Println("[DRY RUN] Would execute: multipass delete hal-nomad && multipass purge")
			return
		}

		// 1. Delete the VM
		out, err := exec.Command("multipass", "delete", "hal-nomad").CombinedOutput()
		if err != nil {
			if strings.Contains(string(out), "does not exist") {
				fmt.Println("⚠️  No VM named 'hal-nomad' found. It might already be destroyed.")
				return
			}
			fmt.Printf("❌ Failed to delete VM: %v\nOutput: %s\n", err, string(out))
			return
		}

		// 2. Purge it to free disk space
		_ = exec.Command("multipass", "purge").Run()

		fmt.Println("✅ Nomad VM destroyed and purged successfully!")
	},
}

func init() {
	Cmd.AddCommand(nomadDestroyCmd)
}
