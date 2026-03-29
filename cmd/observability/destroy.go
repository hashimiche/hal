package observability

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

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
	Use:   "destroy",
	Short: "Destroy the Observability stack and wipe configurations",
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		fmt.Printf("⚙️  Destroying Observability stack via %s...\n", engine)

		for _, container := range obsEcosystem {
			out, err := exec.Command(engine, "rm", "-f", container).Output()
			if err == nil && string(out) != "" {
				fmt.Printf("  ✅ Destroyed container: %s\n", container)
			}
		}

		homeDir, _ := os.UserHomeDir()
		configDir := filepath.Join(homeDir, ".hal", "obs")
		if _, err := os.Stat(configDir); err == nil {
			fmt.Println("  🧹 Wiping local PLG configurations...")
			_ = os.RemoveAll(configDir)
		}

		global.CleanNetworkIfEmpty(engine)
		fmt.Println("✅ Observability environment destroyed successfully!")
	},
}

func init() {
	Cmd.AddCommand(destroyCmd)
}
