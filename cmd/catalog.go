package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var catalogCmd = &cobra.Command{
	Use:   "catalog",
	Short: "List all available products you can deploy",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Available products:")
		fmt.Println(" - vault    (Editions: ce, ent, hcp)")
		fmt.Println(" - nomad    (Options: --with-consul)")
		fmt.Println(" - consul")
		fmt.Println(" - tf       (Editions: tfe, hcp)")
		fmt.Println(" - boundary")
	},
}

func init() {
	rootCmd.AddCommand(catalogCmd)
}