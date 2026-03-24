package terraform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var destroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Tear down the TFE stack and wipe all local state for a fresh restart",
	Run: func(cmd *cobra.Command, args []string) {

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		fmt.Println("💥 Initializing scorched earth policy for TFE...")

		// 1. Kill the containers
		// We use a slice to ensure we catch all of them in one go
		containers := []string{"hal-tfe", "hal-tfe-db", "hal-tfe-redis", "hal-tfe-minio"}
		for _, container := range containers {
			fmt.Printf("🛑 Stopping and removing %s...\n", container)
			_ = exec.Command(engine, "rm", "-f", container).Run()
		}

		// 2. Wipe the local Cert cache
		// This ensures we don't have hostname mismatches if we changed TFE_HOSTNAME
		homeDir, _ := os.UserHomeDir()
		certDir := filepath.Join(homeDir, ".hal", "tfe-certs")
		if _, err := os.Stat(certDir); err == nil {
			fmt.Println("🧹 Wiping local TLS certificate cache...")
			_ = os.RemoveAll(certDir)
		}

		// 3. Cleanup the network
		// Docker networks can sometimes cache container IP metadata
		fmt.Println(" Pruning HAL network...")
		_ = exec.Command(engine, "network", "rm", "hal-net").Run()

		fmt.Println("✨ Environment wiped. You are ready for a clean 'hal terraform deploy'.")
	},
}

func init() {
	// Attach to the parent 'terraform' command
	Cmd.AddCommand(destroyCmd)
}
