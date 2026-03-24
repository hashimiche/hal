package nomad

import "github.com/spf13/cobra"

// Cmd is the exported base command for Nomad
var Cmd = &cobra.Command{
	Use:   "nomad",
	Short: "Manage local Nomad deployments",
}
