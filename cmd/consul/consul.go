package consul

import "github.com/spf13/cobra"

// Cmd is the exported base command for the consul noun
var Cmd = &cobra.Command{
	Use:   "consul",
	Short: "Manage local standalone Consul deployments",
}
