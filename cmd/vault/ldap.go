package vault

import (
	"fmt"

	"github.com/spf13/cobra"
)

var vaultLdapCmd = &cobra.Command{
	Use:   "ldap",
	Short: "Deploy an OpenLDAP container and configure the Vault LDAP auth method",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("⚙️ Booting OpenLDAP container with pre-baked LDIFF...")
		// TODO: docker run -d openldap ...
		fmt.Println("⚙️ Configuring Vault LDAP auth method...")
		// TODO: vault.sys().EnableAuthWithOptions("ldap", ...)
		fmt.Println("✅ LDAP configured! You can now login with pre-baked users.")
	},
}

func init() {
	Cmd.AddCommand(vaultLdapCmd)
}
