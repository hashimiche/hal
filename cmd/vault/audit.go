package vault

import (
	"github.com/spf13/cobra"
)

var vaultAuditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Manage Vault audit devices",
}

func init() {
	Cmd.AddCommand(vaultAuditCmd)
}
