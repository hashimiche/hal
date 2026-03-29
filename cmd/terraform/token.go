package terraform

import (
	"fmt"
	"os/exec"
	"strings"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var tokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Retrieve the Initial Admin Creation Token (IACT) from a running TFE instance",
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		fmt.Println("🔑 Reaching into TFE to retrieve the Admin Token...")

		out, err := exec.Command(engine, "exec", "hal-tfe", "tfectl", "admin", "token").CombinedOutput()
		if err != nil {
			fmt.Printf("❌ Failed to retrieve token. Is TFE fully booted? Error: %v\n", err)
			if strings.Contains(string(out), "No such container") {
				fmt.Println("💡 It looks like hal-tfe isn't running. Run 'hal terraform status' to verify.")
			}
			return
		}

		token := strings.TrimSpace(string(out))

		fmt.Println("\n✅ Initial Admin Creation Token Retrieved!")
		fmt.Println("---------------------------------------------------------")
		fmt.Printf("   Token: %s\n\n", token)
		fmt.Println("🔗 Click here to complete the setup:")
		fmt.Printf("   https://tfe.localhost:8443/admin/account/new?token=%s\n", token)
		fmt.Println("---------------------------------------------------------")
	},
}

func init() {
	Cmd.AddCommand(tokenCmd)
}
