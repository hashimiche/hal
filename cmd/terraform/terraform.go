package terraform

import "github.com/spf13/cobra"

// Cmd is the exported base command for Terraform
var Cmd = &cobra.Command{
	Use:   "terraform",
	Short: "Manage local Terraform Enterprise deployments",
	Run: func(cmd *cobra.Command, args []string) {
		// Default to the Smart Status view if no subcommand is given
		statusCmd.Run(cmd, args)
	},
}
