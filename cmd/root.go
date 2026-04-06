package cmd

import (
	"fmt"
	"os"

	"hal/cmd/boundary"
	"hal/cmd/consul"
	"hal/cmd/mcp"
	"hal/cmd/nomad"
	"hal/cmd/observability"
	"hal/cmd/terraform"
	"hal/cmd/vault"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var (
	debug  bool
	dryRun bool
)

var rootCmd = &cobra.Command{
	Use:   "hal",
	Short: "HAL: HashiCorp Academy Lab",
	Long:  `HAL is a CLI tool to rapidly spin up HashiCorp environments for enablement, training, and testing.`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	// Point the flags directly to the memory addresses of your global variables
	rootCmd.PersistentFlags().BoolVarP(&global.Debug, "debug", "", false, "Enable debug output")
	rootCmd.PersistentFlags().BoolVarP(&global.DryRun, "dry-run", "", false, "Simulate the execution without changing state")

	rootCmd.AddCommand(vault.Cmd)
	rootCmd.AddCommand(nomad.Cmd)
	rootCmd.AddCommand(boundary.Cmd)
	rootCmd.AddCommand(consul.Cmd)
	rootCmd.AddCommand(mcp.Cmd)
	rootCmd.AddCommand(terraform.Cmd)
	rootCmd.AddCommand(observability.Cmd)
}
