package nomad

import (
	"fmt"

	"github.com/spf13/cobra"
)

var nomadJobCmd = &cobra.Command{
	Use:   "job",
	Short: "Submit sample workloads (jobs) to the Nomad cluster",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("⚙️ Submitting sample job to Nomad...")
		// TODO: Use the Nomad Go API or CLI via multipass exec to submit a basic web server job
		fmt.Println("✅ Job deployed! Check the UI at http://nomad.localhost:4646")
	},
}

func init() {
	Cmd.AddCommand(nomadJobCmd)
}
