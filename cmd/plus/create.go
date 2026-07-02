package plus

import (
	"fmt"
	"os/exec"
	"strings"

	"hal/internal/global"
	"hal/internal/ui"

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

		qdrantEnabled := strings.EqualFold(strings.TrimSpace(ragBackend), "qdrant")

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
			if qdrantEnabled {
				fmt.Printf("[DRY RUN] Would ensure Ollama embedding model exists locally: %s\n", embedModel)
				fmt.Printf("[DRY RUN] Would use local pre-seeded Qdrant image if present, otherwise pull: %s\n", qdrantImage)
				fmt.Println("[DRY RUN] Would start container hal-qdrant on hal-net")
			}
			fmt.Println("[DRY RUN] Would start container hal-plus on hal-net")
			fmt.Printf("[DRY RUN] Would set OLLAMA_BASE_URL=%s\n", containerOllamaURL)
			fmt.Printf("[DRY RUN] Would set OLLAMA_MODEL=%s\n", runtimeConfig.RuntimeModel)
			if runtimeConfig.ContextWindow > 0 {
				fmt.Printf("[DRY RUN] Would set OLLAMA_CONTEXT_WINDOW=%d\n", runtimeConfig.ContextWindow)
			}
			fmt.Printf("[DRY RUN] Would set OLLAMA_KEEP_ALIVE=%s\n", runtimeConfig.KeepAlive)
			fmt.Printf("[DRY RUN] Would set HAL_MCP_HTTP_URL=%s\n", plusMCPEnvURL)
			if qdrantEnabled {
				fmt.Println("[DRY RUN] Would set HAL_RAG_BACKEND=qdrant")
				fmt.Printf("[DRY RUN] Would set HAL_QDRANT_URL=%s\n", plusQdrantEnvURL)
				fmt.Printf("[DRY RUN] Would set HAL_DOC_SEARCH_EMBED_MODEL=%s\n", embedModel)
			}
			return
		}

		ui.LogoStart("plus")
		defer ui.LogoStop()

		ui.LogoStep("Preparing Ollama model %s", runtimeConfig.RuntimeModel)
		if err := reconcileOllamaModel(runtimeConfig); err != nil {
			ui.LogoStop()
			fmt.Printf("❌ Failed to prepare Ollama model '%s': %v\n", runtimeConfig.RuntimeModel, err)
			fmt.Printf("   💡 Ensure Ollama is installed, running, and reachable at %s.\n", ollamaHostURL)
			return
		}

		ui.LogoStep("Verifying Ollama model")
		ok, err := ollamaModelAvailable(ollamaHostURL, runtimeConfig.RuntimeModel)
		if err != nil {
			ui.LogoStop()
			fmt.Printf("❌ Ollama preflight failed at %s: %v\n", ollamaHostURL, err)
			fmt.Printf("   💡 Ensure Ollama is running on host and reachable before 'hal plus create'.\n")
			return
		}
		if !ok {
			ui.LogoStop()
			fmt.Printf("❌ Ollama model '%s' was not found at %s after reconciliation\n", runtimeConfig.RuntimeModel, ollamaHostURL)
			fmt.Printf("   💡 Check the configured model source or Modelfile and retry 'hal plus create'.\n")
			return
		}

		if qdrantEnabled {
			ui.LogoStep("Preparing embedding model %s", embedModel)
			if err := ensureEmbedModel(embedModel); err != nil {
				ui.LogoStop()
				fmt.Printf("❌ Failed to prepare embedding model '%s': %v\n", embedModel, err)
				fmt.Printf("   💡 Ensure Ollama is reachable at %s, or rerun with --rag local.\n", ollamaHostURL)
				return
			}
		}

		ui.LogoStep("Checking HAL MCP image")
		if !imageExists(engine, mcpImage) {
			ui.LogoStop()
			fmt.Printf("❌ Required HAL MCP image not found locally: %s\n", mcpImage)
			fmt.Println("   💡 Run 'hal mcp create --http' first, then retry 'hal plus create'.")
			return
		}

		global.EnsureNetwork(engine)

		if plusPull {
			ui.LogoStep("Pulling HAL MCP + Plus images")
			if err := pullImage(engine, mcpImage); err != nil {
				ui.LogoStop()
				fmt.Printf("❌ %v\n", err)
				return
			}
			if err := pullImage(engine, plusImage); err != nil {
				ui.LogoStop()
				fmt.Printf("❌ %v\n", err)
				return
			}
		} else if !imageExists(engine, plusImage) {
			ui.LogoStep("Pulling HAL Plus image")
			if out, err := exec.Command(engine, "pull", plusImage).CombinedOutput(); err != nil {
				ui.LogoStop()
				fmt.Printf("❌ Failed to pull HAL Plus image %s: %v\n%s\n", plusImage, err, string(out))
				return
			}
		}

		ui.LogoStep("Starting HAL MCP")
		if err := ensureRunningContainer(engine, halMCPContainerName, []string{"--network", global.HalNetName, mcpImage}); err != nil {
			ui.LogoStop()
			fmt.Printf("❌ %v\n", err)
			return
		}

		if qdrantEnabled {
			ui.LogoStep("Starting Qdrant")
			if plusPull {
				if err := pullImage(engine, qdrantImage); err != nil {
					ui.LogoStop()
					fmt.Printf("❌ %v\n", err)
					return
				}
			} else if !imageExists(engine, qdrantImage) {
				if out, err := exec.Command(engine, "pull", qdrantImage).CombinedOutput(); err != nil {
					ui.LogoStop()
					fmt.Printf("❌ Failed to pull pre-seeded HAL Plus Qdrant image %s: %v\n%s\n", qdrantImage, err, string(out))
					fmt.Println("   💡 Rerun with '--rag local' to use the in-process retrieval backend instead.")
					return
				}
			}
			if err := ensureRunningContainer(engine, halQdrantContainerName, []string{"--network", global.HalNetName, qdrantImage}); err != nil {
				ui.LogoStop()
				fmt.Printf("❌ %v\n", err)
				return
			}
		}

		plusArgs := []string{
			"--network", global.HalNetName,
			"-p", fmt.Sprintf("%d:%d", plusPort, plusAPIPort),
			"-e", "API_HOST=0.0.0.0",
			"-e", fmt.Sprintf("API_PORT=%d", plusAPIPort),
			"-e", fmt.Sprintf("OLLAMA_BASE_URL=%s", containerOllamaURL),
			"-e", fmt.Sprintf("OLLAMA_MODEL=%s", runtimeConfig.RuntimeModel),
			"-e", fmt.Sprintf("OLLAMA_MODEL_LABEL=%s", runtimeConfig.RequestedModel),
			"-e", fmt.Sprintf("OLLAMA_KEEP_ALIVE=%s", runtimeConfig.KeepAlive),
			"-e", fmt.Sprintf("HAL_MCP_HTTP_URL=%s", plusMCPEnvURL),
			"-e", "HAL_PLUS_CONTAINER_MODE=true",
			plusImage,
		}
		if runtimeConfig.ContextWindow > 0 {
			plusArgs = append(plusArgs[:len(plusArgs)-1], append([]string{"-e", fmt.Sprintf("OLLAMA_CONTEXT_WINDOW=%d", runtimeConfig.ContextWindow)}, plusArgs[len(plusArgs)-1])...)
		}
		if qdrantEnabled {
			qdrantEnv := []string{
				"-e", "HAL_RAG_BACKEND=qdrant",
				"-e", fmt.Sprintf("HAL_QDRANT_URL=%s", plusQdrantEnvURL),
				"-e", fmt.Sprintf("HAL_DOC_SEARCH_EMBED_MODEL=%s", embedModel),
			}
			plusArgs = append(plusArgs[:len(plusArgs)-1], append(qdrantEnv, plusArgs[len(plusArgs)-1])...)
		}
		ui.LogoStep("Starting HAL Plus")
		if err := ensureRunningContainer(engine, halPlusContainerName, plusArgs); err != nil {
			ui.LogoStop()
			fmt.Printf("❌ %v\n", err)
			return
		}

		ui.LogoStop()
		global.RefreshHalHealth(engine)
		ui.Success("HAL Plus runtime created!")
		ui.Section("Runtime")
		ui.Field("Engine", engine)
		ui.Field("HAL Plus", plusImage)
		ui.Field("HAL MCP", mcpImage)
		if qdrantEnabled {
			ui.Field("Qdrant", qdrantImage)
			ui.Field("RAG", fmt.Sprintf("qdrant (embed model: %s)", embedModel))
		} else {
			ui.Field("RAG", "local (in-process)")
		}
		ui.Field("Model", runtimeConfig.RuntimeModel)
		if runtimeConfig.RequestedModel != runtimeConfig.RuntimeModel {
			ui.Field("Preset", runtimeConfig.RequestedModel)
		}
		ui.Field("Ollama", ollamaHostURL)
		ui.Field("UI", fmt.Sprintf("http://%s:%d", plusUIHostname, plusPort))
		ui.Hint("hal plus status  to verify container and endpoint health")
	},
}
