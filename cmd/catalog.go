package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var catalogCmd = &cobra.Command{
	Use:   "catalog",
	Short: "List all available products you can deploy",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("\n=========================================================")
		fmt.Println("               🪐 HAL 9000 PRODUCT CATALOG               ")
		fmt.Println("=========================================================")

		fmt.Println("\n🛡️  SECURITY & ACCESS")
		fmt.Println("   - vault     (Editions: ce, ent)")
		fmt.Println("   - boundary  (Editions: ce, ent)")

		fmt.Println("\n🏗️  INFRASTRUCTURE & ORCHESTRATION")
		fmt.Println("   - nomad     (Options: --with-consul)")
		fmt.Println("   - consul    (Standard deployment)")
		fmt.Println("   - terraform (Editions: ent)")

		fmt.Println("\n=========================================================")
		fmt.Println("💡 Tip: Run 'hal <product> deploy --help' to get started.")
		fmt.Println("=========================================================")
	},
}

func init() {
	rootCmd.AddCommand(catalogCmd)
}
