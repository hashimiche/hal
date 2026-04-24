package plus

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const defaultOllamaKeepAlive = "5m"

type ollamaModelPreset struct {
	Key           string
	BaseModel     string
	ManagedModel  string
	ContextWindow int
	Temperature   float64
	TopP          float64
	TopK          int
	MinP          float64
	RepeatPenalty float64
}

type ollamaRuntimeConfig struct {
	RequestedModel string
	RuntimeModel   string
	Managed        bool
	ManagedByHAL   bool
	ModelfilePath  string
	ModelfileText  string
	ContextWindow  int
	KeepAlive      string
	BaseModel      string
}

var ollamaModelPresets = map[string]ollamaModelPreset{
	"gemma4": {
		Key:           "gemma4",
		BaseModel:     "gemma4:e4b",
		ManagedModel:  "hal-plus-gemma4",
		ContextWindow: 32768,
		Temperature:   1.0,
		TopP:          0.95,
		TopK:          64,
		MinP:          0.0,
		RepeatPenalty: 1.0,
	},
	"qwen3.5": {
		Key:           "qwen3.5",
		BaseModel:     "qwen3.5:9b",
		ManagedModel:  "hal-plus-qwen35",
		ContextWindow: 32768,
		Temperature:   0.6,
		TopP:          0.95,
		TopK:          20,
		MinP:          0.0,
		RepeatPenalty: 1.0,
	},
}

func normalizeOllamaModelSelection(selection string) string {
	trimmed := strings.ToLower(strings.TrimSpace(selection))
	switch trimmed {
	case "gemma", "gemma4":
		return "gemma4"
	case "qwen", "qwen35", "qwen-3.5", "qwen3.5":
		return "qwen3.5"
	default:
		return trimmed
	}
}

func resolveOllamaPreset(selection string) (ollamaModelPreset, bool) {
	preset, ok := ollamaModelPresets[normalizeOllamaModelSelection(selection)]
	return preset, ok
}

func supportedOllamaModelsHelp() string {
	return "Ollama model preset or existing host model name (presets: gemma4, qwen3.5)"
}

func resolveOllamaRuntimeConfig(requestedModel, modelConfigPath, keepAlive string) (ollamaRuntimeConfig, error) {
	selection := strings.TrimSpace(requestedModel)
	if selection == "" {
		return ollamaRuntimeConfig{}, fmt.Errorf("model cannot be empty")
	}

	runtimeConfig := ollamaRuntimeConfig{
		RequestedModel: selection,
		RuntimeModel:   selection,
		KeepAlive:      strings.TrimSpace(keepAlive),
	}
	if runtimeConfig.KeepAlive == "" {
		runtimeConfig.KeepAlive = defaultOllamaKeepAlive
	}

	if preset, ok := resolveOllamaPreset(selection); ok {
		runtimeConfig.RuntimeModel = preset.ManagedModel
		runtimeConfig.Managed = true
		runtimeConfig.ManagedByHAL = true
		runtimeConfig.BaseModel = preset.BaseModel
		runtimeConfig.ContextWindow = preset.ContextWindow
		runtimeConfig.ModelfileText = buildPresetModelfile(preset)
	}

	if strings.TrimSpace(modelConfigPath) == "" {
		return runtimeConfig, nil
	}

	absPath, err := filepath.Abs(modelConfigPath)
	if err != nil {
		return ollamaRuntimeConfig{}, fmt.Errorf("resolve model config path: %w", err)
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return ollamaRuntimeConfig{}, fmt.Errorf("read model config: %w", err)
	}

	runtimeConfig.Managed = true
	runtimeConfig.ManagedByHAL = true
	runtimeConfig.ModelfilePath = absPath
	runtimeConfig.ModelfileText = string(content)
	runtimeConfig.ContextWindow = parseModelfileContextWindow(runtimeConfig.ModelfileText, runtimeConfig.ContextWindow)
	if !strings.Contains(runtimeConfig.RuntimeModel, "hal-plus-") {
		runtimeConfig.RuntimeModel = managedModelAlias(selection)
	}

	return runtimeConfig, nil
}

func buildPresetModelfile(preset ollamaModelPreset) string {
	return strings.TrimSpace(fmt.Sprintf(`FROM %s
PARAMETER num_ctx %d
PARAMETER temperature %.2f
PARAMETER top_p %.2f
PARAMETER top_k %d
PARAMETER min_p %.2f
PARAMETER repeat_penalty %.2f
`,
		preset.BaseModel,
		preset.ContextWindow,
		preset.Temperature,
		preset.TopP,
		preset.TopK,
		preset.MinP,
		preset.RepeatPenalty,
	)) + "\n"
}

func parseModelfileContextWindow(content string, fallback int) int {
	contextWindow := fallback
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if !strings.EqualFold(fields[0], "parameter") || !strings.EqualFold(fields[1], "num_ctx") {
			continue
		}
		var parsed int
		if _, err := fmt.Sscanf(fields[2], "%d", &parsed); err == nil && parsed > 0 {
			contextWindow = parsed
		}
	}
	return contextWindow
}

func managedModelAlias(selection string) string {
	var builder strings.Builder
	for _, char := range strings.ToLower(selection) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			builder.WriteRune(char)
			continue
		}
		if builder.Len() > 0 && builder.String()[builder.Len()-1] != '-' {
			builder.WriteRune('-')
		}
	}
	alias := strings.Trim(builder.String(), "-")
	if alias == "" {
		alias = "custom"
	}
	return "hal-plus-" + alias
}

func reconcileOllamaModel(config ollamaRuntimeConfig) error {
	if config.Managed {
		modelfilePath := config.ModelfilePath
		cleanupPath := ""
		if strings.TrimSpace(modelfilePath) == "" {
			tempFile, err := os.CreateTemp("", "hal-plus-*.Modelfile")
			if err != nil {
				return fmt.Errorf("create temp Modelfile: %w", err)
			}
			if _, err := tempFile.WriteString(config.ModelfileText); err != nil {
				_ = tempFile.Close()
				_ = os.Remove(tempFile.Name())
				return fmt.Errorf("write temp Modelfile: %w", err)
			}
			if err := tempFile.Close(); err != nil {
				_ = os.Remove(tempFile.Name())
				return fmt.Errorf("close temp Modelfile: %w", err)
			}
			modelfilePath = tempFile.Name()
			cleanupPath = tempFile.Name()
		}
		if cleanupPath != "" {
			defer os.Remove(cleanupPath)
		}

		out, err := exec.Command("ollama", "create", config.RuntimeModel, "-f", modelfilePath).CombinedOutput()
		if err != nil {
			return fmt.Errorf("ollama create %s failed: %v (%s)", config.RuntimeModel, err, strings.TrimSpace(string(out)))
		}
		return nil
	}

	if _, err := exec.LookPath("ollama"); err != nil {
		return fmt.Errorf("ollama CLI not found in PATH")
	}

	if ok, err := ollamaModelAvailable(ollamaHostURL, config.RuntimeModel); err == nil && ok {
		return nil
	}

	out, err := exec.Command("ollama", "pull", config.RuntimeModel).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ollama pull %s failed: %v (%s)", config.RuntimeModel, err, strings.TrimSpace(string(out)))
	}
	return nil
}
