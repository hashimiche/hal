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

		runtimeConfig, err := resolveOllamaRuntimeConfig(plusModel, plusModelConfig, ollamaKeepAlive)
		if err != nil {
			fmt.Printf("❌ Invalid Ollama model settings: %v\n", err)
			return
		}

		containerOllamaURL := detectOllamaContainerURL(engine)

		if global.DryRun {
			fmt.Printf("[DRY RUN] Would verify Ollama host endpoint: %s\n", ollamaHostURL)
			if runtimeConfig.ManagedByHAL {
				fmt.Printf("[DRY RUN] Would reconcile HAL-managed Ollama model: %s\n", runtimeConfig.RuntimeModel)
				if runtimeConfig.BaseModel != "" {
					fmt.Printf("[DRY RUN] Would build it from base model: %s\n", runtimeConfig.BaseModel)
				}
				if runtimeConfig.ModelfilePath != "" {
					fmt.Printf("[DRY RUN] Would use model config: %s\n", runtimeConfig.ModelfilePath)
				}
			} else {
				fmt.Printf("[DRY RUN] Would ensure Ollama model exists locally: %s\n", runtimeConfig.RuntimeModel)
			}
			fmt.Printf("[DRY RUN] Would verify local HAL MCP image exists: %s\n", mcpImage)
			fmt.Println("[DRY RUN] Would ensure hal-net exists")
			fmt.Printf("[DRY RUN] Would use local HAL Plus image if present, otherwise pull: %s\n", plusImage)
			fmt.Println("[DRY RUN] Would start container hal-mcp on hal-net")
			fmt.Println("[DRY RUN] Would start container hal-plus on hal-net")
			fmt.Printf("[DRY RUN] Would set OLLAMA_BASE_URL=%s\n", containerOllamaURL)
			fmt.Printf("[DRY RUN] Would set OLLAMA_MODEL=%s\n", runtimeConfig.RuntimeModel)
			if runtimeConfig.ContextWindow > 0 {
				fmt.Printf("[DRY RUN] Would set OLLAMA_CONTEXT_WINDOW=%d\n", runtimeConfig.ContextWindow)
			}
			fmt.Printf("[DRY RUN] Would set OLLAMA_KEEP_ALIVE=%s\n", runtimeConfig.KeepAlive)
			fmt.Println("[DRY RUN] Would set HAL_MCP_HTTP_URL=http://hal-mcp:8080/mcp")
			return
		}

		if err := reconcileOllamaModel(runtimeConfig); err != nil {
			fmt.Printf("❌ Failed to prepare Ollama model '%s': %v\n", runtimeConfig.RuntimeModel, err)
			fmt.Printf("   💡 Ensure Ollama is installed, running, and reachable at %s.\n", ollamaHostURL)
			return
		}

		ok, err := ollamaModelAvailable(ollamaHostURL, runtimeConfig.RuntimeModel)
		if err != nil {
			fmt.Printf("❌ Ollama preflight failed at %s: %v\n", ollamaHostURL, err)
			fmt.Printf("   💡 Ensure Ollama is running on host and reachable before 'hal plus create'.\n")
			return
		}
		if !ok {
			fmt.Printf("❌ Ollama model '%s' was not found at %s after reconciliation\n", runtimeConfig.RuntimeModel, ollamaHostURL)
			fmt.Printf("   💡 Check the configured model source or Modelfile and retry 'hal plus create'.\n")
			return
		}

		if !imageExists(engine, mcpImage) {
			fmt.Printf("❌ Required HAL MCP image not found locally: %s\n", mcpImage)
			fmt.Println("   💡 Run 'hal mcp create --http' first, then retry 'hal plus create'.")
			return
		}

		global.EnsureNetwork(engine)

		if plusPull {
			if err := pullImage(engine, mcpImage); err != nil {
				fmt.Printf("❌ %v\n", err)
				return
			}
			if err := pullImage(engine, plusImage); err != nil {
				fmt.Printf("❌ %v\n", err)
				return
			}
		} else if !imageExists(engine, plusImage) {
			if out, err := exec.Command(engine, "pull", plusImage).CombinedOutput(); err != nil {
				fmt.Printf("❌ Failed to pull HAL Plus image %s: %v\n%s\n", plusImage, err, string(out))
				return
			}
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
			"-e", fmt.Sprintf("OLLAMA_MODEL=%s", runtimeConfig.RuntimeModel),
			"-e", fmt.Sprintf("OLLAMA_MODEL_LABEL=%s", runtimeConfig.RequestedModel),
			"-e", fmt.Sprintf("OLLAMA_KEEP_ALIVE=%s", runtimeConfig.KeepAlive),
			"-e", "HAL_MCP_HTTP_URL=http://hal-mcp:8080/mcp",
			"-e", "HAL_PLUS_CONTAINER_MODE=true",
			plusImage,
		}
		if runtimeConfig.ContextWindow > 0 {
			plusArgs = append(plusArgs[:len(plusArgs)-1], append([]string{"-e", fmt.Sprintf("OLLAMA_CONTEXT_WINDOW=%d", runtimeConfig.ContextWindow)}, plusArgs[len(plusArgs)-1])...)
		}
		if err := ensureRunningContainer(engine, halPlusContainerName, plusArgs); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		fmt.Println("✅ HAL Plus runtime created successfully.")
		global.RefreshHalStatus(engine)
		fmt.Printf("🐳 Engine:       %s\n", engine)
		fmt.Printf("🐳 HAL Plus:     %s\n", plusImage)
		fmt.Printf("🐳 HAL MCP:      %s\n", mcpImage)
		fmt.Printf("🧠 Ollama model: %s\n", runtimeConfig.RuntimeModel)
		if runtimeConfig.RequestedModel != runtimeConfig.RuntimeModel {
			fmt.Printf("🧩 Model preset: %s\n", runtimeConfig.RequestedModel)
		}
		fmt.Printf("🧠 Ollama host:  %s\n", ollamaHostURL)
		fmt.Printf("🌐 UI URL:       http://hal.localhost:%d\n", plusPort)
		fmt.Println("💡 Run 'hal plus status' to verify container and endpoint health.")
	},
}
