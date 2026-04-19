package vault

import "github.com/spf13/cobra"

// Cmd is the exported base command for Vault
var Cmd = &cobra.Command{
	Use:   "vault",
	Short: "Manage local HashiCorp Vault deployments and integrations",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		// Default to the Smart Status view if no subcommand is given
		vaultStatusCmd.Run(cmd, args)
	},
}
