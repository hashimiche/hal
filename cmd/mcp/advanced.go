package mcp

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
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
			"name":        "hal_capabilities",
			"description": "Return deterministic HAL action/alias/deprecation metadata and examples.",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			"name":        "hal_status_structured",
			"description": "Return machine-readable product/feature status with endpoint, health, and reason fields.",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			"name":        "hal_plan_action",
			"description": "Plan validated step-by-step command flow from an intent (prechecks, commands, postchecks, rollback).",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"intent": map[string]interface{}{
						"type": "string",
					},
				},
				"required": []string{"intent"},
			},
		},
		{
			"name":        "hal_validate_command",
			"description": "Validate a proposed HAL command and return validity, normalized form, and corrections.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"proposed_command": map[string]interface{}{
						"type": "string",
					},
				},
				"required": []string{"proposed_command"},
			},
		},
		{
			"name":        "hal_dry_run_action",
			"description": "Preview deploy/start actions with --dry-run, dependencies, and changed resources metadata.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type": "string",
					},
					"execute_preview": map[string]interface{}{
						"type": "boolean",
					},
				},
				"required": []string{"action"},
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
	case "hal_capabilities":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return typedToolError(errCodeInvalidInput, err.Error(), "Remove unsupported parameters and retry.", nil), true
		}
		skillMeta := map[string]interface{}{"loaded": false}
		if idx, err := getSkillIndex(); err == nil && idx != nil {
			skillMeta = map[string]interface{}{
				"loaded":              true,
				"skills_count":        len(idx.Skills),
				"commands_count":      len(idx.Commands),
				"deprecated_commands": idx.DeprecatedCommands,
			}
		}
		data := map[string]interface{}{
			"server":      mcpServerName,
			"version":     mcpServerVersion,
			"timestamp":   time.Now().UTC().Format(time.RFC3339),
			"actions":     allActionSpecs(),
			"tool_names":  collectToolNames(),
			"error_codes": supportedErrorCodes(),
			"skills":      skillMeta,
		}
		return toolSuccess(data), true
	case "hal_status_structured":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return typedToolError(errCodeInvalidInput, err.Error(), "Remove unsupported parameters and retry.", nil), true
		}
		status, err := buildStructuredStatus()
		if err != nil {
			return typedToolError(errCodeExecutionFailed, err.Error(), "Verify container engine is running and retry.", []string{"hal status"}), true
		}
		return toolSuccess(status), true
	case "hal_plan_action":
		if err := ensureOnlyKeys(args, map[string]bool{"intent": true}); err != nil {
			return typedToolError(errCodeInvalidInput, err.Error(), "Only provide intent.", nil), true
		}
		intent, _ := args["intent"].(string)
		intent = strings.ToLower(strings.TrimSpace(intent))
		if intent == "" {
			return typedToolError(errCodeInvalidInput, "intent is required", "Provide an intent like 'start vault' or 'enable vault k8s'.", nil), true
		}
		plan, ok := buildPlan(intent)
		if !ok {
			return typedToolError(errCodeUnknownAction, "unsupported intent", "Try one of: start vault, start terraform, start obs, enable vault k8s, destroy terraform.", []string{"hal_capabilities"}), true
		}
		return toolSuccess(plan), true
	case "hal_validate_command":
		if err := ensureOnlyKeys(args, map[string]bool{"proposed_command": true}); err != nil {
			return typedToolError(errCodeInvalidInput, err.Error(), "Only provide proposed_command.", nil), true
		}
		cmdText, _ := args["proposed_command"].(string)
		if strings.TrimSpace(cmdText) == "" {
			return typedToolError(errCodeInvalidInput, "proposed_command is required", "Provide a HAL command string.", nil), true
		}
		result := validateCommand(cmdText)
		return toolSuccess(result), true
	case "hal_dry_run_action":
		if err := ensureOnlyKeys(args, map[string]bool{"action": true, "execute_preview": true}); err != nil {
			return typedToolError(errCodeInvalidInput, err.Error(), "Provide action and optional execute_preview.", nil), true
		}
		actionID, _ := args["action"].(string)
		actionID = strings.TrimSpace(strings.ToLower(actionID))
		spec, ok := actionByID(actionID)
		if !ok {
			return typedToolError(errCodeUnknownAction, "unknown action", "Use hal_capabilities to list valid action ids.", []string{"hal_capabilities"}), true
		}
		executePreview := true
		if raw, ok := args["execute_preview"]; ok {
			parsed, ok := raw.(bool)
			if !ok {
				return typedToolError(errCodeInvalidInput, "execute_preview must be boolean", "Use true or false.", nil), true
			}
			executePreview = parsed
		}

		preview := map[string]interface{}{
			"action":            spec,
			"dry_run_command":   buildDryRunCommand(spec.Command),
			"execution_preview": nil,
		}
		if executePreview {
			execRes := runHAL(append([]string{"--dry-run"}, spec.Command...)...)
			preview["execution_preview"] = execRes
			if execRes.ExitCode != 0 {
				code := classifyExecutionError(execRes.Output)
				return typedToolError(code, "dry-run command failed", "Review execution_preview output and remediation hints.", []string{"hal_validate_command", "hal_diagnostics"}), true
			}
		}
		return toolSuccess(preview), true
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

func supportedErrorCodes() []typedError {
	return []typedError{
		{Code: errCodeInvalidInput, Message: "Input parameters are invalid."},
		{Code: errCodeUnknownAction, Message: "Requested action is not known to HAL MCP."},
		{Code: errCodeDependencyMissing, Message: "A required local dependency is missing."},
		{Code: errCodeNotDeployed, Message: "Target product is not deployed."},
		{Code: errCodeAuthRequired, Message: "Operation requires credentials or token."},
		{Code: errCodePortConflict, Message: "Required port appears to be in conflict."},
		{Code: errCodeExecutionFailed, Message: "Command execution failed."},
	}
}

func collectToolNames() []string {
	tools := declaredTools()
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		if name, ok := t["name"].(string); ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func allActionSpecs() []actionSpec {
	specs := baseActionSpecs()
	idx, err := getSkillIndex()
	if err != nil || idx == nil {
		return specs
	}

	byID := map[string]int{}
	for i := range specs {
		byID[specs[i].ID] = i
	}

	for actionKey, commands := range idx.CommandsByActionKey {
		targetID := mapActionKeyToSpecID(actionKey)
		if specIdx, ok := byID[targetID]; ok {
			for _, cmd := range commands {
				specs[specIdx].Examples = appendUnique(specs[specIdx].Examples, cmd)
			}
			continue
		}

		inferredCmd := []string{}
		if len(commands) > 0 {
			parts := strings.Fields(commands[0])
			if len(parts) > 1 && parts[0] == "hal" {
				inferredCmd = parts[1:]
			}
		}
		if len(inferredCmd) == 0 {
			continue
		}

		newSpec := actionSpec{
			ID:           mapActionKeyToSpecID(actionKey),
			Command:      inferredCmd,
			Aliases:      []string{},
			Examples:     commands,
			Dependencies: []string{"see_skill_docs"},
			Resources:    []string{},
			SideEffects:  "refer to HAL help and skill documentation",
			Idempotency:  "command-dependent",
			TimeoutSec:   120,
			Retry:        "consult product status and retry as appropriate",
		}
		specs = append(specs, newSpec)
		byID[newSpec.ID] = len(specs) - 1
	}

	for deprecatedCmd, replacement := range idx.DeprecatedCommands {
		parts := strings.Fields(deprecatedCmd)
		if len(parts) < 2 || parts[0] != "hal" {
			continue
		}
		actionID := mapActionKeyToSpecID(commandToActionKey(deprecatedCmd))
		specIdx, ok := byID[actionID]
		if !ok {
			inferred := actionSpec{
				ID:           actionID,
				Command:      parts[1:],
				Aliases:      []string{},
				Examples:     []string{deprecatedCmd},
				Dependencies: []string{},
				Resources:    []string{},
				SideEffects:  "none",
				Idempotency:  "n/a",
				TimeoutSec:   0,
				Retry:        "n/a",
			}
			specs = append(specs, inferred)
			specIdx = len(specs) - 1
			byID[actionID] = specIdx
		}
		specs[specIdx].Deprecated = true
		specs[specIdx].DeprecatedMsg = fmt.Sprintf("Use '%s' instead.", replacement)
		specs[specIdx].Examples = appendUnique(specs[specIdx].Examples, replacement)
	}

	sort.Slice(specs, func(i, j int) bool { return specs[i].ID < specs[j].ID })
	return specs
}

func baseActionSpecs() []actionSpec {
	return []actionSpec{
		{
			ID:           "vault_deploy",
			Command:      []string{"vault", "deploy"},
			Aliases:      []string{"start vault", "deploy vault"},
			Examples:     []string{"hal vault deploy", "hal --dry-run vault deploy"},
			Parameters:   []map[string]string{{"name": "--version", "type": "string"}, {"name": "--helper-image", "type": "string"}},
			Dependencies: []string{"docker_or_podman"},
			Resources:    []string{"hal-vault container", "vault.localhost endpoint"},
			SideEffects:  "creates/updates local Vault container resources",
			Idempotency:  "repeat-safe; converges to running state",
			TimeoutSec:   180,
			Retry:        "retry once after checking engine health",
		},
		{
			ID:           "terraform_deploy",
			Command:      []string{"terraform", "deploy"},
			Aliases:      []string{"start terraform", "deploy tfe"},
			Examples:     []string{"hal terraform deploy", "hal --dry-run terraform deploy"},
			Parameters:   []map[string]string{{"name": "--version", "type": "string"}, {"name": "--minio-api-port", "type": "int"}, {"name": "--minio-console-port", "type": "int"}},
			Dependencies: []string{"docker_or_podman", "TFE_LICENSE env var"},
			Resources:    []string{"hal-tfe", "hal-tfe-db", "hal-tfe-redis", "hal-tfe-minio", "hal-tfe-proxy"},
			SideEffects:  "creates local Terraform Enterprise stack",
			Idempotency:  "repeat-safe with existing resources; use --force for reset",
			TimeoutSec:   600,
			Retry:        "retry after fixing license or capacity issues",
		},
		{
			ID:           "obs_deploy",
			Command:      []string{"obs", "deploy"},
			Aliases:      []string{"start obs", "deploy observability"},
			Examples:     []string{"hal obs deploy", "hal --dry-run obs deploy"},
			Dependencies: []string{"docker_or_podman"},
			Resources:    []string{"hal-grafana", "hal-prometheus", "hal-loki"},
			SideEffects:  "creates observability stack containers and dashboards",
			Idempotency:  "repeat-safe",
			TimeoutSec:   240,
			Retry:        "retry once after port/conflict checks",
		},
		{
			ID:           "consul_deploy",
			Command:      []string{"consul", "deploy"},
			Aliases:      []string{"start consul", "deploy consul"},
			Examples:     []string{"hal consul deploy", "hal --dry-run consul deploy"},
			Dependencies: []string{"docker_or_podman"},
			Resources:    []string{"hal-consul container", "consul.localhost endpoint"},
			SideEffects:  "creates local Consul server resources",
			Idempotency:  "repeat-safe",
			TimeoutSec:   180,
			Retry:        "retry after checking engine health",
		},
		{
			ID:           "nomad_deploy",
			Command:      []string{"nomad", "deploy"},
			Aliases:      []string{"start nomad", "deploy nomad"},
			Examples:     []string{"hal nomad deploy", "hal --dry-run nomad deploy"},
			Dependencies: []string{"multipass"},
			Resources:    []string{"hal-nomad vm", "nomad api"},
			SideEffects:  "creates local Nomad VM and cluster services",
			Idempotency:  "repeat-safe",
			TimeoutSec:   300,
			Retry:        "retry after checking multipass health",
		},
		{
			ID:           "boundary_deploy",
			Command:      []string{"boundary", "deploy"},
			Aliases:      []string{"start boundary", "deploy boundary"},
			Examples:     []string{"hal boundary deploy", "hal --dry-run boundary deploy"},
			Dependencies: []string{"docker_or_podman"},
			Resources:    []string{"hal-boundary controller", "boundary.localhost endpoint"},
			SideEffects:  "creates local Boundary control plane resources",
			Idempotency:  "repeat-safe",
			TimeoutSec:   240,
			Retry:        "retry after checking engine health",
		},
		{
			ID:           "vault_k8s_enable",
			Command:      []string{"vault", "k8s", "--enable"},
			Aliases:      []string{"enable vault k8s", "start vault k8s"},
			Examples:     []string{"hal vault k8s --enable", "hal --dry-run vault k8s --enable"},
			Dependencies: []string{"kind", "kubectl", "helm", "docker_or_podman"},
			Resources:    []string{"kind cluster", "vault auth mounts", "vso resources"},
			SideEffects:  "provisions KinD/VSO and configures Vault paths",
			Idempotency:  "repeat-safe with occasional reconcile",
			TimeoutSec:   420,
			Retry:        "retry after checking cluster prerequisites",
		},
		{
			ID:           "vault_ldap_enable",
			Command:      []string{"vault", "ldap", "--enable"},
			Aliases:      []string{"enable vault ldap", "start vault ldap"},
			Examples:     []string{"hal vault ldap --enable", "hal --dry-run vault ldap --enable"},
			Dependencies: []string{"docker_or_podman", "vault"},
			Resources:    []string{"hal-openldap", "hal-phpldapadmin", "vault ldap auth", "vault ldap secrets"},
			SideEffects:  "creates local LDAP services and configures Vault auth/secrets engines",
			Idempotency:  "repeat-safe with --force reset option",
			TimeoutSec:   300,
			Retry:        "retry after checking Vault health",
		},
		{
			ID:           "vault_database_enable",
			Command:      []string{"vault", "database", "--enable"},
			Aliases:      []string{"enable vault database", "start vault database"},
			Examples:     []string{"hal vault database --enable", "hal --dry-run vault database --enable --backend mariadb"},
			Dependencies: []string{"docker_or_podman", "vault"},
			Resources:    []string{"hal-vault-mariadb", "vault database secrets engine"},
			SideEffects:  "creates selected database backend and configures Vault dynamic credentials",
			Idempotency:  "repeat-safe with cleanup on reset",
			TimeoutSec:   300,
			Retry:        "retry after checking Vault health",
		},
		{
			ID:           "boundary_ssh_enable",
			Command:      []string{"boundary", "ssh", "--enable"},
			Aliases:      []string{"enable boundary ssh", "start boundary ssh"},
			Examples:     []string{"hal boundary ssh --enable", "hal --dry-run boundary ssh --enable"},
			Dependencies: []string{"multipass", "boundary"},
			Resources:    []string{"hal-boundary-ssh vm", "boundary ssh target resources"},
			SideEffects:  "creates SSH target VM and wires Boundary target resources",
			Idempotency:  "repeat-safe with --force reset option",
			TimeoutSec:   240,
			Retry:        "retry after checking Boundary and multipass health",
		},
		{
			ID:           "boundary_mariadb_enable",
			Command:      []string{"boundary", "mariadb", "--enable"},
			Aliases:      []string{"enable boundary mariadb", "start boundary mariadb"},
			Examples:     []string{"hal boundary mariadb --enable", "hal --dry-run boundary mariadb --enable", "hal boundary mariadb --enable --with-vault"},
			Dependencies: []string{"docker_or_podman", "boundary"},
			Resources:    []string{"hal-boundary-target-mariadb", "boundary mariadb target resources"},
			SideEffects:  "creates MariaDB target and wires Boundary resources, optionally Vault brokering",
			Idempotency:  "repeat-safe with --force reset option",
			TimeoutSec:   240,
			Retry:        "retry after checking Boundary health",
		},
		{
			ID:            "terraform_token",
			Command:       []string{"terraform", "token"},
			Aliases:       []string{"tfe token"},
			Deprecated:    true,
			DeprecatedMsg: "Use 'hal terraform workspace --enable' instead.",
			Examples:      []string{"hal terraform workspace --enable"},
			SideEffects:   "none",
			Idempotency:   "n/a",
			TimeoutSec:    0,
			Retry:         "n/a",
		},
	}
}

func actionByID(id string) (actionSpec, bool) {
	for _, s := range allActionSpecs() {
		if s.ID == id {
			return s, true
		}
	}
	return actionSpec{}, false
}

func buildDryRunCommand(cmd []string) string {
	parts := append([]string{"hal", "--dry-run"}, cmd...)
	return strings.Join(parts, " ")
}

func buildPlan(intent string) (map[string]interface{}, bool) {
	plans := map[string]map[string]interface{}{
		"start vault": {
			"intent":     "start vault",
			"prechecks":  []string{"hal status", "hal capacity"},
			"steps":      []map[string]string{{"command": "hal vault deploy", "reason": "deploy core Vault"}},
			"postchecks": []string{"hal vault status", "hal status"},
			"rollback":   []string{"hal vault destroy"},
		},
		"start terraform": {
			"intent":     "start terraform",
			"prechecks":  []string{"hal status", "hal capacity", "echo $TFE_LICENSE"},
			"steps":      []map[string]string{{"command": "hal terraform deploy", "reason": "deploy local TFE stack"}, {"command": "hal terraform workspace --enable", "reason": "wire GitLab workspace automation"}},
			"postchecks": []string{"hal terraform status", "hal status"},
			"rollback":   []string{"hal terraform destroy"},
		},
		"start obs": {
			"intent":     "start obs",
			"prechecks":  []string{"hal status", "hal capacity"},
			"steps":      []map[string]string{{"command": "hal obs deploy", "reason": "deploy Grafana/Prometheus/Loki"}},
			"postchecks": []string{"hal obs status", "hal status"},
			"rollback":   []string{"hal obs destroy"},
		},
		"start consul": {
			"intent":     "start consul",
			"prechecks":  []string{"hal status", "hal capacity"},
			"steps":      []map[string]string{{"command": "hal consul deploy", "reason": "deploy Consul control plane"}},
			"postchecks": []string{"hal consul status", "hal status"},
			"rollback":   []string{"hal consul destroy"},
		},
		"start nomad": {
			"intent":     "start nomad",
			"prechecks":  []string{"hal status", "multipass version"},
			"steps":      []map[string]string{{"command": "hal nomad deploy", "reason": "deploy Nomad VM and cluster"}},
			"postchecks": []string{"hal nomad status", "hal status"},
			"rollback":   []string{"hal nomad destroy"},
		},
		"start boundary": {
			"intent":     "start boundary",
			"prechecks":  []string{"hal status", "hal vault status"},
			"steps":      []map[string]string{{"command": "hal boundary deploy", "reason": "deploy Boundary control plane"}},
			"postchecks": []string{"hal boundary status", "hal status"},
			"rollback":   []string{"hal boundary destroy"},
		},
		"enable vault k8s": {
			"intent":     "enable vault k8s",
			"prechecks":  []string{"hal vault status", "kind --version", "kubectl version --client", "helm version"},
			"steps":      []map[string]string{{"command": "hal vault k8s --enable", "reason": "provision KinD + VSO flow"}},
			"postchecks": []string{"hal vault k8s", "hal status"},
			"rollback":   []string{"hal vault k8s --disable"},
		},
		"destroy terraform": {
			"intent":     "destroy terraform",
			"prechecks":  []string{"hal terraform status"},
			"steps":      []map[string]string{{"command": "hal terraform destroy", "reason": "remove TFE stack"}},
			"postchecks": []string{"hal terraform status", "hal status"},
			"rollback":   []string{"hal terraform deploy", "hal terraform workspace --enable"},
		},
	}

	if p, ok := plans[intent]; ok {
		return p, true
	}

	intentNorm := strings.TrimSpace(strings.ToLower(intent))
	for _, spec := range allActionSpecs() {
		if spec.Deprecated {
			continue
		}
		candidateKeys := make([]string, 0, len(spec.Aliases)+len(spec.Examples)+2)
		candidateKeys = append(candidateKeys, spec.ID)
		candidateKeys = append(candidateKeys, strings.Join(spec.Command, " "))
		candidateKeys = append(candidateKeys, spec.Aliases...)
		for _, ex := range spec.Examples {
			candidateKeys = append(candidateKeys, strings.TrimSpace(strings.TrimPrefix(ex, "hal ")))
		}

		matched := false
		for _, key := range candidateKeys {
			k := strings.ToLower(strings.TrimSpace(key))
			if k == "" {
				continue
			}
			if intentNorm == k || strings.Contains(intentNorm, k) || strings.Contains(k, intentNorm) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}

		if len(spec.Command) == 0 {
			continue
		}
		root := spec.Command[0]
		rollback := []string{"hal status"}
		if root != "status" && root != "capacity" && root != "catalog" && root != "destroy" && root != "version" && root != "mcp" {
			rollback = []string{fmt.Sprintf("hal %s status", root), fmt.Sprintf("hal %s destroy", root)}
		}

		planned := map[string]interface{}{
			"intent":     intent,
			"prechecks":  []string{"hal status", "hal capacity"},
			"steps":      []map[string]string{{"command": "hal " + strings.Join(spec.Command, " "), "reason": "generated from skills-backed action catalog"}},
			"postchecks": []string{fmt.Sprintf("hal %s status", root), "hal status"},
			"rollback":   rollback,
		}
		return planned, true
	}
	return nil, false
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
		result["suggestions"] = []string{"hal terraform workspace --enable"}
		result["normalized_command"] = normalized
		return result
	}

	validProducts := map[string][]string{
		"status":    {},
		"capacity":  {},
		"catalog":   {},
		"destroy":   {},
		"version":   {},
		"mcp":       {"create", "up", "status", "down"},
		"vault":     {"deploy", "status", "destroy", "audit", "jwt", "k8s", "ldap", "database", "oidc", "db"},
		"consul":    {"deploy", "status", "destroy"},
		"nomad":     {"deploy", "status", "destroy", "job"},
		"boundary":  {"deploy", "status", "destroy", "mariadb", "ssh"},
		"terraform": {"deploy", "status", "destroy", "workspace", "cli", "agent"},
		"obs":       {"deploy", "status", "destroy"},
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

func mapActionKeyToSpecID(actionKey string) string {
	if actionKey == "" {
		return "unknown_action"
	}
	return strings.ReplaceAll(strings.TrimSpace(strings.ToLower(actionKey)), "-", "_")
}

func commandToActionKey(command string) string {
	key, ok := inferActionKeyFromCommand(normalizeDisplayCommand(command))
	if !ok {
		return "unknown_action"
	}
	return key
}

func buildStructuredStatus() (map[string]interface{}, error) {
	engine, err := global.DetectEngine()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	products := []map[string]interface{}{
		buildProductState(engine, "consul", []string{"hal-consul"}, map[string]string{"core": boolState(checkContainer(engine, "hal-consul"))}, "http://consul.localhost:8500"),
		buildProductState(engine, "vault", []string{"hal-vault"}, map[string]string{"audit": resolveVaultAuditFeature(engine), "k8s": boolState(checkContainer(engine, "kind-control-plane")), "jwt": boolState(checkContainer(engine, "hal-gitlab")), "ldap": boolState(checkContainer(engine, "hal-openldap")), "database": boolState(checkContainer(engine, "hal-vault-mariadb")), "oidc": boolState(checkContainer(engine, "hal-keycloak"))}, "http://vault.localhost:8200"),
		buildProductState(engine, "nomad", []string{"hal-nomad"}, map[string]string{"job": boolState(checkMultipass("hal-nomad"))}, "multipass://hal-nomad"),
		buildProductState(engine, "boundary", []string{"hal-boundary"}, map[string]string{"mariadb": boolState(checkContainer(engine, "hal-boundary-target-mariadb")), "ssh": boolState(checkMultipass("hal-boundary-ssh"))}, "http://boundary.localhost:9200"),
		buildProductState(engine, "terraform", []string{"hal-tfe", "hal-tfe-db", "hal-tfe-redis", "hal-tfe-minio", "hal-tfe-proxy"}, map[string]string{"workspace": boolState(checkContainer(engine, "hal-tfe") && checkContainer(engine, "hal-gitlab"))}, "https://tfe.localhost:8443"),
		buildProductState(engine, "obs", []string{"hal-grafana", "hal-prometheus", "hal-loki"}, map[string]string{"grafana": boolState(checkContainer(engine, "hal-grafana")), "prometheus": boolState(checkContainer(engine, "hal-prometheus")), "loki": boolState(checkContainer(engine, "hal-loki"))}, "http://grafana.localhost:3000"),
	}

	return map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"engine":    engine,
		"products":  products,
		"generated": now,
	}, nil
}

func buildProductState(engine, product string, containers []string, features map[string]string, endpoint string) map[string]interface{} {
	runningCount := 0
	for _, c := range containers {
		if c == "hal-nomad" {
			if checkMultipass("hal-nomad") {
				runningCount++
			}
			continue
		}
		if checkContainer(engine, c) {
			runningCount++
		}
	}

	state := "not_deployed"
	health := "down"
	reason := "required resources are not running"
	if runningCount > 0 && runningCount < len(containers) {
		state = "partial"
		health = "degraded"
		reason = "some resources are running"
	}
	if runningCount == len(containers) {
		state = "running"
		health = "healthy"
		reason = "all required resources are running"
	}
	if len(containers) == 1 && runningCount == 1 {
		state = "running"
		health = "healthy"
		reason = "primary resource is running"
	}

	featureRows := make([]map[string]string, 0, len(features))
	for k, v := range features {
		healthState := "down"
		reasonState := "feature is disabled"
		if v == "enabled" {
			healthState = "healthy"
			reasonState = "feature is enabled"
		}
		featureRows = append(featureRows, map[string]string{
			"feature": k,
			"state":   v,
			"health":  healthState,
			"reason":  reasonState,
		})
	}
	sort.Slice(featureRows, func(i, j int) bool { return featureRows[i]["feature"] < featureRows[j]["feature"] })

	return map[string]interface{}{
		"product":    product,
		"state":      state,
		"health":     health,
		"reason":     reason,
		"endpoint":   endpoint,
		"containers": containers,
		"features":   featureRows,
	}
}

func resolveVaultAuditFeature(engine string) string {
	if !checkContainer(engine, "hal-vault") {
		return "disabled"
	}
	out, err := exec.Command(
		engine,
		"exec",
		"-e",
		"VAULT_ADDR=http://127.0.0.1:8200",
		"-e",
		"VAULT_TOKEN=root",
		"hal-vault",
		"vault",
		"audit",
		"list",
		"-format=json",
	).Output()
	if err != nil {
		return "unknown"
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "{}" || trimmed == "" {
		return "disabled"
	}
	return "enabled"
}

func checkContainer(engine, name string) bool {
	out, err := exec.Command(engine, "ps", "-q", "-f", fmt.Sprintf("name=^%s$", name)).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

func checkMultipass(name string) bool {
	out, err := exec.Command("multipass", "info", name, "--format", "csv").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Running")
}

func boolState(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func buildDiagnostics(product string, tailLines int) (map[string]interface{}, error) {
	engine, err := global.DetectEngine()
	if err != nil {
		return nil, fmt.Errorf("engine detection failed: %w", err)
	}

	containersByProduct := map[string][]string{
		"vault":     {"hal-vault", "hal-openldap", "hal-keycloak", "hal-mariadb", "hal-gitlab"},
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
		if !checkContainer(engine, c) {
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
