package consul

import (
	"fmt"
	"hal/internal/global"

	"github.com/spf13/cobra"
)

var consulStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the status of the Consul deployment",
	Run: func(cmd *cobra.Command, args []string) {
		if global.DryRun {
			fmt.Println("[DRY RUN] Would check Consul status.")
			return
		}

		if global.Debug {
			fmt.Println("[DEBUG] Querying local system and HCP API for Consul instances...")
		}

		fmt.Println("🔍 Checking Consul status...")

		// TODO: This is where we will eventually wire up the real logic.
		// e.g., using the Docker SDK to see if the container is running,
		// or pinging the local Consul API (http://localhost:8200/v1/sys/health)

		// Mock output for the vibe coding phase:
		fmt.Println("   - Local (ent): 🟢 Running (http://localhost:8200)")
		fmt.Println("   - HCP:         🔴 Not Configured")
	},
}

func init() {
	// Bind this status verb to the parent consulCmd
	Cmd.AddCommand(consulStatusCmd)
}
