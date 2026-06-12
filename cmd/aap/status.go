package aap

import (
	"fmt"
	"os/exec"
	"strings"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var aapStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the status of the AAP runtime",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("⚪ Error: %v\n", err)
			return
		}

		out, err := exec.Command(engine, "inspect", "-f", "{{.State.Status}}", aapContainerName).Output()
		status := strings.TrimSpace(string(out))
		endpoint := aapStatusEndpoint(engine)

		fmt.Println("AAP Runtime Status")
		fmt.Println("==================")
		fmt.Printf("Engine: %s\n", engine)
		if err != nil {
			fmt.Printf("Container: %s (down)\n", aapContainerName)
			fmt.Printf("Endpoint:  %s\n", endpoint)
			fmt.Println("💡 Tip: Run 'hal aap create' to start it.")
			return
		}

		if status == "running" {
			fmt.Printf("Container: %s (running)\n", aapContainerName)
			fmt.Printf("Endpoint:  %s\n", endpoint)
			fmt.Println("💡 Tip: Run 'hal aap update' after changing image or port flags.")
			return
		}

		fmt.Printf("Container: %s (%s)\n", aapContainerName, status)
		fmt.Printf("Endpoint:  %s\n", endpoint)
		fmt.Println("💡 Tip: Run 'hal aap update' to reconcile, or 'hal aap delete' then 'hal aap create'.")
	},
}

func aapStatusEndpoint(engine string) string {
	out, err := exec.Command(
		engine,
		"inspect",
		"-f",
		"{{(index (index .NetworkSettings.Ports \"443/tcp\") 0).HostPort}}",
		aapContainerName,
	).Output()
	if err != nil {
		return "https://aap.localhost"
	}

	hostPort := strings.TrimSpace(string(out))
	if hostPort == "" || hostPort == "<no value>" || hostPort == "443" {
		return "https://aap.localhost"
	}

	return fmt.Sprintf("https://aap.localhost:%s", hostPort)
}

func init() {
	Cmd.AddCommand(aapStatusCmd)
}
