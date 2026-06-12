package aap

import "github.com/spf13/cobra"

// Cmd is the exported base command for AAP.
var Cmd = &cobra.Command{
	Use:   "aap",
	Short: "Manage the local AAP runtime",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		aapStatusCmd.Run(cmd, args)
	},
}
