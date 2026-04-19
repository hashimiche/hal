package boundary

import "github.com/spf13/cobra"

var Cmd = &cobra.Command{
	Use:   "boundary",
	Short: "Manage local HashiCorp Boundary deployments",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		statusCmd.Run(cmd, args)
	},
}
