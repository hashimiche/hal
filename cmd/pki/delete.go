package pki

import (
	"fmt"
	"hal/internal/global"

	"github.com/spf13/cobra"
)

var (
	pkiDeleteRootMount string
	pkiDeleteIntMount  string
)

var pkiDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Disable and remove Vault PKI engines and associated policy",
	Run: func(cmd *cobra.Command, args []string) {
		if global.DryRun {
			fmt.Printf("[DRY RUN] Would unmount '%s' and '%s' from Vault\n", pkiDeleteRootMount, pkiDeleteIntMount)
			fmt.Println("[DRY RUN] Would delete policy 'hal-pki-issuer'")
			return
		}

		client, err := getVaultClient()
		if err != nil {
			fmt.Printf("⚠️  Cannot reach Vault: %v\n", err)
			fmt.Println("   If Vault is down, PKI engines will be gone on next 'hal vault delete'.")
			return
		}

		fmt.Println("🗑️  Removing Vault PKI engines...")

		if err := client.Sys().Unmount(pkiDeleteRootMount); err != nil {
			fmt.Printf("  ⚠️  %s: %v\n", pkiDeleteRootMount, err)
		} else {
			fmt.Printf("  ✅ Unmounted '%s'\n", pkiDeleteRootMount)
		}

		if err := client.Sys().Unmount(pkiDeleteIntMount); err != nil {
			fmt.Printf("  ⚠️  %s: %v\n", pkiDeleteIntMount, err)
		} else {
			fmt.Printf("  ✅ Unmounted '%s'\n", pkiDeleteIntMount)
		}

		_ = client.Sys().DeletePolicy("hal-pki-issuer")
		fmt.Println("  ✅ Policy 'hal-pki-issuer' deleted.")

		fmt.Println("\n✅ PKI engines removed.")
		fmt.Println("💡 Next Step: hal pki create")
	},
}

func init() {
	pkiDeleteCmd.Flags().StringVar(&pkiDeleteRootMount, "root-mount", "pki-root", "Vault mount path for the Root CA to remove")
	pkiDeleteCmd.Flags().StringVar(&pkiDeleteIntMount, "int-mount", "pki-int", "Vault mount path for the Intermediate CA to remove")
	Cmd.AddCommand(pkiDeleteCmd)
}
