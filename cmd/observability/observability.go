package observability

import "github.com/spf13/cobra"

var Cmd = &cobra.Command{
	Use:   "obs",
	Short: "Manage the local Observability stack (Prometheus, Grafana, Loki, Tempo)",
}
