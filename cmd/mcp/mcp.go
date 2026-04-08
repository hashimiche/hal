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
	"time"

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
	createBinaryPath  string
	createJSONOnly    bool
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

		managedBinary := ""
		if !createJSONOnly {
			managedBinary, err = provisionManagedMCPBinary(createBinaryPath)
			if err != nil {
				fmt.Printf("❌ Failed to provision HAL MCP binary: %v\n", err)
				fmt.Println("   💡 Retry with '--json' if you only want the MCP config file.")
				return
			}
		}

		commandName := strings.TrimSpace(createCommandName)
		if commandName == "" {
			commandName = "hal"
		}
		if managedBinary != "" && !cmd.Flags().Changed("command") {
			commandName = managedBinary
		}

		configPath := filepath.Join(dir, "hal-mcp.json")
		config := map[string]interface{}{
			"mcpServers": map[string]interface{}{
				"hal": map[string]interface{}{
					"command": commandName,
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
		if managedBinary != "" {
			fmt.Printf("🧩 Binary:      %s\n", managedBinary)
		}
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

		managedBinaryPath, managedPathErr := defaultManagedBinaryPath()
		if managedPathErr != nil {
			managedBinaryPath = "unknown"
		}
		hasManagedBinary := false
		if managedPathErr == nil {
			if _, err := os.Stat(managedBinaryPath); err == nil {
				hasManagedBinary = true
			}
		}

		fmt.Println("HAL MCP Status")
		fmt.Println("================")
		fmt.Printf("Config path:  %s\n", configPath)
		fmt.Printf("Binary path:  %s\n", managedBinaryPath)
		if hasConfig {
			fmt.Println("Config file:  ✅ Present")
		} else {
			fmt.Println("Config file:  ⚪ Not created yet")
		}
		if hasManagedBinary {
			fmt.Println("Managed bin:  ✅ Present")
		} else {
			fmt.Println("Managed bin:  ⚪ Not found")
		}
		fmt.Println("Transport:    stdio")
		fmt.Println("Lifecycle:    on-demand (stdio clients start/stop the server process)")
		fmt.Println("💡 Tip: Run 'hal mcp create' to generate/update MCP config and managed binary.")
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
	Content           []mcpTextContent `json:"content"`
	IsError           bool             `json:"isError,omitempty"`
	StructuredContent interface{}      `json:"structuredContent,omitempty"`
}

type toolsCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type toolExecution struct {
	Command   string `json:"command"`
	ExitCode  int    `json:"exit_code"`
	Output    string `json:"output"`
	Timestamp string `json:"timestamp"`
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

func defaultManagedBinaryPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".hal", "bin", "hal-mcp"), nil
}

func provisionManagedMCPBinary(targetPath string) (string, error) {
	resolved := strings.TrimSpace(targetPath)
	if resolved == "" {
		defaultPath, err := defaultManagedBinaryPath()
		if err != nil {
			return "", err
		}
		resolved = defaultPath
	}

	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return "", err
	}

	src, err := os.Open(exePath)
	if err != nil {
		return "", err
	}
	defer src.Close()

	dst, err := os.Create(resolved)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", err
	}

	if err := os.Chmod(resolved, 0o755); err != nil {
		return "", err
	}

	return resolved, nil
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
	base := []map[string]interface{}{
		{
			"name":        "hal_status",
			"description": "Run 'hal status' and return global deployment state with executed command metadata.",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			"name":        "hal_capacity",
			"description": "Run HAL capacity views. Accepted view values: current, active, pending. Rejects unknown parameters.",
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
			"description": "Run 'hal <product> status' for one product. Rejects unknown parameters.",
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
		{
			"name":        "hal_help",
			"description": "Run help commands to ground the model on real HAL command/flag syntax.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"target": map[string]interface{}{
						"type":        "string",
						"description": "help target",
						"enum":        []string{"root", "vault", "consul", "nomad", "boundary", "terraform", "obs", "mcp"},
					},
				},
			},
		},
		{
			"name":        "hal_snapshot",
			"description": "Collect a grounded read-only snapshot: global status, capacity views, and product status outputs.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"include_products": map[string]interface{}{
						"type":        "boolean",
						"description": "Include per-product status commands",
					},
					"include_capacity_views": map[string]interface{}{
						"type":        "boolean",
						"description": "Include active and pending capacity outputs",
					},
				},
			},
		},
	}
	withOps := append(base, mcpOpsTools()...)
	return append(withOps, mcpAdvancedTools()...)
}

func callTool(name string, args map[string]interface{}) mcpToolCallResult {
	if args == nil {
		args = map[string]interface{}{}
	}
	switch strings.TrimSpace(name) {
	case "hal_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return toolError(err.Error())
		}
		execRes := runHAL("status")
		return toolResultFromExecution(execRes)
	case "hal_capacity":
		if err := ensureOnlyKeys(args, map[string]bool{"view": true}); err != nil {
			return toolError(err.Error())
		}

		view := "current"
		if rawView, ok := args["view"]; ok {
			if parsed, ok := rawView.(string); ok {
				view = strings.ToLower(strings.TrimSpace(parsed))
			} else {
				return toolError("invalid type for view; expected string")
			}
		}

		switch view {
		case "", "current":
			execRes := runHAL("capacity")
			return toolResultFromExecution(execRes)
		case "active":
			execRes := runHAL("capacity", "--active")
			return toolResultFromExecution(execRes)
		case "pending":
			execRes := runHAL("capacity", "--pending")
			return toolResultFromExecution(execRes)
		default:
			return toolError("invalid view value; expected current, active, or pending")
		}
	case "hal_product_status":
		if err := ensureOnlyKeys(args, map[string]bool{"product": true}); err != nil {
			return toolError(err.Error())
		}

		product, _ := args["product"].(string)
		product = strings.ToLower(strings.TrimSpace(product))
		switch product {
		case "vault", "consul", "nomad", "boundary", "terraform", "obs":
			execRes := runHAL(product, "status")
			return toolResultFromExecution(execRes)
		default:
			return toolError("invalid product; expected vault, consul, nomad, boundary, terraform, or obs")
		}
	case "hal_help":
		if err := ensureOnlyKeys(args, map[string]bool{"target": true}); err != nil {
			return toolError(err.Error())
		}
		target := "root"
		if rawTarget, ok := args["target"]; ok {
			parsedTarget, ok := rawTarget.(string)
			if !ok {
				return toolError("invalid type for target; expected string")
			}
			target = strings.ToLower(strings.TrimSpace(parsedTarget))
		}

		switch target {
		case "", "root":
			execRes := runHAL("--help")
			return toolResultFromExecution(execRes)
		case "vault", "consul", "nomad", "boundary", "terraform", "obs", "mcp":
			execRes := runHAL(target, "--help")
			return toolResultFromExecution(execRes)
		default:
			return toolError("invalid target; expected root, vault, consul, nomad, boundary, terraform, obs, or mcp")
		}
	case "hal_snapshot":
		if err := ensureOnlyKeys(args, map[string]bool{"include_products": true, "include_capacity_views": true}); err != nil {
			return toolError(err.Error())
		}
		includeProducts := true
		if raw, ok := args["include_products"]; ok {
			parsed, ok := raw.(bool)
			if !ok {
				return toolError("invalid type for include_products; expected boolean")
			}
			includeProducts = parsed
		}

		includeCapacityViews := true
		if raw, ok := args["include_capacity_views"]; ok {
			parsed, ok := raw.(bool)
			if !ok {
				return toolError("invalid type for include_capacity_views; expected boolean")
			}
			includeCapacityViews = parsed
		}

		snapshot := map[string]interface{}{
			"timestamp":    time.Now().UTC().Format(time.RFC3339),
			"server":       mcpServerName,
			"hal_snapshot": map[string]interface{}{},
		}

		entries := map[string]interface{}{}
		entries["status"] = runHAL("status")
		entries["capacity_current"] = runHAL("capacity")
		if includeCapacityViews {
			entries["capacity_active"] = runHAL("capacity", "--active")
			entries["capacity_pending"] = runHAL("capacity", "--pending")
		}
		if includeProducts {
			entries["vault_status"] = runHAL("vault", "status")
			entries["consul_status"] = runHAL("consul", "status")
			entries["nomad_status"] = runHAL("nomad", "status")
			entries["boundary_status"] = runHAL("boundary", "status")
			entries["terraform_status"] = runHAL("terraform", "status")
			entries["obs_status"] = runHAL("obs", "status")
		}
		snapshot["hal_snapshot"] = entries

		textBody, _ := json.MarshalIndent(snapshot, "", "  ")
		return mcpToolCallResult{
			Content:           []mcpTextContent{{Type: "text", Text: string(textBody)}},
			StructuredContent: snapshot,
		}
	default:
		if opsRes, handled := handleOpsTool(name, args); handled {
			return opsRes
		}
		if advRes, handled := handleAdvancedTool(name, args); handled {
			return ensureContractResult(name, advRes)
		}
		return toolError("unknown tool")
	}
}

func toolResultFromExecution(execRes toolExecution) mcpToolCallResult {
	if execRes.ExitCode != 0 {
		return opErrorForTool("hal_runtime", classifyContractError(execRes.Output), "command execution failed; run a recommended command for remediation", map[string]interface{}{"execution": execRes}, []string{"hal --help"}, []opCheck{{Name: "execution", Status: "error", Details: execRes.Command}}, nil, nil)
	}
	return opSuccessForTool("hal_runtime", "command execution succeeded", map[string]interface{}{"execution": execRes}, []string{execRes.Command}, []opCheck{{Name: "execution", Status: "ok", Details: execRes.Command}}, nil, nil, nil)
}

func toolError(message string) mcpToolCallResult {
	return opErrorForTool("hal_runtime", codeParseError, message, nil, []string{"hal --help"}, []opCheck{{Name: "input", Status: "error", Details: message}}, nil, nil)
}

func ensureContractResult(toolName string, result mcpToolCallResult) mcpToolCallResult {
	raw, err := json.Marshal(result.StructuredContent)
	if err == nil {
		var env map[string]interface{}
		if jsonErr := json.Unmarshal(raw, &env); jsonErr == nil {
			if _, hasStatus := env["status"]; hasStatus {
				if _, hasCode := env["code"]; hasCode {
					if _, hasRecommended := env["recommended_commands"]; hasRecommended {
						return result
					}
				}
			}
		}
	}

	checks := []opCheck{{Name: "legacy_tool", Status: "warn", Details: "wrapped legacy structured content"}}
	if result.IsError {
		return opErrorForTool(toolName, codeUnsupportedOp, "legacy tool returned non-contract payload; wrapped into contract", result.StructuredContent, []string{"hal --help"}, checks, nil, nil)
	}
	return opSuccessForTool(toolName, "legacy tool payload wrapped into contract", result.StructuredContent, []string{"hal --help"}, checks, nil, nil, nil)
}

func ensureOnlyKeys(args map[string]interface{}, allowed map[string]bool) error {
	for key := range args {
		if !allowed[key] {
			return fmt.Errorf("unknown argument: %s", key)
		}
	}
	return nil
}

func runHAL(args ...string) toolExecution {
	exePath, err := os.Executable()
	if err != nil {
		return toolExecution{Command: "hal " + strings.Join(args, " "), ExitCode: 1, Output: fmt.Sprintf("cannot resolve hal executable: %v", err), Timestamp: time.Now().UTC().Format(time.RFC3339)}
	}
	commandPath := exePath
	base := strings.ToLower(filepath.Base(exePath))
	if strings.Contains(base, ".test") {
		if halPath, lookErr := exec.LookPath("hal"); lookErr == nil {
			commandPath = halPath
		}
	}
	cmd := exec.Command(commandPath, args...)
	cmd.Env = os.Environ()
	out, runErr := cmd.CombinedOutput()
	exitCode := 0
	if runErr != nil {
		exitCode = 1
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}

	return toolExecution{
		Command:   "hal " + strings.Join(args, " "),
		ExitCode:  exitCode,
		Output:    string(out),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
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
	createCmd.Flags().StringVar(&createBinaryPath, "binary-path", "", "Path to write the managed HAL binary used by MCP clients (default ~/.hal/bin/hal-mcp)")
	createCmd.Flags().BoolVar(&createJSONOnly, "json", false, "Only generate/update MCP config JSON (skip managed binary provisioning)")
	upCmd.Flags().StringVar(&upTransport, "transport", "stdio", "MCP transport to use (stdio for MVP)")

	Cmd.AddCommand(createCmd)
	Cmd.AddCommand(upCmd)
	Cmd.AddCommand(statusCmd)
	Cmd.AddCommand(downCmd)
}
