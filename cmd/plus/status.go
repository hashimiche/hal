package plus

import (
	"fmt"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show HAL Plus runtime status",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		plusState := containerState(engine, halPlusContainerName)
		mcpState := containerState(engine, halMCPContainerName)
		plusImagePresent := imageExists(engine, plusImage)
		mcpImagePresent := imageExists(engine, mcpImage)

		uiReady := plusState == "running" && endpointReady(fmt.Sprintf("http://127.0.0.1:%d/api/health", plusPort))
		mcpReady := mcpState == "running"

		fmt.Println("HAL Plus Status")
		fmt.Println("================")
		fmt.Printf("Engine:              %s\n", engine)
		fmt.Printf("HAL Plus image:      %s (%s)\n", plusImage, boolLabel(plusImagePresent))
		fmt.Printf("HAL MCP image:       %s (%s)\n", mcpImage, boolLabel(mcpImagePresent))
		fmt.Printf("HAL Plus container:  %s (%s)\n", halPlusContainerName, plusState)
		fmt.Printf("HAL MCP container:   %s (%s)\n", halMCPContainerName, mcpState)
		fmt.Printf("HAL Plus health:     %s\n", boolLabel(uiReady))
		fmt.Printf("HAL MCP health:      %s\n", boolLabel(mcpReady))
		fmt.Printf("UI endpoint:         http://hal.localhost:%d\n", plusPort)

		if plusState != "running" || mcpState != "running" {
			fmt.Println("💡 Tip: Run 'hal plus create' to start or reconcile HAL Plus and HAL MCP containers.")
		}
	},
}

func boolLabel(ok bool) string {
	if ok {
		return "ready"
	}
	return "missing"
}
