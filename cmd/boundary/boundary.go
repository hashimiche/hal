package boundary

import "github.com/spf13/cobra"

// Cmd is the exported base command for the boundary noun
var Cmd = &cobra.Command{
	Use:   "boundary",
	Short: "Manage local Boundary zero-trust deployments",
}
