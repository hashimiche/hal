package nomad

import (
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"
)

var destroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Destroy the Nomad VM",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("⚙️  Destroying Nomad VM via Multipass...")

		_ = exec.Command("multipass", "delete", "hal-nomad").Run()
		_ = exec.Command("multipass", "purge").Run()

		fmt.Println("✅ Nomad environment destroyed successfully!")
	},
}

func init() {
	Cmd.AddCommand(destroyCmd)
}
