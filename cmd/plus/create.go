package plus

import (
	"fmt"
	"os/exec"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create HAL Plus container runtime",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		containerOllamaURL := detectOllamaContainerURL(engine)

		if global.DryRun {
			fmt.Printf("[DRY RUN] Would verify Ollama host endpoint: %s\n", ollamaHostURL)
			fmt.Printf("[DRY RUN] Would verify model availability: %s\n", plusModel)
			fmt.Printf("[DRY RUN] Would verify local HAL MCP image exists: %s\n", mcpImage)
			fmt.Println("[DRY RUN] Would ensure hal-net exists")
			fmt.Printf("[DRY RUN] Would pull HAL Plus image: %s\n", plusImage)
			fmt.Println("[DRY RUN] Would start container hal-mcp on hal-net")
			fmt.Println("[DRY RUN] Would start container hal-plus on hal-net")
			fmt.Printf("[DRY RUN] Would set OLLAMA_BASE_URL=%s\n", containerOllamaURL)
			fmt.Println("[DRY RUN] Would set HAL_MCP_HTTP_URL=http://hal-mcp:8080/mcp")
			return
		}

		ok, err := ollamaModelAvailable(ollamaHostURL, plusModel)
		if err != nil {
			fmt.Printf("❌ Ollama preflight failed at %s: %v\n", ollamaHostURL, err)
			fmt.Printf("   💡 Ensure Ollama is running on host and reachable before 'hal plus create'.\n")
			return
		}
		if !ok {
			fmt.Printf("❌ Ollama model '%s' was not found at %s\n", plusModel, ollamaHostURL)
			fmt.Printf("   💡 Pull the model first (example): ollama pull %s\n", plusModel)
			return
		}

		if !imageExists(engine, mcpImage) {
			fmt.Printf("❌ Required HAL MCP image not found locally: %s\n", mcpImage)
			fmt.Println("   💡 Run 'hal mcp create --http' first, then retry 'hal plus create'.")
			return
		}

		global.EnsureNetwork(engine)

		if out, err := exec.Command(engine, "pull", plusImage).CombinedOutput(); err != nil {
			fmt.Printf("❌ Failed to pull HAL Plus image %s: %v\n%s\n", plusImage, err, string(out))
			return
		}

		if err := ensureRunningContainer(engine, halMCPContainerName, []string{"--network", "hal-net", mcpImage}); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		plusArgs := []string{
			"--network", "hal-net",
			"-p", fmt.Sprintf("%d:9000", plusPort),
			"-e", "API_HOST=0.0.0.0",
			"-e", "API_PORT=9000",
			"-e", fmt.Sprintf("OLLAMA_BASE_URL=%s", containerOllamaURL),
			"-e", fmt.Sprintf("OLLAMA_MODEL=%s", plusModel),
			"-e", "HAL_MCP_HTTP_URL=http://hal-mcp:8080/mcp",
			"-e", "HAL_PLUS_CONTAINER_MODE=true",
			plusImage,
		}
		if err := ensureRunningContainer(engine, halPlusContainerName, plusArgs); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		fmt.Println("✅ HAL Plus runtime created successfully.")
		fmt.Printf("🐳 Engine:       %s\n", engine)
		fmt.Printf("🐳 HAL Plus:     %s\n", plusImage)
		fmt.Printf("🐳 HAL MCP:      %s\n", mcpImage)
		fmt.Printf("🧠 Ollama host:  %s\n", ollamaHostURL)
		fmt.Printf("🌐 UI URL:       http://hal.localhost:%d\n", plusPort)
		fmt.Println("💡 Run 'hal plus status' to verify container and endpoint health.")
	},
}
