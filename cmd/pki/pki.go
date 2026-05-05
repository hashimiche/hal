package pki

import "github.com/spf13/cobra"

// Cmd is the exported top-level pki command.
var Cmd = &cobra.Command{
	Use:   "pki",
	Short: "Manage Vault PKI secrets engines (Root CA, Intermediate CA, cert issuance)",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		pkiStatusCmd.Run(cmd, args)
	},
}
