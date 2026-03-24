package terraform

import "github.com/spf13/cobra"

// Cmd is the exported base command for Terraform
var Cmd = &cobra.Command{
	Use:   "terraform",
	Short: "Manage local Terraform deployments",
}
