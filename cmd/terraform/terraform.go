package terraform

import "github.com/spf13/cobra"

// Cmd is the exported base command for Terraform
var Cmd = &cobra.Command{
	Use:     "terraform",
	Aliases: []string{"tf"},
	Short:   "Manage local Terraform Enterprise deployments",
	Args:    cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		// Default to the Smart Status view if no subcommand is given
		statusCmd.Run(cmd, args)
	},
}
