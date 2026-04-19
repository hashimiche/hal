package consul

import "github.com/spf13/cobra"

var Cmd = &cobra.Command{
	Use:   "consul",
	Short: "Manage the local Consul Control Plane",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		consulStatusCmd.Run(cmd, args)
	},
}
