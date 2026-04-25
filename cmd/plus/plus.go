package plus

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	halPlusContainerName = "hal-plus"
	halMCPContainerName  = "hal-mcp"
)

var (
	plusImage          string
	mcpImage           string
	plusPort           int
	plusModel          string
	plusModelConfig    string
	ollamaHostURL      string
	ollamaContainerURL string
	ollamaKeepAlive    string
)

// Cmd manages HAL Plus container lifecycle.
var Cmd = &cobra.Command{
	Use:   "plus",
	Short: "Manage HAL Plus web UI runtime",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		statusCmd.Run(cmd, args)
	},
}

func detectOllamaContainerURL(engine string) string {
	if strings.TrimSpace(ollamaContainerURL) != "" {
		return strings.TrimSpace(ollamaContainerURL)
	}
	if engine == "podman" {
		return "http://host.containers.internal:11434"
	}
	return "http://host.docker.internal:11434"
}

func imageExists(engine, image string) bool {
	err := exec.Command(engine, "image", "inspect", image).Run()
	return err == nil
}

func containerState(engine, name string) string {
	out, err := exec.Command(engine, "inspect", "-f", "{{.State.Status}}", name).Output()
	if err != nil {
		return "missing"
	}
	return strings.TrimSpace(string(out))
}

func ensureRunningContainer(engine string, name string, args []string) error {
	state := containerState(engine, name)
	if state == "running" {
		return nil
	}
	if state != "missing" {
		_ = exec.Command(engine, "rm", "-f", name).Run()
	}
	cmdArgs := append([]string{"run", "-d", "--name", name}, args...)
	out, err := exec.Command(engine, cmdArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run %s: %v (%s)", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

type ollamaTagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

func ollamaModelAvailable(baseURL, model string) (bool, error) {
	url := strings.TrimRight(baseURL, "/") + "/api/tags"
	client := http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return false, fmt.Errorf("status %d", resp.StatusCode)
	}

	var tags ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return false, err
	}

	needle := strings.TrimSpace(model)
	for _, m := range tags.Models {
		name := strings.TrimSpace(m.Name)
		if name == needle || strings.HasPrefix(name, needle+":") {
			return true, nil
		}
	}
	return false, nil
}

func endpointReady(url string) bool {
	client := http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func runOutput(engine string, args ...string) string {
	out, err := exec.Command(engine, args...).CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(out))
	}
	return strings.TrimSpace(string(out))
}

func prettyJSON(v interface{}) string {
	buf := bytes.Buffer{}
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return "{}"
	}
	return strings.TrimSpace(buf.String())
}

func init() {
	plusImage = "ghcr.io/hashimiche/hal-plus:latest"
	mcpImage = "ghcr.io/hashimiche/hal-mcp:latest"
	plusPort = 9000
	plusModel = "qwen3.5"
	plusModelConfig = ""
	ollamaHostURL = "http://127.0.0.1:11434"
	ollamaContainerURL = ""
	ollamaKeepAlive = defaultOllamaKeepAlive

	createCmd.Flags().StringVar(&plusImage, "image", plusImage, "HAL Plus image to run")
	createCmd.Flags().StringVar(&mcpImage, "mcp-image", mcpImage, "HAL MCP image expected to exist locally")
	createCmd.Flags().IntVar(&plusPort, "port", plusPort, "Host port for HAL Plus UI")
	createCmd.Flags().StringVar(&plusModel, "model", plusModel, supportedOllamaModelsHelp())
	createCmd.Flags().StringVar(&plusModelConfig, "model-config", plusModelConfig, "Optional Modelfile path used to build a HAL-managed Ollama model on the host")
	createCmd.Flags().StringVar(&ollamaHostURL, "ollama-host-url", ollamaHostURL, "Host-side Ollama URL used for preflight checks")
	createCmd.Flags().StringVar(&ollamaContainerURL, "ollama-base-url", ollamaContainerURL, "Container-side OLLAMA_BASE_URL override (defaults by engine)")
	createCmd.Flags().StringVar(&ollamaKeepAlive, "keep-alive", ollamaKeepAlive, "Ollama model keep-alive duration for HAL Plus chat requests (for example 10m or 0)")

	statusCmd.Flags().StringVar(&plusImage, "image", plusImage, "HAL Plus image expected")
	statusCmd.Flags().StringVar(&mcpImage, "mcp-image", mcpImage, "HAL MCP image expected")

	Cmd.AddCommand(createCmd)
	Cmd.AddCommand(statusCmd)
	Cmd.AddCommand(deleteCmd)

	deleteCmd.Flags().StringVar(&plusImage, "image", plusImage, "HAL Plus image expected")
	deleteCmd.Flags().StringVar(&mcpImage, "mcp-image", mcpImage, "HAL MCP image expected")
}
