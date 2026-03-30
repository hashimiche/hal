package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version will be overwritten by GoReleaser during the build process!
var Version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of HAL",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("🔴 HAL - Hashicorp Academy Labs")
		fmt.Printf("   Version: %s\n", Version)
	},
}

func init() {
	// This enables the `hal version` subcommand
	rootCmd.AddCommand(versionCmd)

	// This enables the `hal --version` and `hal -v` flags magically!
	// (Cobra intercepts these flags and prints rootCmd.Version)
	rootCmd.Version = Version
}
