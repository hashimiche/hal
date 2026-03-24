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

		// 🎯 THE FIX: Changed 'tfe-admin retrieve-iact' to 'tfectl admin token'
		out, err := exec.Command(engine, "exec", "hal-tfe", "tfectl", "admin", "token").CombinedOutput()
		if err != nil {
			fmt.Printf("❌ Failed to retrieve token. Is TFE fully booted? Error: %v\n", err)
			if strings.Contains(string(out), "No such container") {
				fmt.Println("💡 It looks like hal-tfe isn't running. Did you run 'hal terraform deploy'?")
			}
			return
		}

		fmt.Println("\n✅ Initial Admin Creation Token:")
		fmt.Println("--------------------------------------------------")
		fmt.Println(strings.TrimSpace(string(out)))
		fmt.Println("--------------------------------------------------")
		fmt.Printf("🔗 Paste this at: https://tfe.localhost/admin/account/new?token=%s\n", strings.TrimSpace(string(out)))
	},
}

func init() {
	Cmd.AddCommand(tokenCmd)
}
