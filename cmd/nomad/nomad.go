package nomad

import "github.com/spf13/cobra"

var Cmd = &cobra.Command{
	Use:   "nomad",
	Short: "Manage the local Nomad cluster via Multipass",
	Run: func(cmd *cobra.Command, args []string) {
		nomadStatusCmd.Run(cmd, args)
	},
}
