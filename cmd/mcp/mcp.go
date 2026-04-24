package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

type CleanupResult struct {
	ConfigRemoved  bool
	BinaryRemoved  bool
	PIDRemoved     bool
	ProcessStopped bool
	Warnings       []string
}

const (
	// mcpProtocolVersionStdio is advertised over stdio (legacy MCP clients).
	mcpProtocolVersionStdio = "2024-11-05"
	// mcpProtocolVersionStreamableHTTP is advertised over the streamable-http transport,
	// matching the MCP 2025-03-26 spec used by HashiCorp's MCP servers.
	mcpProtocolVersionStreamableHTTP = "2025-03-26"
	mcpServerName                    = "hal-mcp"
	mcpServerVersion                 = "0.1.0"

	// transportStdio is the default stdio transport.
	transportStdio = "stdio"
	// transportStreamableHTTP is the streamable-http transport per MCP 2025-03-26.
	transportStreamableHTTP = "streamable-http"
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
	createHTTPImage   bool
	createHTTPTag     string
	upTransport       string
	upHTTPHost        string
	upHTTPPort        int
	upHTTPPath        string
	policyProfile     string
	policyJSON        bool
)

// Cmd is the exported HAL MCP base command.
var Cmd = &cobra.Command{
	Use:   "mcp",
	Short: "Manage HAL MCP server lifecycle",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		statusCmd.Run(cmd, args)
	},
}

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create or replace HAL MCP client config scaffold",
	Long:  "Create or replace the HAL MCP config and managed binary in place. Existing HAL-managed MCP artifacts are refreshed rather than duplicated.",
	Run: func(cmd *cobra.Command, args []string) {
		if createHTTPImage {
			engine, err := global.DetectEngine()
			if err != nil {
				fmt.Printf("❌ Failed to detect container engine for MCP image build: %v\n", err)
				return
			}

			imageTag := strings.TrimSpace(createHTTPTag)
			if imageTag == "" {
				fmt.Println("❌ Image tag cannot be empty (use --http-tag).")
				return
			}

			if err := buildManagedMCPHTTPImage(engine, imageTag); err != nil {
				fmt.Printf("❌ Failed to build HAL MCP HTTP image: %v\n", err)
				return
			}

			fmt.Println("✅ HAL MCP HTTP image created locally.")
			fmt.Printf("🐳 Engine:      %s\n", engine)
			fmt.Printf("🐳 Image:       %s\n", imageTag)
			fmt.Println("🧭 Next:        Start this image on hal-net and point HAL Plus to HAL_MCP_HTTP_URL=http://hal-mcp:8080/mcp")
			return
		}

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
					"args":    []string{"mcp", "serve"},
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

		fmt.Println("✅ HAL MCP scaffold created or refreshed in place.")
		if managedBinary != "" {
			fmt.Printf("🧩 Binary:      %s\n", managedBinary)
		}
		fmt.Printf("📄 Config:      %s\n", configPath)
		fmt.Println("♻️  Behavior:    Existing HAL-managed MCP artifacts are replaced in place when present.")
		fmt.Println("🧭 Next:        Point your MCP client to this config file (or copy the hal server block into your client config)")
	},
}

var upCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the HAL MCP server (stdio or streamable-http)",
	Long: `Run the HAL MCP server for an MCP client.

Transports:
  stdio            Default. Server reads/writes JSON-RPC over stdio. Used by
                   MCP clients that spawn the server as a child process.
  streamable-http  MCP 2025-03-26 streamable HTTP transport. Server listens on
                   --http-host:--http-port at --http-path. Suitable for
                   container deployments on a shared network.`,
	Run: func(cmd *cobra.Command, args []string) {
		transport := strings.ToLower(strings.TrimSpace(upTransport))
		if transport == "" {
			transport = transportStdio
		}

		switch transport {
		case transportStdio:
			if stdinIsTerminal() {
				fmt.Fprintln(os.Stderr, "ℹ️  'hal mcp serve' is the raw MCP stdio server entrypoint.")
				fmt.Fprintln(os.Stderr, "   Start it from an MCP client using the config generated by 'hal mcp create'.")
				fmt.Fprintln(os.Stderr, "   For readiness checks, use 'hal mcp status'.")
				fmt.Fprintln(os.Stderr, "   For container/network deployments, use --transport streamable-http.")
				return
			}

			if err := serveStdioMCP(os.Stdin, os.Stdout); err != nil {
				fmt.Fprintf(os.Stderr, "❌ HAL MCP server stopped with error: %v\n", err)
			}
		case transportStreamableHTTP:
			if err := serveStreamableHTTPMCP(upHTTPHost, upHTTPPort, upHTTPPath); err != nil {
				fmt.Fprintf(os.Stderr, "❌ HAL MCP server stopped with error: %v\n", err)
			}
		default:
			fmt.Printf("❌ Unsupported transport '%s'. Supported: %s, %s.\n", transport, transportStdio, transportStreamableHTTP)
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
		fmt.Println("💡 Tip: Run 'hal mcp create' to generate or replace the MCP config and managed binary.")
	},
}

var downCmd = &cobra.Command{
	Use:   "delete",
	Short: "Remove HAL MCP managed artifacts",
	Run: func(cmd *cobra.Command, args []string) {
		result := CleanupArtifacts()
		if !result.ConfigRemoved && !result.BinaryRemoved && !result.PIDRemoved && !result.ProcessStopped && len(result.Warnings) == 0 {
			fmt.Println("ℹ️  No HAL MCP managed artifacts found.")
			return
		}

		fmt.Println("✅ HAL MCP artifacts cleaned.")
		fmt.Printf("   - Config removed:   %t\n", result.ConfigRemoved)
		fmt.Printf("   - Binary removed:   %t\n", result.BinaryRemoved)
		fmt.Printf("   - PID removed:      %t\n", result.PIDRemoved)
		fmt.Printf("   - Process stopped:  %t\n", result.ProcessStopped)
		if len(result.Warnings) > 0 {
			fmt.Println("\n⚠️  Cleanup warnings:")
			for _, warning := range result.Warnings {
				fmt.Printf("   - %s\n", warning)
			}
		}
	},
}

var policyCmd = &cobra.Command{
	Use:   "policy",
	Short: "Print HAL MCP runtime answer/tool policy",
	Long:  "Print the HAL MCP runtime policy profile used to guide AI clients and MCP tool orchestration. This is an introspection/export command, not an MCP lifecycle resource.",
	Run: func(cmd *cobra.Command, args []string) {
		policy, err := buildPolicyProfile(policyProfile)
		if err != nil {
			fmt.Printf("❌ Failed to build policy profile: %v\n", err)
			return
		}

		if policyJSON {
			payload, err := json.MarshalIndent(policy, "", "  ")
			if err != nil {
				fmt.Printf("❌ Failed to render policy JSON: %v\n", err)
				return
			}
			fmt.Println(string(payload))
			return
		}

		fmt.Printf("HAL MCP Policy (%s)\n", policyProfile)
		fmt.Println("========================")
		fmt.Println("Use '--json' for machine-readable output.")
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

func buildManagedMCPHTTPImage(engine, imageTag string) error {
	sourceRoot, err := resolveHalSourceRoot()
	if err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "hal-mcp-image-")
	if err != nil {
		return fmt.Errorf("create temp build directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	dockerfile := `FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/hal ./main.go

FROM alpine:3.20
RUN apk add --no-cache docker-cli
RUN adduser -D -u 10001 hal
COPY --from=build /out/hal /usr/local/bin/hal
USER hal
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/hal", "mcp", "serve", "--transport", "streamable-http", "--http-host", "0.0.0.0", "--http-port", "8080", "--http-path", "/mcp"]
`
	if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		return fmt.Errorf("write temporary Dockerfile: %w", err)
	}

	buildCmd := exec.Command(engine, "build", "-t", imageTag, "-f", filepath.Join(tmpDir, "Dockerfile"), sourceRoot)
	buildOutput, err := buildCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s build failed: %w\n%s", engine, err, strings.TrimSpace(string(buildOutput)))
	}

	return nil
}

func resolveHalSourceRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}

	dir := wd
	for {
		modPath := filepath.Join(dir, "go.mod")
		payload, readErr := os.ReadFile(modPath)
		if readErr == nil {
			if strings.Contains(string(payload), "module hal") {
				return dir, nil
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("cannot locate HAL source root with go.mod (run from hal repo tree)")
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

		res := handleRPCRequest(req, mcpProtocolVersionStdio)
		if res == nil {
			continue
		}

		if err := writeResponse(writer, *res); err != nil {
			return err
		}
	}
}

// serveStreamableHTTPMCP serves the MCP protocol over the streamable HTTP
// transport defined in MCP spec 2025-03-26. It implements the minimal subset
// needed for request/response: a single POST endpoint that returns either an
// application/json body (for requests with an id) or 202 Accepted (for
// notifications). Server-initiated SSE streams are not used.
func serveStreamableHTTPMCP(host string, port int, path string) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("invalid http port %d", port)
	}
	if strings.TrimSpace(host) == "" {
		host = "0.0.0.0"
	}
	if strings.TrimSpace(path) == "" {
		path = "/mcp"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	mux := http.NewServeMux()
	mux.HandleFunc(path, handleStreamableHTTPRPC)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"server":"hal-mcp"}`))
	})

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	fmt.Fprintf(os.Stderr, "✅ HAL MCP streamable-http server listening on http://%s%s\n", addr, path)
	fmt.Fprintf(os.Stderr, "   Protocol version: %s\n", mcpProtocolVersionStreamableHTTP)
	fmt.Fprintf(os.Stderr, "   Health endpoint:  http://%s/healthz\n", addr)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return <-errCh
	}
}

func handleStreamableHTTPRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bodyBytes, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSONRPCError(w, nil, rpcParseError, "failed to read request body")
		return
	}

	var req rpcRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeJSONRPCError(w, nil, rpcParseError, "invalid JSON")
		return
	}

	if req.JSONRPC != "2.0" || req.Method == "" {
		if req.ID != nil {
			writeJSONRPCError(w, req.ID, rpcInvalidRequest, "invalid JSON-RPC request")
			return
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}

	res := handleRPCRequest(req, mcpProtocolVersionStreamableHTTP)
	if res == nil {
		// Notification: no response body expected.
		w.WriteHeader(http.StatusAccepted)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(res); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  HAL MCP streamable-http encode error: %v\n", err)
	}
}

func writeJSONRPCError(w http.ResponseWriter, id interface{}, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}
	_ = json.NewEncoder(w).Encode(resp)
}

func handleRPCRequest(req rpcRequest, protocolVersion string) *rpcResponse {
	base := &rpcResponse{JSONRPC: "2.0", ID: req.ID}

	switch req.Method {
	case "initialize":
		base.Result = map[string]interface{}{
			"protocolVersion": protocolVersion,
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
	if strings.Contains(base, ".test") || strings.Contains(base, "hal-mcp") {
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

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func CleanupArtifacts() CleanupResult {
	result := CleanupResult{}

	dir, err := ensureMCPDir()
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("mcp directory access failed: %v", err))
		return result
	}

	pidPath := filepath.Join(dir, "hal-mcp.pid")
	if pidData, err := os.ReadFile(pidPath); err == nil {
		pidStr := strings.TrimSpace(string(pidData))
		pid, convErr := strconv.Atoi(pidStr)
		if convErr != nil || pid <= 0 {
			result.Warnings = append(result.Warnings, fmt.Sprintf("invalid MCP PID value in %s: %q", pidPath, pidStr))
		} else {
			proc, findErr := os.FindProcess(pid)
			if findErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("could not find MCP process %d: %v", pid, findErr))
			} else if signalErr := proc.Signal(syscall.SIGTERM); signalErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("failed to stop MCP process %d: %v", pid, signalErr))
			} else {
				result.ProcessStopped = true
			}
		}
		if removeErr := os.Remove(pidPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to remove MCP PID file: %v", removeErr))
		} else {
			result.PIDRemoved = true
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		result.Warnings = append(result.Warnings, fmt.Sprintf("failed to read MCP PID file: %v", err))
	}

	configPath := filepath.Join(dir, "hal-mcp.json")
	if err := os.Remove(configPath); err == nil {
		result.ConfigRemoved = true
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		result.Warnings = append(result.Warnings, fmt.Sprintf("failed to remove MCP config: %v", err))
	}

	if managedBinaryPath, err := defaultManagedBinaryPath(); err == nil {
		if removeErr := os.Remove(managedBinaryPath); removeErr == nil {
			result.BinaryRemoved = true
		} else if !errors.Is(removeErr, os.ErrNotExist) {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to remove managed MCP binary: %v", removeErr))
		}
	} else {
		result.Warnings = append(result.Warnings, fmt.Sprintf("managed binary path resolution failed: %v", err))
	}

	return result
}

func init() {
	createCmd.Flags().StringVar(&createCommandName, "command", "hal", "HAL command name/path to use in generated MCP client config")
	createCmd.Flags().StringVar(&createBinaryPath, "binary-path", "", "Path to write the managed HAL binary used by MCP clients (default ~/.hal/bin/hal-mcp)")
	createCmd.Flags().BoolVar(&createJSONOnly, "json", false, "Only generate/replace MCP config JSON (skip managed binary provisioning)")
	createCmd.Flags().BoolVar(&createHTTPImage, "http", false, "Build a local HAL MCP container image for streamable-http transport")
	createCmd.Flags().StringVar(&createHTTPTag, "http-tag", "hashimiche/hal-mcp:local", "Image tag used when --http is set")
	upCmd.Flags().StringVar(&upTransport, "transport", transportStdio, "MCP transport to use: stdio or streamable-http")
	upCmd.Flags().StringVar(&upHTTPHost, "http-host", "0.0.0.0", "Host/interface to bind when --transport=streamable-http")
	upCmd.Flags().IntVar(&upHTTPPort, "http-port", 8080, "TCP port to listen on when --transport=streamable-http")
	upCmd.Flags().StringVar(&upHTTPPath, "http-path", "/mcp", "HTTP path to expose when --transport=streamable-http")
	policyCmd.Flags().StringVar(&policyProfile, "profile", "strict", "Policy profile to emit (strict or standard)")
	policyCmd.Flags().BoolVar(&policyJSON, "json", true, "Print policy as JSON")

	Cmd.AddCommand(createCmd)
	Cmd.AddCommand(upCmd)
	Cmd.AddCommand(statusCmd)
	Cmd.AddCommand(downCmd)
	Cmd.AddCommand(policyCmd)
}
