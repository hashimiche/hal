package mcp

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"hal/internal/global"
)

const (
	errCodeInvalidInput      = "INVALID_INPUT"
	errCodeUnknownAction     = "UNKNOWN_ACTION"
	errCodeDependencyMissing = "MISSING_DEPENDENCY"
	errCodeNotDeployed       = "NOT_DEPLOYED"
	errCodeAuthRequired      = "AUTH_REQUIRED"
	errCodePortConflict      = "PORT_CONFLICT"
	errCodeExecutionFailed   = "EXECUTION_FAILED"
)

type typedError struct {
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	Remediation string   `json:"remediation,omitempty"`
	Next        []string `json:"next,omitempty"`
}

type actionSpec struct {
	ID            string              `json:"id"`
	Command       []string            `json:"command"`
	Aliases       []string            `json:"aliases"`
	Deprecated    bool                `json:"deprecated"`
	DeprecatedMsg string              `json:"deprecated_message,omitempty"`
	Examples      []string            `json:"examples"`
	Parameters    []map[string]string `json:"parameters,omitempty"`
	Dependencies  []string            `json:"dependencies,omitempty"`
	Resources     []string            `json:"changed_resources,omitempty"`
	SideEffects   string              `json:"side_effects"`
	Idempotency   string              `json:"idempotency"`
	TimeoutSec    int                 `json:"timeout_seconds"`
	Retry         string              `json:"retry"`
}

func mcpAdvancedTools() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"name":        "hal_status_structured",
			"description": "Return machine-readable product/feature status with endpoint, health, and reason fields.",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			"name":        "hal_diagnostics",
			"description": "Return structured diagnostics for a product: recent logs, last failure hint, and health probe summary.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"product": map[string]interface{}{
						"type": "string",
						"enum": []string{"vault", "consul", "nomad", "boundary", "terraform", "obs"},
					},
					"tail_lines": map[string]interface{}{
						"type": "integer",
					},
				},
				"required": []string{"product"},
			},
		},
	}
}

func handleAdvancedTool(name string, args map[string]interface{}) (mcpToolCallResult, bool) {
	switch strings.TrimSpace(name) {
	case "hal_status_structured":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return typedToolError(errCodeInvalidInput, err.Error(), "Remove unsupported parameters and retry.", nil), true
		}
		status, err := buildStructuredStatus()
		if err != nil {
			return typedToolError(errCodeExecutionFailed, err.Error(), "Verify container engine is running and retry.", []string{"hal status"}), true
		}
		return toolSuccess(status), true
	case "hal_diagnostics":
		if err := ensureOnlyKeys(args, map[string]bool{"product": true, "tail_lines": true}); err != nil {
			return typedToolError(errCodeInvalidInput, err.Error(), "Provide product and optional tail_lines.", nil), true
		}
		product, _ := args["product"].(string)
		product = strings.ToLower(strings.TrimSpace(product))
		tailLines := 120
		if raw, ok := args["tail_lines"]; ok {
			switch v := raw.(type) {
			case float64:
				tailLines = int(v)
			default:
				return typedToolError(errCodeInvalidInput, "tail_lines must be integer", "Use a numeric value between 20 and 500.", nil), true
			}
		}
		if tailLines < 20 {
			tailLines = 20
		}
		if tailLines > 500 {
			tailLines = 500
		}
		diag, err := buildDiagnostics(product, tailLines)
		if err != nil {
			return typedToolError(classifyExecutionError(err.Error()), err.Error(), "Check product deployment and engine health.", []string{"hal status", fmt.Sprintf("hal %s status", product)}), true
		}
		return toolSuccess(diag), true
	default:
		return mcpToolCallResult{}, false
	}
}

func toolSuccess(data interface{}) mcpToolCallResult {
	body, _ := json.MarshalIndent(data, "", "  ")
	return mcpToolCallResult{
		Content:           []mcpTextContent{{Type: "text", Text: string(body)}},
		StructuredContent: data,
	}
}

func typedToolError(code, message, remediation string, next []string) mcpToolCallResult {
	errObj := typedError{Code: code, Message: message, Remediation: remediation, Next: next}
	body, _ := json.MarshalIndent(map[string]interface{}{"ok": false, "error": errObj}, "", "  ")
	return mcpToolCallResult{
		Content:           []mcpTextContent{{Type: "text", Text: string(body)}},
		IsError:           true,
		StructuredContent: map[string]interface{}{"ok": false, "error": errObj},
	}
}

func validateCommand(proposed string) map[string]interface{} {
	raw := strings.TrimSpace(proposed)
	parts := strings.Fields(raw)
	result := map[string]interface{}{
		"proposed_command":   raw,
		"valid":              false,
		"normalized_command": "",
		"errors":             []string{},
		"suggestions":        []string{},
	}
	if len(parts) == 0 {
		result["errors"] = []string{"empty command"}
		result["suggestions"] = []string{"hal --help"}
		return result
	}

	if parts[0] != "hal" {
		result["errors"] = []string{"command must start with 'hal'"}
		result["suggestions"] = []string{"hal --help"}
		return result
	}

	if len(parts) == 1 {
		result["valid"] = true
		result["normalized_command"] = "hal"
		return result
	}

	alias := map[string]string{"tf": "terraform", "observability": "obs"}
	if normalized, ok := alias[parts[1]]; ok {
		parts[1] = normalized
	}

	normalized := strings.Join(parts, " ")
	if idx, err := getSkillIndex(); err == nil && idx != nil {
		if replacement, ok := idx.DeprecatedCommands[normalized]; ok {
			result["errors"] = []string{fmt.Sprintf("deprecated command: %s", normalized)}
			result["suggestions"] = []string{replacement}
			result["normalized_command"] = normalized
			return result
		}
	}

	if len(parts) >= 3 && parts[1] == "terraform" && parts[2] == "token" {
		result["errors"] = []string{"deprecated command: hal terraform token"}
		result["suggestions"] = []string{"hal terraform workspace enable"}
		result["normalized_command"] = normalized
		return result
	}

	validProducts := map[string][]string{
		"status":    {},
		"capacity":  {},
		"catalog":   {},
		"delete":    {},
		"version":   {},
		"daisy":     {},
		"creds":     {"status"},
		"health":    {"create", "update", "delete"},
		"plus":      {"create", "status", "delete"},
		"mcp":       {"create", "serve", "status", "delete"},
		"vault":     {"create", "status", "delete", "update", "audit", "oidc", "jwt", "k8s", "ldap", "database", "db", "userpass", "up", "os", "pki", "obs"},
		"consul":    {"create", "status", "delete", "update", "obs"},
		"nomad":     {"create", "status", "delete", "update", "job", "obs"},
		"boundary":  {"create", "status", "delete", "update", "mariadb", "ssh", "obs"},
		"terraform": {"create", "status", "delete", "update", "agent", "api-workflow", "api", "vcs-workflow", "vcs", "workspace", "ws", "twin", "bis", "dup", "saml", "obs"},
		"obs":       {"create", "status", "delete", "update"},
	}

	root := parts[1]
	subs, ok := validProducts[root]
	if !ok {
		result["errors"] = []string{"unknown root command"}
		result["suggestions"] = []string{"hal --help"}
		return result
	}

	if len(parts) == 2 {
		result["valid"] = true
		result["normalized_command"] = strings.Join(parts, " ")
		return result
	}

	if len(subs) == 0 {
		if strings.HasPrefix(parts[2], "-") {
			result["valid"] = true
			result["normalized_command"] = strings.Join(parts, " ")
			return result
		}
		result["errors"] = []string{"unexpected subcommand"}
		result["suggestions"] = []string{fmt.Sprintf("hal %s --help", root)}
		result["normalized_command"] = strings.Join(parts, " ")
		return result
	}

	sub := parts[2]
	for _, allowed := range subs {
		if allowed == sub {
			result["valid"] = true
			result["normalized_command"] = strings.Join(parts, " ")
			return result
		}
	}
	if strings.HasPrefix(sub, "-") {
		result["valid"] = true
		result["normalized_command"] = strings.Join(parts, " ")
		return result
	}

	result["errors"] = []string{"unknown subcommand"}
	result["suggestions"] = []string{fmt.Sprintf("hal %s --help", root)}
	result["normalized_command"] = strings.Join(parts, " ")
	return result
}

func buildStructuredStatus() (map[string]interface{}, error) {
	engine, err := global.DetectEngine()
	if err != nil {
		return nil, err
	}

	snap, err := global.BuildStatusSnapshot(engine)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(snap, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func buildDiagnostics(product string, tailLines int) (map[string]interface{}, error) {
	engine, err := global.DetectEngine()
	if err != nil {
		return nil, fmt.Errorf("engine detection failed: %w", err)
	}

	containersByProduct := map[string][]string{
		"vault":     {"hal-vault", "hal-openldap", "hal-authentik-server", "hal-authentik-worker", "hal-mariadb", "hal-gitlab"},
		"consul":    {"hal-consul"},
		"nomad":     {},
		"boundary":  {"hal-boundary", "hal-boundary-target-mariadb"},
		"terraform": {"hal-tfe", "hal-tfe-db", "hal-tfe-redis", "hal-tfe-minio", "hal-tfe-proxy"},
		"obs":       {"hal-grafana", "hal-prometheus", "hal-loki"},
	}
	containers, ok := containersByProduct[product]
	if !ok {
		return nil, fmt.Errorf("unsupported product: %s", product)
	}

	logs := map[string]string{}
	failureHints := map[string]string{}
	reFailure := regexp.MustCompile(`(?i)(error|failed|panic|denied|refused|timeout)`) // safe heuristic

	for _, c := range containers {
		if !global.CheckContainer(engine, c) {
			logs[c] = "container not running"
			failureHints[c] = "container not running"
			continue
		}
		out, _ := exec.Command(engine, "logs", "--tail", strconv.Itoa(tailLines), c).CombinedOutput()
		text := string(out)
		logs[c] = text
		lines := strings.Split(text, "\n")
		hint := ""
		for i := len(lines) - 1; i >= 0; i-- {
			if reFailure.MatchString(lines[i]) {
				hint = strings.TrimSpace(lines[i])
				break
			}
		}
		if hint == "" {
			hint = "no obvious failure line detected"
		}
		failureHints[c] = hint
	}

	healthProbe := runHAL(product, "status")
	return map[string]interface{}{
		"product":              product,
		"engine":               engine,
		"tail_lines":           tailLines,
		"health_probe_summary": healthProbe,
		"last_failure_cause":   failureHints,
		"recent_logs":          logs,
		"timeout_seconds":      30,
		"retry_semantics":      "safe to retry diagnostics commands immediately",
		"side_effects":         "none",
		"idempotent":           true,
		"generated_at":         time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func classifyExecutionError(output string) string {
	lower := strings.ToLower(output)
	switch {
	case strings.Contains(lower, "license") || strings.Contains(lower, "token") || strings.Contains(lower, "unauthorized"):
		return errCodeAuthRequired
	case strings.Contains(lower, "port is already allocated") || strings.Contains(lower, "address already in use"):
		return errCodePortConflict
	case strings.Contains(lower, "not running") || strings.Contains(lower, "not deployed"):
		return errCodeNotDeployed
	case strings.Contains(lower, "not found") || strings.Contains(lower, "missing") || strings.Contains(lower, "command not found"):
		return errCodeDependencyMissing
	default:
		return errCodeExecutionFailed
	}
}
