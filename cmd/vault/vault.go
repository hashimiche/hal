package vault

import "github.com/spf13/cobra"

// Cmd is the exported base command for Vault
var Cmd = &cobra.Command{
	Use:   "vault",
	Short: "Manage local Vault deployments",
}
