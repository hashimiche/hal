package observability

import "github.com/spf13/cobra"

var Cmd = &cobra.Command{
	Use:   "obs",
	Short: "Manage the local PLG Observability stack",
	Run: func(cmd *cobra.Command, args []string) {
		statusCmd.Run(cmd, args)
	},
}
