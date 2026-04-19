package observability

import (
	"fmt"
	"os/exec"
	"strings"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var obsEcosystem = []string{
	"hal-grafana",
	"hal-promtail",
	"hal-loki",
	"hal-prometheus",
}

var destroyCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete the Observability stack and wipe configurations",
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		fmt.Printf("⚙️  Destroying Observability stack via %s...\n", engine)
		if global.DryRun {
			fmt.Printf("[DRY RUN] Would remove containers: %s\n", strings.Join(obsEcosystem, ", "))
			fmt.Println("[DRY RUN] Would wipe local PLG configurations")
			fmt.Println("[DRY RUN] Would clean hal-net if unused")
			return
		}

		for _, container := range obsEcosystem {
			out, err := exec.Command(engine, "rm", "-f", container).Output()
			if err == nil && string(out) != "" {
				fmt.Printf("  ✅ Destroyed container: %s\n", container)
			}
		}

		fmt.Println("  🧹 Wiping local PLG configurations...")
		if err := global.RemoveObsState(); err != nil {
			fmt.Printf("  ⚠️  Failed to wipe local PLG configurations: %v\n", err)
		}

		global.CleanNetworkIfEmpty(engine)
		fmt.Println("✅ Observability environment destroyed successfully!")
	},
}

func init() {
	Cmd.AddCommand(destroyCmd)
}
