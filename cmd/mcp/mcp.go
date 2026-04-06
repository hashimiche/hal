package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

const (
	mcpProtocolVersion = "2024-11-05"
	mcpServerName      = "hal-mcp"
	mcpServerVersion   = "0.1.0"
)

const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
)

var (
	createCommandName string
	upTransport       string
)

// Cmd is the exported HAL MCP base command.
var Cmd = &cobra.Command{
	Use:   "mcp",
	Short: "Manage HAL MCP server lifecycle",
	Run: func(cmd *cobra.Command, args []string) {
		statusCmd.Run(cmd, args)
	},
}

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create HAL MCP client config scaffold",
	Run: func(cmd *cobra.Command, args []string) {
		dir, err := ensureMCPDir()
		if err != nil {
			fmt.Printf("❌ Failed to prepare MCP directory: %v\n", err)
			return
		}

		if strings.TrimSpace(createCommandName) == "" {
			createCommandName = "hal"
		}

		configPath := filepath.Join(dir, "hal-mcp.json")
		config := map[string]interface{}{
			"mcpServers": map[string]interface{}{
				"hal": map[string]interface{}{
					"command": strings.TrimSpace(createCommandName),
					"args":    []string{"mcp", "up"},
				},
			},
		}

		payload, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			fmt.Printf("❌ Failed to render MCP config: %v\n", err)
			return
		}

		if err := os.WriteFile(configPath, append(payload, '\n'), 0o644); err != nil {
			fmt.Printf("❌ Failed to write %s: %v\n", configPath, err)
			return
		}

		fmt.Println("✅ HAL MCP scaffold created.")
		fmt.Printf("📄 Config:      %s\n", configPath)
		fmt.Println("🧭 Next:        Point your MCP client to this config file (or copy the hal server block into your client config)")
	},
}

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Run the HAL MCP server",
	Run: func(cmd *cobra.Command, args []string) {
		transport := strings.ToLower(strings.TrimSpace(upTransport))
		if transport == "" {
			transport = "stdio"
		}
		if transport != "stdio" {
			fmt.Printf("❌ Unsupported transport '%s'. Current MVP supports stdio only.\n", transport)
			return
		}

		if err := serveStdioMCP(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "❌ HAL MCP server stopped with error: %v\n", err)
		}
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show HAL MCP readiness state",
	Run: func(cmd *cobra.Command, args []string) {
		dir, err := ensureMCPDir()
		if err != nil {
			fmt.Printf("❌ Failed to access MCP directory: %v\n", err)
			return
		}

		configPath := filepath.Join(dir, "hal-mcp.json")
		_, statErr := os.Stat(configPath)
		hasConfig := statErr == nil

		exePath, exeErr := os.Executable()
		binaryHint := exePath
		if exeErr != nil {
			binaryHint = "unknown"
		}

		fmt.Println("HAL MCP Status")
		fmt.Println("================")
		fmt.Printf("Config dir:   %s\n", dir)
		if hasConfig {
			fmt.Printf("Config file:  ✅ %s\n", configPath)
		} else {
			fmt.Printf("Config file:  ⚪ %s (not created yet)\n", configPath)
		}
		fmt.Println("Transport:    stdio")
		fmt.Printf("HAL binary:   %s\n", binaryHint)
		fmt.Println("Lifecycle:    on-demand (stdio clients start/stop the server process)")
		fmt.Println("💡 Tip: Run 'hal mcp create' to generate a reusable client config block.")
	},
}

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Stop a background HAL MCP server (if present)",
	Run: func(cmd *cobra.Command, args []string) {
		dir, err := ensureMCPDir()
		if err != nil {
			fmt.Printf("❌ Failed to access MCP directory: %v\n", err)
			return
		}

		pidPath := filepath.Join(dir, "hal-mcp.pid")
		pidData, err := os.ReadFile(pidPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				fmt.Println("ℹ️  No HAL MCP daemon PID found. In stdio mode, clients manage process lifetime automatically.")
				return
			}
			fmt.Printf("❌ Failed to read PID file: %v\n", err)
			return
		}

		pidStr := strings.TrimSpace(string(pidData))
		pid, err := strconv.Atoi(pidStr)
		if err != nil || pid <= 0 {
			fmt.Printf("❌ Invalid PID value in %s: %q\n", pidPath, pidStr)
			return
		}

		proc, err := os.FindProcess(pid)
		if err != nil {
			fmt.Printf("❌ Could not find process %d: %v\n", pid, err)
			return
		}

		if err := proc.Signal(syscall.SIGTERM); err != nil {
			fmt.Printf("❌ Failed to stop process %d: %v\n", pid, err)
			return
		}

		_ = os.Remove(pidPath)
		fmt.Printf("✅ Sent SIGTERM to HAL MCP process %d and removed PID file.\n", pid)
	},
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type mcpTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mcpToolCallResult struct {
	Content []mcpTextContent `json:"content"`
	IsError bool             `json:"isError,omitempty"`
}

type toolsCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

func ensureMCPDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(homeDir, ".hal", "mcp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func serveStdioMCP(stdin io.Reader, stdout io.Writer) error {
	reader := bufio.NewReader(stdin)
	writer := bufio.NewWriter(stdout)

	for {
		messageBytes, err := readFramedMessage(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		var req rpcRequest
		if err := json.Unmarshal(messageBytes, &req); err != nil {
			if writeErr := writeResponse(writer, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: rpcParseError, Message: "invalid JSON"}}); writeErr != nil {
				return writeErr
			}
			continue
		}

		if req.JSONRPC != "2.0" || req.Method == "" {
			if req.ID != nil {
				if writeErr := writeResponse(writer, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: rpcInvalidRequest, Message: "invalid JSON-RPC request"}}); writeErr != nil {
					return writeErr
				}
			}
			continue
		}

		res := handleRPCRequest(req)
		if res == nil {
			continue
		}

		if err := writeResponse(writer, *res); err != nil {
			return err
		}
	}
}

func handleRPCRequest(req rpcRequest) *rpcResponse {
	base := &rpcResponse{JSONRPC: "2.0", ID: req.ID}

	switch req.Method {
	case "initialize":
		base.Result = map[string]interface{}{
			"protocolVersion": mcpProtocolVersion,
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    mcpServerName,
				"version": mcpServerVersion,
			},
		}
		return base
	case "notifications/initialized":
		return nil
	case "ping":
		base.Result = map[string]bool{"ok": true}
		return base
	case "tools/list":
		base.Result = map[string]interface{}{"tools": declaredTools()}
		return base
	case "tools/call":
		var params toolsCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil || strings.TrimSpace(params.Name) == "" {
			base.Error = &rpcError{Code: rpcInvalidParams, Message: "invalid tools/call params"}
			return base
		}
		base.Result = callTool(params.Name, params.Arguments)
		return base
	default:
		if req.ID == nil {
			return nil
		}
		base.Error = &rpcError{Code: rpcMethodNotFound, Message: "method not found"}
		return base
	}
}

func declaredTools() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"name":        "hal_status",
			"description": "Run 'hal status' and return the global deployment summary output.",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			"name":        "hal_capacity",
			"description": "Run HAL capacity views. Accepted view values: current, active, pending.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"view": map[string]interface{}{
						"type":        "string",
						"description": "capacity view name",
						"enum":        []string{"current", "active", "pending"},
					},
				},
			},
		},
		{
			"name":        "hal_product_status",
			"description": "Run 'hal <product> status' for one product.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"product": map[string]interface{}{
						"type":        "string",
						"description": "product command name",
						"enum":        []string{"vault", "consul", "nomad", "boundary", "terraform", "obs"},
					},
				},
				"required": []string{"product"},
			},
		},
	}
}

func callTool(name string, args map[string]interface{}) mcpToolCallResult {
	switch strings.TrimSpace(name) {
	case "hal_status":
		out, err := runHAL("status")
		return toolResultFromOutput(out, err)
	case "hal_capacity":
		view := "current"
		if rawView, ok := args["view"]; ok {
			if parsed, ok := rawView.(string); ok {
				view = strings.ToLower(strings.TrimSpace(parsed))
			}
		}

		switch view {
		case "", "current":
			out, err := runHAL("capacity")
			return toolResultFromOutput(out, err)
		case "active":
			out, err := runHAL("capacity", "--active")
			return toolResultFromOutput(out, err)
		case "pending":
			out, err := runHAL("capacity", "--pending")
			return toolResultFromOutput(out, err)
		default:
			return mcpToolCallResult{
				Content: []mcpTextContent{{Type: "text", Text: "invalid view value; expected current, active, or pending"}},
				IsError: true,
			}
		}
	case "hal_product_status":
		product, _ := args["product"].(string)
		product = strings.ToLower(strings.TrimSpace(product))
		switch product {
		case "vault", "consul", "nomad", "boundary", "terraform", "obs":
			out, err := runHAL(product, "status")
			return toolResultFromOutput(out, err)
		default:
			return mcpToolCallResult{
				Content: []mcpTextContent{{Type: "text", Text: "invalid product; expected vault, consul, nomad, boundary, terraform, or obs"}},
				IsError: true,
			}
		}
	default:
		return mcpToolCallResult{
			Content: []mcpTextContent{{Type: "text", Text: "unknown tool"}},
			IsError: true,
		}
	}
}

func toolResultFromOutput(out string, err error) mcpToolCallResult {
	text := strings.TrimSpace(out)
	if text == "" {
		text = "(no output)"
	}
	if err != nil {
		return mcpToolCallResult{
			Content: []mcpTextContent{{Type: "text", Text: fmt.Sprintf("command failed: %v\n\n%s", err, text)}},
			IsError: true,
		}
	}
	return mcpToolCallResult{Content: []mcpTextContent{{Type: "text", Text: text}}}
}

func runHAL(args ...string) (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot resolve hal executable: %w", err)
	}
	cmd := exec.Command(exePath, args...)
	cmd.Env = os.Environ()
	out, runErr := cmd.CombinedOutput()
	return string(out), runErr
}

func readFramedMessage(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		headerName := strings.ToLower(strings.TrimSpace(parts[0]))
		headerValue := strings.TrimSpace(parts[1])
		if headerName == "content-length" {
			value, err := strconv.Atoi(headerValue)
			if err != nil || value < 0 {
				return nil, fmt.Errorf("invalid content-length header")
			}
			contentLength = value
		}
	}

	if contentLength < 0 {
		return nil, fmt.Errorf("missing content-length header")
	}

	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeResponse(writer *bufio.Writer, resp rpcResponse) error {
	body, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	if _, err := writer.Write(body); err != nil {
		return err
	}
	return writer.Flush()
}

func init() {
	createCmd.Flags().StringVar(&createCommandName, "command", "hal", "HAL command name/path to use in generated MCP client config")
	upCmd.Flags().StringVar(&upTransport, "transport", "stdio", "MCP transport to use (stdio for MVP)")

	Cmd.AddCommand(createCmd)
	Cmd.AddCommand(upCmd)
	Cmd.AddCommand(statusCmd)
	Cmd.AddCommand(downCmd)
}
