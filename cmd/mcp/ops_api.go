package mcp

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hal/cmd/creds"
	"hal/internal/global"
)

const (
	statusSuccess = "success"
	statusError   = "error"
)

const (
	codeCommandNotFound     = "command_not_found"
	codeInvalidFlag         = "invalid_flag"
	codeMissingDependency   = "missing_dependency"
	codeNotDeployed         = "not_deployed"
	codeNotAuthenticated    = "not_authenticated"
	codePermissionDenied    = "permission_denied"
	codeEndpointUnreachable = "endpoint_unreachable"
	codeTimeout             = "timeout"
	codeParseError          = "parse_error"
	codeUnsupportedOp       = "unsupported_operation"
	mcpContractVersion      = "2026-04-13"
	mcpPolicyVersion        = "2026-04-13"
)

type opContractResponse struct {
	ContractVersion     string         `json:"contract_version,omitempty"`
	Status              string         `json:"status"`
	Code                string         `json:"code"`
	Message             string         `json:"message"`
	Domain              string         `json:"domain"`
	Capability          string         `json:"capability"`
	Resource            string         `json:"resource"`
	Data                interface{}    `json:"data"`
	RecommendedCommands []string       `json:"recommended_commands"`
	Checks              []opCheck      `json:"checks"`
	NextSteps           []opNextStep   `json:"next_steps,omitempty"`
	Credentials         *opCredentials `json:"credentials,omitempty"`
	Grounding           *opGrounding   `json:"grounding,omitempty"`
	Docs                []string       `json:"docs"`
}

type opCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Details string `json:"details,omitempty"`
}

type opNextStep struct {
	Order           int      `json:"order"`
	Title           string   `json:"title"`
	ExpectedOutcome string   `json:"expected_outcome"`
	Commands        []string `json:"commands,omitempty"`
}

type opCredentials struct {
	References []string `json:"references,omitempty"`
	Redacted   bool     `json:"redacted"`
}

type opGrounding struct {
	Source     string  `json:"source"`
	Mode       string  `json:"mode"`
	Confidence float64 `json:"confidence"`
	Profile    string  `json:"profile,omitempty"`
	Version    string  `json:"version,omitempty"`
}

func mcpOpsTools() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"name":        "get_runtime_status",
			"description": "Return products, versions, endpoints, deployment state and feature state in structured form.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "hal_status_baseline",
			"description": "Alias of get_runtime_status for deterministic LLM routing.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "get_vault_status",
			"description": "Return Vault core runtime status and checks.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "get_terraform_status",
			"description": "Return Terraform Enterprise runtime status and checks.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "get_audit_summary",
			"description": "Return compact Vault audit behavioral summary and key events for timeframe/filter.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"timeframe": map[string]interface{}{"type": "string"},
					"filter":    map[string]interface{}{"type": "string"},
				},
			},
		},
		{
			"name":        "get_oidc_status",
			"description": "Return OIDC status, mount path, config completeness and missing fields.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "get_jwt_status",
			"description": "Return JWT auth mount status, config completeness and missing fields.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "get_boundary_status",
			"description": "Return Boundary lifecycle status and critical checks.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "get_tfe_status",
			"description": "Return Terraform Enterprise runtime and workspace wiring status.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "get_tfe_api_workflow_status",
			"description": "Return Terraform API helper readiness for local TFE workflows.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "get_tfe_vcs_workflow_status",
			"description": "Return Terraform VCS-driven workflow readiness: shared GitLab + TFE workspace wiring, endpoints, lab credentials, and the manual push trigger.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "get_k8s_integration_status",
			"description": "Return Vault Kubernetes integration readiness including VSO/CSI checks.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "get_vault_pki_status",
			"description": "Return Vault PKI readiness: Root/Intermediate CA engines, hal-role, and the optional cert-manager (--k8s) and ACME/Caddy (--acme) demo layers.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "get_ldap_status",
			"description": "Return Vault LDAP demo readiness and key checks.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "get_vault_database_status",
			"description": "Return Vault database secrets demo readiness and key checks.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "get_boundary_mariadb_status",
			"description": "Return Boundary MariaDB target readiness and key checks.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "get_consul_status",
			"description": "Return Consul runtime status and checks.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "get_nomad_status",
			"description": "Return Nomad runtime status and checks.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "get_obs_status",
			"description": "Return observability stack status and checks.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "get_active_credentials",
			"description": "Return structured per-service credentials for currently running lab services (URLs, usernames, demo passwords, and the cached TFE API token) — the structured equivalent of `hal creds status`.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "get_capabilities",
			"description": "Return HAL's capability catalog: exposed MCP tools, real CLI commands, action keys, and embedded skills count. Engine-independent and read-only.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "hal_policy_profile",
			"description": "Return the HAL runtime answer/tool policy profile (hal_first, strict by default) that grounds assistant behavior. Read-only.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"profile": map[string]interface{}{
						"type":        "string",
						"description": "policy profile name (default: strict)",
					},
				},
			},
		},
		{
			"name":        "validate_command",
			"description": "Validate a proposed HAL command against the real command surface (rejects unknown/deprecated commands, normalizes aliases). Read-only; never executes.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "proposed hal command to validate, e.g. 'hal vault status'",
					},
					"proposed_command": map[string]interface{}{
						"type":        "string",
						"description": "alias of command",
					},
				},
			},
		},
	}
}

func handleOpsTool(name string, args map[string]interface{}) (mcpToolCallResult, bool) {
	switch strings.TrimSpace(name) {
	case "get_runtime_status", "hal_status_baseline":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opError(codeParseError, err.Error(), nil, []string{"hal status"}, nil), true
		}
		status, err := buildStructuredStatus()
		if err != nil {
			return opError(classifyContractError(err.Error()), err.Error(), nil, []string{"hal status"}, nil), true
		}
		usage := map[string]interface{}{}
		if engine, ok := status["engine"].(string); ok {
			if u, uErr := buildEngineUsage(engine); uErr == nil {
				usage = u
			}
		}
		data := map[string]interface{}{"runtime": status, "engine_usage": usage}
		return opSuccess("runtime status collected", data, []string{"hal status", "hal capacity"}, nil), true

	case "get_vault_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_vault_status", codeParseError, err.Error(), nil, []string{"hal vault status"}, nil, nil, nil), true
		}
		return handleStatusCommandTool("get_vault_status", []string{"vault", "status"}, []string{"hal vault status", "hal vault create"}, []string{"https://developer.hashicorp.com/vault"}), true

	case "get_terraform_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_terraform_status", codeParseError, err.Error(), nil, []string{"hal terraform status"}, nil, nil, nil), true
		}
		return handleTerraformRuntimeStatus("get_terraform_status"), true

	case "get_audit_summary":
		if err := ensureOnlyKeys(args, map[string]bool{"timeframe": true, "filter": true}); err != nil {
			return opError(codeParseError, err.Error(), nil, []string{"hal vault audit --help"}, nil), true
		}
		timeframe := "15m"
		if raw, ok := args["timeframe"]; ok {
			if v, ok := raw.(string); ok && strings.TrimSpace(v) != "" {
				timeframe = strings.TrimSpace(v)
			}
		}
		filter := ""
		if raw, ok := args["filter"]; ok {
			if v, ok := raw.(string); ok {
				filter = strings.TrimSpace(v)
			}
		}
		summary := buildAuditSummary(timeframe, filter)
		return opSuccess("audit summary generated", summary, []string{"hal vault status", "hal vault audit"}, []string{"https://developer.hashicorp.com/vault/docs/audit"}), true

	case "get_oidc_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opError(codeParseError, err.Error(), nil, []string{"hal vault oidc --help"}, nil), true
		}
		status, rec, err := buildOIDCOrJWTStatus("oidc")
		if err != nil {
			return opError(classifyContractError(err.Error()), err.Error(), nil, rec, []string{"https://developer.hashicorp.com/vault/docs/auth/jwt"}), true
		}
		return opSuccess("oidc status collected", status, rec, []string{"https://developer.hashicorp.com/vault/docs/auth/jwt/oidc-providers"}), true

	case "get_jwt_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opError(codeParseError, err.Error(), nil, []string{"hal vault jwt --help"}, nil), true
		}
		status, rec, err := buildOIDCOrJWTStatus("jwt")
		if err != nil {
			return opError(classifyContractError(err.Error()), err.Error(), nil, rec, []string{"https://developer.hashicorp.com/vault/docs/auth/jwt"}), true
		}
		return opSuccess("jwt status collected", status, rec, []string{"https://developer.hashicorp.com/vault/docs/auth/jwt"}), true

	case "get_boundary_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_boundary_status", codeParseError, err.Error(), nil, []string{"hal boundary status"}, nil, nil, nil), true
		}
		execRes := runHAL("boundary", "status")
		checks := []opCheck{{Name: "boundary_status_command", Status: statusFromExecution(execRes), Details: strings.TrimSpace(execRes.Output)}}
		if execRes.ExitCode != 0 {
			return opErrorForTool("get_boundary_status", classifyContractError(execRes.Output), "boundary status check failed; run recovery commands", map[string]interface{}{"execution": execRes}, []string{"hal boundary create", "hal boundary status"}, checks, nil, []string{"https://developer.hashicorp.com/boundary"}), true
		}
		return opSuccessForTool("get_boundary_status", "boundary status collected", map[string]interface{}{"execution": execRes}, []string{"hal boundary status", "hal boundary ssh"}, checks, nil, nil, []string{"https://developer.hashicorp.com/boundary"}), true

	case "get_tfe_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_tfe_status", codeParseError, err.Error(), nil, []string{"hal terraform status"}, nil, nil, nil), true
		}
		return handleTFEStatus(), true

	case "get_tfe_api_workflow_status", "get_tfe_cli_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_tfe_api_workflow_status", codeParseError, err.Error(), nil, []string{"hal terraform api-workflow"}, nil, nil, nil), true
		}
		return handleTFECLIStatus(), true

	case "get_tfe_vcs_workflow_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_tfe_vcs_workflow_status", codeParseError, err.Error(), nil, []string{"hal terraform vcs-workflow"}, nil, nil, nil), true
		}
		return handleTFEVCSWorkflowStatus(), true

	case "get_k8s_integration_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_k8s_integration_status", codeParseError, err.Error(), nil, []string{"hal vault k8s"}, nil, nil, nil), true
		}
		execRes := runHAL("vault", "k8s")
		checks := []opCheck{{Name: "vault_k8s_status", Status: statusFromExecution(execRes), Details: "vault k8s/vso/csi check"}}
		if execRes.ExitCode != 0 {
			return opErrorForTool("get_k8s_integration_status", classifyContractError(execRes.Output), "k8s integration check failed; inspect vault and kind prerequisites", map[string]interface{}{"execution": execRes}, []string{"hal vault status", "hal vault k8s enable"}, checks, nil, nil), true
		}
		return opSuccessForTool("get_k8s_integration_status", "vault k8s integration status collected", map[string]interface{}{"execution": execRes}, []string{"hal vault k8s", "hal vault k8s enable", "hal vault k8s enable --csi"}, checks, nil, nil, nil), true

	case "get_vault_pki_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_vault_pki_status", codeParseError, err.Error(), nil, []string{"hal vault pki"}, nil, nil, nil), true
		}
		execRes := runHAL("vault", "pki")
		checks := []opCheck{{Name: "vault_pki_status", Status: statusFromExecution(execRes), Details: "vault pki engines + cert-manager/acme demo check"}}
		if execRes.ExitCode != 0 {
			return opErrorForTool("get_vault_pki_status", classifyContractError(execRes.Output), "vault pki check failed; ensure Vault is reachable and PKI engines are enabled", map[string]interface{}{"execution": execRes}, []string{"hal vault status", "hal vault pki enable"}, checks, nil, nil), true
		}
		return opSuccessForTool("get_vault_pki_status", "vault pki status collected", map[string]interface{}{"execution": execRes}, []string{"hal vault pki", "hal vault pki enable", "hal vault pki enable --acme", "hal vault pki enable --k8s"}, checks, nil, nil, []string{"https://developer.hashicorp.com/vault/docs/secrets/pki"}), true

	case "get_ldap_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_ldap_status", codeParseError, err.Error(), nil, []string{"hal vault ldap"}, nil, nil, nil), true
		}
		return handleStatusCommandTool("get_ldap_status", []string{"vault", "ldap"}, []string{"hal vault ldap", "hal vault ldap enable"}, []string{"https://developer.hashicorp.com/vault/docs/auth/ldap"}), true

	case "get_vault_database_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_vault_database_status", codeParseError, err.Error(), nil, []string{"hal vault database"}, nil, nil, nil), true
		}
		return handleStatusCommandTool("get_vault_database_status", []string{"vault", "database"}, []string{"hal vault database", "hal vault database enable --backend mariadb"}, []string{"https://developer.hashicorp.com/vault/docs/secrets/databases"}), true

	case "get_boundary_mariadb_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_boundary_mariadb_status", codeParseError, err.Error(), nil, []string{"hal boundary mariadb"}, nil, nil, nil), true
		}
		return handleStatusCommandTool("get_boundary_mariadb_status", []string{"boundary", "mariadb"}, []string{"hal boundary mariadb", "hal boundary mariadb enable"}, []string{"https://developer.hashicorp.com/boundary"}), true

	case "get_consul_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_consul_status", codeParseError, err.Error(), nil, []string{"hal consul status"}, nil, nil, nil), true
		}
		return handleStatusCommandTool("get_consul_status", []string{"consul", "status"}, []string{"hal consul status", "hal consul create"}, []string{"https://developer.hashicorp.com/consul"}), true

	case "get_nomad_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_nomad_status", codeParseError, err.Error(), nil, []string{"hal nomad status"}, nil, nil, nil), true
		}
		return handleStatusCommandTool("get_nomad_status", []string{"nomad", "status"}, []string{"hal nomad status", "hal nomad create"}, []string{"https://developer.hashicorp.com/nomad"}), true

	case "get_obs_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_obs_status", codeParseError, err.Error(), nil, []string{"hal obs status"}, nil, nil, nil), true
		}
		return handleStatusCommandTool("get_obs_status", []string{"obs", "status"}, []string{"hal obs status", "hal obs create"}, []string{"https://grafana.com/docs/", "https://prometheus.io/docs/", "https://grafana.com/oss/loki/"}), true

	case "get_active_credentials":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_active_credentials", codeParseError, err.Error(), nil, []string{"hal creds status"}, nil, nil, nil), true
		}
		return handleActiveCredentials(), true

	case "get_capabilities":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_capabilities", codeParseError, err.Error(), nil, []string{"hal catalog"}, nil, nil, nil), true
		}
		caps := buildCapabilities()
		checks := []opCheck{{Name: "capabilities", Status: "ok", Details: "catalog enumerated"}}
		return opSuccessForTool("get_capabilities", "hal capability catalog collected", caps, []string{"hal catalog", "hal --help"}, checks, nil, nil, nil), true

	case "hal_policy_profile":
		if err := ensureOnlyKeys(args, map[string]bool{"profile": true}); err != nil {
			return opErrorForTool("hal_policy_profile", codeParseError, err.Error(), nil, []string{"hal mcp status"}, nil, nil, nil), true
		}
		profile := "strict"
		if raw, ok := args["profile"]; ok {
			if v, ok := raw.(string); ok && strings.TrimSpace(v) != "" {
				profile = strings.TrimSpace(v)
			}
		}
		checks := []opCheck{{Name: "policy_profile", Status: "ok", Details: profile}}
		return opSuccessForTool("hal_policy_profile", "hal runtime policy profile collected", buildPolicyProfile(profile), []string{"hal mcp status", "hal status"}, checks, nil, nil, nil), true

	case "validate_command":
		if err := ensureOnlyKeys(args, map[string]bool{"command": true, "proposed_command": true}); err != nil {
			return opErrorForTool("validate_command", codeParseError, err.Error(), nil, []string{"hal --help"}, nil, nil, nil), true
		}
		proposed := ""
		if raw, ok := args["command"]; ok {
			if v, ok := raw.(string); ok {
				proposed = strings.TrimSpace(v)
			}
		}
		if proposed == "" {
			if raw, ok := args["proposed_command"]; ok {
				if v, ok := raw.(string); ok {
					proposed = strings.TrimSpace(v)
				}
			}
		}
		if proposed == "" {
			return opErrorForTool("validate_command", codeParseError, "command is required; run a recommended command for remediation", nil, []string{"hal --help"}, nil, nil, nil), true
		}
		return handleValidateCommand(proposed), true

	default:
		return mcpToolCallResult{}, false
	}
}

func handleValidateCommand(proposed string) mcpToolCallResult {
	check := validateCommand(proposed)
	valid, _ := check["valid"].(bool)
	normalized, _ := check["normalized_command"].(string)
	suggestions := []string{}
	if raw, ok := check["suggestions"].([]string); ok {
		suggestions = raw
	}
	if valid {
		commands := []string{}
		if strings.HasPrefix(normalized, "hal") {
			commands = append(commands, normalized)
		}
		commands = append(commands, "hal --help")
		checks := []opCheck{{Name: "command_validation", Status: "ok", Details: normalized}}
		return opSuccessForTool("validate_command", "proposed command is valid against the hal command surface", check, commands, checks, nil, nil, nil)
	}
	commands := []string{"hal --help"}
	if len(suggestions) > 0 {
		commands = suggestions
	}
	checks := []opCheck{{Name: "command_validation", Status: "warn", Details: normalized}}
	return opErrorForTool("validate_command", codeCommandNotFound, "proposed command failed validation; run a recommended command for remediation", check, commands, checks, nil, nil)
}

// buildCapabilities enumerates HAL's read-only capability surface: exposed MCP
// tools, real CLI commands, action keys, and embedded skills. Engine-independent.
func buildCapabilities() map[string]interface{} {
	toolNames := []string{}
	for _, tool := range declaredTools() {
		if n, ok := tool["name"].(string); ok && strings.TrimSpace(n) != "" {
			toolNames = append(toolNames, strings.TrimSpace(n))
		}
	}
	sort.Strings(toolNames)

	commands := []string{}
	actions := []string{}
	skillNames := []string{}
	deprecated := map[string]string{}
	skillsCount := 0
	if idx, err := getSkillIndex(); err == nil && idx != nil {
		commands = append(commands, idx.Commands...)
		for key := range idx.CommandsByActionKey {
			actions = append(actions, key)
		}
		sort.Strings(actions)
		for _, skill := range idx.Skills {
			name := strings.TrimSpace(skill.Name)
			if name == "" {
				name = skill.Path
			}
			skillNames = append(skillNames, name)
		}
		skillsCount = len(idx.Skills)
		for k, v := range idx.DeprecatedCommands {
			deprecated[k] = v
		}
	}
	// Guarantee a non-empty actions surface even if skills are unavailable.
	if len(actions) == 0 {
		actions = append(actions, toolNames...)
	}

	return map[string]interface{}{
		"contract_version":    mcpContractVersion,
		"policy_version":      mcpPolicyVersion,
		"tools":               toolNames,
		"actions":             actions,
		"commands":            commands,
		"deprecated_commands": deprecated,
		"skills": map[string]interface{}{
			"skills_count": skillsCount,
			"names":        skillNames,
		},
	}
}

// buildPolicyProfile returns the HAL runtime answer/tool policy profile that
// grounds assistant behavior. The required_prefetch_tools all map to real,
// registered MCP tools.
func buildPolicyProfile(profile string) map[string]interface{} {
	if strings.TrimSpace(profile) == "" {
		profile = "strict"
	}
	return map[string]interface{}{
		"policy_version":   mcpPolicyVersion,
		"contract_version": mcpContractVersion,
		"profile":          profile,
		"answer_policy": map[string]interface{}{
			"mode":                           "hal_first",
			"disallow_unverified_claims":     true,
			"disallow_non_hal_primary_paths": true,
			"include_verification_commands":  true,
			"include_official_docs":          true,
		},
		"tool_policy": map[string]interface{}{
			"required_prefetch_tools": []string{"hal_status_baseline", "get_capabilities", "hal_policy_profile", "validate_command"},
			"on_uncertain_then_call":  []string{"validate_command", "hal_help", "get_capabilities"},
			"fallback": map[string]interface{}{
				"mode":         "fail_closed",
				"allow_answer": false,
				"message":      "HAL MCP policy unavailable; run hal mcp status and retry.",
			},
		},
		"recommended_bootstrap": []string{"hal mcp status", "hal status", "hal --help"},
		"source":                "hal-mcp-runtime",
	}
}

func handleActiveCredentials() mcpToolCallResult {
	active, err := creds.CollectActiveCredentials()
	if err != nil {
		return opErrorForTool("get_active_credentials", classifyContractError(err.Error()), err.Error(), nil, []string{"hal creds status"}, nil, nil, nil)
	}
	data := map[string]interface{}{"any_active": active.AnyActive, "services": active.Services}
	if !active.AnyActive {
		return opErrorForTool("get_active_credentials", codeNotDeployed, "no active lab services detected; start a service first", data, []string{"hal vault create", "hal terraform create"}, nil, nil, nil)
	}
	return opSuccessForTool("get_active_credentials", "active lab credentials collected", data, []string{"hal creds status"}, nil, nil, nil, nil)
}

func opSuccess(message string, data interface{}, commands []string, docs []string) mcpToolCallResult {
	return opSuccessForTool("ops", message, data, commands, []opCheck{{Name: "contract", Status: "ok", Details: "schema envelope populated"}}, nil, nil, docs)
}

func opError(code string, message string, data interface{}, commands []string, docs []string) mcpToolCallResult {
	return opErrorForTool("ops", code, message, data, commands, []opCheck{{Name: "contract", Status: "warn", Details: "error envelope populated"}}, nil, docs)
}

func opSuccessForTool(toolName, message string, data interface{}, commands []string, checks []opCheck, next []opNextStep, creds *opCredentials, docs []string) mcpToolCallResult {
	resp := opContractResponse{
		ContractVersion:     mcpContractVersion,
		Status:              statusSuccess,
		Code:                "ok",
		Message:             strings.TrimSpace(message),
		Domain:              domainForTool(toolName),
		Capability:          capabilityForTool(toolName),
		Resource:            resourceForTool(toolName),
		Data:                data,
		RecommendedCommands: sanitizeRecommendedCommands(commands),
		Checks:              normalizeChecks(checks),
		NextSteps:           normalizeNextSteps(next),
		Credentials:         creds,
		Grounding:           defaultGrounding(toolName),
		Docs:                sortedUnique(docs),
	}
	if err := validateContractEnvelope(resp); err != nil {
		resp = contractValidationFailure(toolName, err)
	}
	body, _ := json.MarshalIndent(resp, "", "  ")
	return mcpToolCallResult{Content: []mcpTextContent{{Type: "text", Text: string(body)}}, StructuredContent: resp}
}

func opErrorForTool(toolName, code, message string, data interface{}, commands []string, checks []opCheck, next []opNextStep, docs []string) mcpToolCallResult {
	if code == "" {
		code = codeUnsupportedOp
	}
	if !strings.Contains(strings.ToLower(message), "run") {
		message = strings.TrimSpace(message) + "; run a recommended command for remediation"
	}
	resp := opContractResponse{
		ContractVersion:     mcpContractVersion,
		Status:              statusError,
		Code:                code,
		Message:             message,
		Domain:              domainForTool(toolName),
		Capability:          capabilityForTool(toolName),
		Resource:            resourceForTool(toolName),
		Data:                data,
		RecommendedCommands: sanitizeRecommendedCommands(commands),
		Checks:              normalizeChecks(checks),
		NextSteps:           normalizeNextSteps(next),
		Grounding:           defaultGrounding(toolName),
		Docs:                sortedUnique(docs),
	}
	if err := validateContractEnvelope(resp); err != nil {
		resp = contractValidationFailure(toolName, err)
	}
	body, _ := json.MarshalIndent(resp, "", "  ")
	return mcpToolCallResult{Content: []mcpTextContent{{Type: "text", Text: string(body)}}, IsError: true, StructuredContent: resp}
}

func contractValidationFailure(toolName string, err error) opContractResponse {
	return opContractResponse{
		ContractVersion:     mcpContractVersion,
		Status:              statusError,
		Code:                codeParseError,
		Message:             "contract validation failed: " + strings.TrimSpace(err.Error()) + "; run a recommended command for remediation",
		Domain:              domainForTool(toolName),
		Capability:          capabilityForTool(toolName),
		Resource:            "validation",
		Data:                map[string]interface{}{"validation_error": err.Error()},
		RecommendedCommands: []string{"hal --help"},
		Checks:              []opCheck{{Name: "contract_validation", Status: "error", Details: strings.TrimSpace(err.Error())}},
		Grounding:           defaultGrounding(toolName),
		Docs:                []string{},
	}
}

func validateContractEnvelope(resp opContractResponse) error {
	if resp.Status != statusSuccess && resp.Status != statusError {
		return fmt.Errorf("status must be success or error")
	}
	if strings.TrimSpace(resp.Code) == "" {
		return fmt.Errorf("code is required")
	}
	if strings.TrimSpace(resp.ContractVersion) == "" {
		return fmt.Errorf("contract_version is required")
	}
	if strings.TrimSpace(resp.Message) == "" {
		return fmt.Errorf("message is required")
	}
	allowedDomains := map[string]bool{"hal": true, "vault": true, "boundary": true, "tfe": true, "consul": true, "nomad": true, "obs": true, "terraform": true, "k8s": true, "cross-product": true}
	if !allowedDomains[resp.Domain] {
		return fmt.Errorf("invalid domain: %s", resp.Domain)
	}
	if strings.TrimSpace(resp.Capability) == "" {
		return fmt.Errorf("capability is required")
	}
	if strings.TrimSpace(resp.Resource) == "" {
		return fmt.Errorf("resource is required")
	}
	if len(resp.Checks) == 0 {
		return fmt.Errorf("checks must contain at least one item")
	}
	allowedCheckStatuses := map[string]bool{"ok": true, "warn": true, "error": true, "unknown": true}
	for _, check := range resp.Checks {
		if strings.TrimSpace(check.Name) == "" {
			return fmt.Errorf("check name is required")
		}
		if !allowedCheckStatuses[check.Status] {
			return fmt.Errorf("invalid check status: %s", check.Status)
		}
	}
	if err := validateCommandList(resp.RecommendedCommands); err != nil {
		return err
	}
	for _, step := range resp.NextSteps {
		if step.Order < 1 {
			return fmt.Errorf("next_steps order must be >= 1")
		}
		if strings.TrimSpace(step.Title) == "" || strings.TrimSpace(step.ExpectedOutcome) == "" {
			return fmt.Errorf("next_steps title and expected_outcome are required")
		}
		if err := validateCommandList(step.Commands); err != nil {
			return fmt.Errorf("invalid next_steps commands: %w", err)
		}
	}
	for _, raw := range resp.Docs {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		u, err := url.ParseRequestURI(raw)
		if err != nil || u.Scheme == "" {
			return fmt.Errorf("invalid docs URI: %s", raw)
		}
	}
	if resp.Credentials != nil && !resp.Credentials.Redacted {
		return fmt.Errorf("credentials must be redacted by default")
	}
	if resp.Grounding != nil {
		if strings.TrimSpace(resp.Grounding.Source) == "" {
			return fmt.Errorf("grounding source is required")
		}
		allowedModes := map[string]bool{"tool_verified": true, "fallback": true}
		if !allowedModes[resp.Grounding.Mode] {
			return fmt.Errorf("invalid grounding mode: %s", resp.Grounding.Mode)
		}
		if resp.Grounding.Confidence < 0 || resp.Grounding.Confidence > 1 {
			return fmt.Errorf("grounding confidence must be between 0 and 1")
		}
	}
	return nil
}

func defaultGrounding(toolName string) *opGrounding {
	profile := "standard"
	if strings.Contains(toolName, "policy") || strings.Contains(toolName, "status") || strings.Contains(toolName, "validate") {
		profile = "strict"
	}
	return &opGrounding{
		Source:     "hal-mcp",
		Mode:       "tool_verified",
		Confidence: 1,
		Profile:    profile,
		Version:    mcpPolicyVersion,
	}
}

func validateCommandList(commands []string) error {
	allowedPrefixes := []string{"hal", "vault", "boundary", "consul", "nomad", "terraform", "kubectl", "curl", "jq"}
	for _, cmd := range commands {
		trimmed := strings.TrimSpace(cmd)
		if trimmed == "" {
			return fmt.Errorf("empty command")
		}
		ok := false
		for _, p := range allowedPrefixes {
			if trimmed == p || strings.HasPrefix(trimmed, p+" ") {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("command has invalid prefix: %s", trimmed)
		}
	}
	return nil
}

func sanitizeRecommendedCommands(commands []string) []string {
	allowedPrefixes := []string{"hal ", "vault ", "boundary ", "consul ", "nomad ", "terraform ", "kubectl ", "curl ", "jq "}
	out := []string{}
	for _, cmd := range sortedUnique(commands) {
		trimmed := strings.TrimSpace(cmd)
		if trimmed == "" {
			continue
		}
		allowed := false
		for _, p := range allowedPrefixes {
			if strings.HasPrefix(trimmed, p) || trimmed == strings.TrimSpace(p) {
				allowed = true
				break
			}
		}
		if !allowed {
			continue
		}
		if strings.HasPrefix(trimmed, "hal ") {
			res := validateCommand(trimmed)
			if valid, ok := res["valid"].(bool); !ok || !valid {
				continue
			}
			if normalized, ok := res["normalized_command"].(string); ok && strings.TrimSpace(normalized) != "" {
				trimmed = normalized
			}
		}
		out = append(out, trimmed)
	}
	return sortedUnique(out)
}

func normalizeChecks(checks []opCheck) []opCheck {
	if len(checks) == 0 {
		return []opCheck{{Name: "status", Status: "unknown", Details: "no checks provided"}}
	}
	allowed := map[string]bool{"ok": true, "warn": true, "error": true, "unknown": true}
	out := make([]opCheck, 0, len(checks))
	for _, c := range checks {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		st := strings.ToLower(strings.TrimSpace(c.Status))
		if !allowed[st] {
			st = "unknown"
		}
		out = append(out, opCheck{Name: name, Status: st, Details: strings.TrimSpace(c.Details)})
	}
	if len(out) == 0 {
		return []opCheck{{Name: "status", Status: "unknown", Details: "no checks provided"}}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func normalizeNextSteps(next []opNextStep) []opNextStep {
	if len(next) == 0 {
		return nil
	}
	out := make([]opNextStep, 0, len(next))
	for i, step := range next {
		order := step.Order
		if order <= 0 {
			order = i + 1
		}
		out = append(out, opNextStep{
			Order:           order,
			Title:           strings.TrimSpace(step.Title),
			ExpectedOutcome: strings.TrimSpace(step.ExpectedOutcome),
			Commands:        sanitizeRecommendedCommands(step.Commands),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out
}

func statusFromExecution(execRes toolExecution) string {
	if execRes.ExitCode == 0 {
		return "ok"
	}
	return "error"
}

func domainForTool(toolName string) string {
	switch {
	case strings.Contains(toolName, "oidc") || strings.Contains(toolName, "jwt") || strings.Contains(toolName, "vault") || strings.Contains(toolName, "audit"):
		return "vault"
	case strings.Contains(toolName, "boundary") || strings.Contains(toolName, "ssh"):
		return "boundary"
	case strings.Contains(toolName, "tfe"):
		return "tfe"
	case strings.Contains(toolName, "terraform"):
		return "terraform"
	case strings.Contains(toolName, "consul"):
		return "consul"
	case strings.Contains(toolName, "nomad"):
		return "nomad"
	case strings.Contains(toolName, "obs"):
		return "obs"
	case strings.Contains(toolName, "k8s"):
		return "k8s"
	case strings.Contains(toolName, "cross"):
		return "cross-product"
	default:
		return "hal"
	}
}

func capabilityForTool(toolName string) string {
	if strings.TrimSpace(toolName) == "" {
		return "general"
	}
	return strings.TrimSpace(toolName)
}

func resourceForTool(toolName string) string {
	if strings.Contains(toolName, "runtime") {
		return "runtime"
	}
	if strings.Contains(toolName, "status") {
		return "status"
	}
	if strings.Contains(toolName, "enable") || strings.Contains(toolName, "setup") {
		return "workflow"
	}
	if strings.Contains(toolName, "dependencies") {
		return "dependencies"
	}
	return "general"
}

func contractErrorCodes() []string {
	return []string{
		codeCommandNotFound,
		codeInvalidFlag,
		codeMissingDependency,
		codeNotDeployed,
		codeNotAuthenticated,
		codePermissionDenied,
		codeEndpointUnreachable,
		codeTimeout,
		codeParseError,
		codeUnsupportedOp,
	}
}

func classifyContractError(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "unknown flag") || strings.Contains(lower, "invalid flag"):
		return codeInvalidFlag
	case strings.Contains(lower, "not found") || strings.Contains(lower, "unknown root command") || strings.Contains(lower, "unknown subcommand"):
		return codeCommandNotFound
	case strings.Contains(lower, "permission") || strings.Contains(lower, "denied"):
		return codePermissionDenied
	case strings.Contains(lower, "token") || strings.Contains(lower, "unauthorized") || strings.Contains(lower, "auth"):
		return codeNotAuthenticated
	case strings.Contains(lower, "timeout"):
		return codeTimeout
	case strings.Contains(lower, "unreachable") || strings.Contains(lower, "connection refused") || strings.Contains(lower, "cannot connect"):
		return codeEndpointUnreachable
	case strings.Contains(lower, "not deployed") || strings.Contains(lower, "not running"):
		return codeNotDeployed
	case strings.Contains(lower, "command not found") || strings.Contains(lower, "missing"):
		return codeMissingDependency
	default:
		return codeUnsupportedOp
	}
}

func sortedUnique(items []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func buildEngineUsage(engine string) (map[string]interface{}, error) {
	usage, err := global.GetEngineUsage(engine)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"cpu_total":                 usage.CPUs,
		"memory_mb":                 usage.MemoryMB,
		"live_cpu_percent":          usage.LiveCPUPercent,
		"live_memory_mb":            usage.LiveMemMB,
		"container_cpu_percent_sum": usage.ContainerCPUPercent,
		"container_memory_mb":       usage.ContainerMemMB,
		"container_count":           usage.ContainerCount,
		"source":                    usage.LiveSource,
	}, nil
}

func componentContext(component string) (map[string]interface{}, []string, error) {
	switch component {
	case "vault":
		return map[string]interface{}{
			"component": component,
			"endpoint":  "http://127.0.0.1:8200",
			"auth": map[string]interface{}{
				"token_reference": "VAULT_TOKEN (local lab default is configured in HAL runtime)",
				"sensitive":       true,
			},
			"state": map[string]interface{}{
				"vault_container_running": global.CheckContainer("podman", "hal-vault") || global.CheckContainer("docker", "hal-vault"),
			},
		}, []string{"hal vault status", "hal vault create", "hal vault audit"}, nil
	case "oidc", "vault_oidc":
		status, cmds, err := buildOIDCOrJWTStatus("oidc")
		return status, cmds, err
	case "jwt", "vault_jwt":
		status, cmds, err := buildOIDCOrJWTStatus("jwt")
		return status, cmds, err
	case "vault_k8s":
		return map[string]interface{}{
			"component": "vault_k8s",
			"platform":  "kind + helm + kubectl",
			"modes":     []string{"native", "csi", "jwt"},
			"flags":     []string{"--update", "--csi", "--jwt-auth"},
			"endpoint":  "http://web.localhost:8088",
		}, []string{"hal vault k8s", "hal vault k8s enable", "hal vault k8s enable --csi", "hal vault k8s disable"}, nil
	case "vault_vso":
		return map[string]interface{}{
			"component":   "vault_vso",
			"implemented": "hal vault k8s workflow",
			"runtime":     "vault-secrets-operator in namespace vso",
			"health_hint": "helm list -n vso",
		}, []string{"hal vault k8s", "hal vault k8s enable"}, nil
	case "vault_csi":
		return map[string]interface{}{
			"component":   "vault_csi",
			"implemented": "hal vault k8s --csi",
			"runtime":     "VSO CSI projection mode",
			"health_hint": "kubectl get pods -n vso",
		}, []string{"hal vault k8s enable --csi", "hal vault k8s"}, nil
	case "vault_ldap":
		return map[string]interface{}{
			"component":  "vault_ldap",
			"status_cmd": "hal vault ldap",
			"notes":      "Use command without flags for smart status and next-step guidance",
		}, []string{"hal vault ldap", "hal vault ldap enable", "hal vault ldap disable"}, nil
	case "vault_database":
		return map[string]interface{}{
			"component":  "vault_database",
			"status_cmd": "hal vault database",
			"notes":      "Use command without flags for smart status and next-step guidance",
		}, []string{"hal vault database", "hal vault database enable", "hal vault database disable"}, nil
	case "terraform":
		return map[string]interface{}{
			"component": component,
			"endpoint":  "https://tfe.localhost:8443",
			"auth": map[string]interface{}{
				"token_reference": "~/.hal/tfe-app-api-token",
				"sensitive":       true,
			},
			"license": map[string]interface{}{
				"environment_variable": "TFE_LICENSE",
				"required":             true,
			},
			"browser": map[string]interface{}{
				"self_signed_certificate": true,
				"user_action":             "accept browser risk warning",
			},
			"related_endpoints": []string{
				"http://127.0.0.1:19000",
				"http://127.0.0.1:19001",
				"http://grafana.localhost:3000",
				"http://prometheus.localhost:9090",
			},
		}, []string{"hal terraform status", "hal terraform create", "hal terraform vcs-workflow", "hal terraform api-workflow", "hal terraform agent"}, nil
	case "terraform_vcs_workflow", "terraform_workspace":
		return map[string]interface{}{
			"component":  "terraform_vcs_workflow",
			"depends_on": []string{"hal-tfe", "hal-gitlab"},
			"workflow":   "prepare gitlab repo + wire TFE workspace VCS",
			"trigger":    "push commit to main branch",
		}, []string{"hal terraform vcs-workflow", "hal terraform vcs-workflow enable", "hal terraform status"}, nil
	case "terraform_agent":
		return map[string]interface{}{
			"component":         "terraform_agent",
			"agent_container":   "hal-tfe-agent",
			"default_pool_name": "hal-agent-pool",
			"default_image":     "hashicorp/tfc-agent:1.28",
			"workflow":          "create/reuse agent pool, mint token, register local tfc-agent",
		}, []string{"hal terraform agent", "hal terraform agent enable", "hal terraform agent disable", "hal terraform status"}, nil
	case "terraform_api_workflow", "terraform_cli":
		return map[string]interface{}{
			"component":        "terraform_api_workflow",
			"helper_container": "hal-tfe-api",
			"default_org":      "hal",
			"auth_files":       []string{"/root/.tfx.hcl", "/root/.terraform.d/credentials.tfrc.json"},
			"seeded_projects":  []string{"Dave", "Frank"},
			"workflow":         "build helper image if needed, then open helper shell against local TFE",
		}, []string{"hal terraform api-workflow", "hal tf api-workflow enable", "hal terraform status"}, nil
	case "consul":
		return map[string]interface{}{"component": component, "endpoint": "http://consul.localhost:8500"}, []string{"hal consul status"}, nil
	case "nomad":
		return map[string]interface{}{"component": component, "endpoint": "multipass://hal-nomad"}, []string{"hal nomad status"}, nil
	case "boundary":
		return map[string]interface{}{"component": component, "endpoint": "http://boundary.localhost:9200"}, []string{"hal boundary status", "hal boundary create", "hal boundary ssh"}, nil
	case "boundary_ssh":
		return map[string]interface{}{
			"component": "boundary_ssh",
			"platform":  "multipass",
			"vm":        "hal-boundary-ssh",
			"flags":     []string{"--update"},
		}, []string{"hal boundary ssh", "hal boundary ssh enable", "hal boundary ssh disable"}, nil
	case "boundary_mariadb":
		return map[string]interface{}{
			"component":  "boundary_mariadb",
			"status_cmd": "hal boundary mariadb",
			"notes":      "Use command without flags for smart status and next-step guidance",
		}, []string{"hal boundary mariadb", "hal boundary mariadb enable", "hal boundary mariadb disable"}, nil
	case "obs":
		return map[string]interface{}{"component": component, "endpoints": []string{"http://grafana.localhost:3000", "http://prometheus.localhost:9090", "http://loki.localhost:3100/ready"}}, []string{"hal obs status"}, nil
	default:
		return nil, nil, fmt.Errorf("unsupported component: %s", component)
	}
}

func buildAuditSummary(timeframe, filter string) map[string]interface{} {
	summary := map[string]interface{}{
		"timeframe": timeframe,
		"filter":    filter,
		"generated": time.Now().UTC().Format(time.RFC3339),
		"signals":   []string{},
		"raw":       map[string]interface{}{},
	}
	execRes := runHAL("vault", "status")
	summary["raw"] = map[string]interface{}{"vault_status": execRes}
	signals := []string{}
	outLower := strings.ToLower(execRes.Output)
	if strings.Contains(outLower, "down") {
		signals = append(signals, "vault appears down")
	}
	if strings.Contains(outLower, "up") {
		signals = append(signals, "vault appears up")
	}
	if strings.Contains(outLower, "audit") && strings.Contains(outLower, "enabled") {
		signals = append(signals, "vault audit appears enabled")
	}
	summary["signals"] = sortedUnique(signals)
	return summary
}

func buildOIDCOrJWTStatus(mode string) (map[string]interface{}, []string, error) {
	engine, err := global.DetectEngine()
	if err != nil {
		return nil, []string{"hal status"}, err
	}
	if !global.CheckContainer(engine, "hal-vault") {
		return map[string]interface{}{"enabled": false, "mount_path": mode + "/", "config_complete": false, "missing_fields": []string{"vault_not_running"}}, []string{"hal vault create", "hal vault status"}, fmt.Errorf("vault is not deployed")
	}

	authPath := mode + "/"
	if mode == "oidc" {
		authPath = "oidc/"
	}
	authList := runVaultAuthList(engine)
	enabled := strings.Contains(authList, authPath)
	missing := []string{}
	if !enabled {
		missing = append(missing, "auth_mount")
	}
	if mode == "oidc" && !global.CheckContainer(engine, "hal-keycloak") {
		missing = append(missing, "keycloak_provider")
	}
	if mode == "jwt" && !global.CheckContainer(engine, "hal-gitlab") {
		missing = append(missing, "gitlab_provider")
	}
	status := map[string]interface{}{
		"mode":            mode,
		"enabled":         enabled,
		"mount_path":      authPath,
		"config_complete": len(missing) == 0,
		"missing_fields":  missing,
		"auth_state":      map[string]interface{}{"sensitive_fields": []string{"client_secret", "jwt_validation_pubkeys"}, "secure_mode_required": true},
	}
	recommended := []string{"hal vault status"}
	if mode == "oidc" {
		recommended = append(recommended, "hal vault oidc enable")
	} else {
		recommended = append(recommended, "hal vault jwt enable")
	}
	return status, sortedUnique(recommended), nil
}

func runVaultAuthList(engine string) string {
	out, err := exec.Command(
		engine,
		"exec",
		"-e",
		"VAULT_ADDR=http://127.0.0.1:8200",
		"-e",
		"VAULT_TOKEN=root",
		"hal-vault",
		"vault",
		"auth",
		"list",
		"-format=json",
	).CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
}

func handleStatusCommandTool(toolName string, command []string, recommended []string, docs []string) mcpToolCallResult {
	execRes := runHAL(command...)
	checks := []opCheck{{Name: strings.Join(command, "_"), Status: statusFromExecution(execRes), Details: "status command result"}}
	data := map[string]interface{}{"execution": execRes}
	if execRes.ExitCode != 0 {
		return opErrorForTool(toolName, classifyContractError(execRes.Output), "status command failed; run recovery commands", data, recommended, checks, nil, docs)
	}
	return opSuccessForTool(toolName, "status collected", data, recommended, checks, nil, nil, docs)
}

func terraformRuntimeState() (string, map[string]interface{}, error) {
	status, err := buildStructuredStatus()
	if err != nil {
		return "", nil, err
	}
	engine, _ := status["engine"].(string)
	products, _ := status["products"].([]map[string]interface{})
	for _, product := range products {
		if name, _ := product["product"].(string); name == "terraform" {
			return engine, product, nil
		}
	}
	return engine, nil, fmt.Errorf("terraform runtime state unavailable")
}

func terraformFeatureState(product map[string]interface{}, featureName string) string {
	features, _ := product["features"].([]map[string]string)
	for _, feature := range features {
		if feature["feature"] == featureName {
			return feature["state"]
		}
	}
	return "unknown"
}

func checkStatusFromState(state string) string {
	switch strings.TrimSpace(strings.ToLower(state)) {
	case "running":
		return "ok"
	case "enabled":
		return "ok"
	case "partial":
		return "warn"
	case "not_deployed":
		return "error"
	case "disabled":
		return "error"
	default:
		return "unknown"
	}
}

func runtimeCodeFromState(state string) string {
	switch strings.TrimSpace(strings.ToLower(state)) {
	case "running":
		return "ok"
	case "partial":
		return codeEndpointUnreachable
	case "not_deployed":
		return codeNotDeployed
	default:
		return codeTimeout
	}
}

func handleTerraformRuntimeStatus(toolName string) mcpToolCallResult {
	_, product, err := terraformRuntimeState()
	if err != nil {
		return opErrorForTool(toolName, codeTimeout, err.Error(), nil, []string{"hal status", "hal terraform status"}, []opCheck{{Name: "terraform_runtime", Status: "error", Details: "unable to resolve runtime state"}}, nil, []string{"https://developer.hashicorp.com/terraform/enterprise"})
	}
	state, _ := product["state"].(string)
	reason, _ := product["reason"].(string)
	checks := []opCheck{{Name: "terraform_runtime", Status: checkStatusFromState(state), Details: reason}}
	data := map[string]interface{}{"runtime": product}
	if state != "running" {
		return opErrorForTool(toolName, runtimeCodeFromState(state), "terraform enterprise runtime not healthy; deploy terraform first", data, []string{"hal terraform create", "hal terraform status"}, checks, nil, []string{"https://developer.hashicorp.com/terraform/enterprise"})
	}
	return opSuccessForTool(toolName, "terraform runtime status collected", data, []string{"hal terraform status", "hal terraform create"}, checks, nil, nil, []string{"https://developer.hashicorp.com/terraform/enterprise"})
}

func handleTFEStatus() mcpToolCallResult {
	_, product, err := terraformRuntimeState()
	if err != nil {
		return opErrorForTool("get_tfe_status", codeTimeout, err.Error(), nil, []string{"hal status", "hal terraform status"}, []opCheck{{Name: "terraform_runtime", Status: "error", Details: "unable to resolve runtime state"}}, nil, []string{"https://developer.hashicorp.com/terraform/enterprise"})
	}
	state, _ := product["state"].(string)
	reason, _ := product["reason"].(string)
	workspaceState := terraformFeatureState(product, "workspace")
	checks := []opCheck{
		{Name: "terraform_runtime", Status: checkStatusFromState(state), Details: reason},
		{Name: "terraform_workspace", Status: checkStatusFromState(workspaceState), Details: "workspace automation readiness"},
	}
	data := map[string]interface{}{
		"runtime":         product,
		"workspace_state": workspaceState,
		"workspace_hint":  "Use hal terraform vcs-workflow for workspace-specific guidance.",
	}
	if state != "running" {
		return opErrorForTool("get_tfe_status", runtimeCodeFromState(state), "tfe runtime not healthy; deploy terraform first", data, []string{"hal terraform create", "hal terraform status"}, checks, nil, []string{"https://developer.hashicorp.com/terraform/enterprise"})
	}
	return opSuccessForTool("get_tfe_status", "tfe status collected", data, []string{"hal terraform status", "hal terraform vcs-workflow enable"}, checks, nil, nil, []string{"https://developer.hashicorp.com/terraform/enterprise"})
}

func handleTFECLIStatus() mcpToolCallResult {
	engine, product, err := terraformRuntimeState()
	if err != nil {
		return opErrorForTool("get_tfe_cli_status", codeTimeout, err.Error(), nil, []string{"hal status", "hal terraform status"}, []opCheck{{Name: "terraform_runtime", Status: "error", Details: "unable to resolve runtime state"}}, nil, []string{"https://developer.hashicorp.com/terraform/enterprise"})
	}
	state, _ := product["state"].(string)
	reason, _ := product["reason"].(string)
	newHelperReady := global.CheckContainer(engine, "hal-tfe-api")
	legacyHelperReady := global.CheckContainer(engine, "hal-tfe-cli")
	cliHelperReady := newHelperReady || legacyHelperReady
	helperContainerName := "hal-tfe-api"
	if !newHelperReady && legacyHelperReady {
		helperContainerName = "hal-tfe-cli"
	}
	homeDir, _ := os.UserHomeDir()
	tokenPath := filepath.Join(homeDir, ".hal", "tfe-app-api-token")
	_, tokenErr := os.Stat(tokenPath)
	tokenReady := tokenErr == nil
	checks := []opCheck{
		{Name: "terraform_runtime", Status: checkStatusFromState(state), Details: reason},
		{Name: "terraform_cli_helper", Status: checkStatusFromState(global.BoolState(cliHelperReady)), Details: helperContainerName + " helper availability"},
		{Name: "terraform_cli_token_cache", Status: checkStatusFromState(global.BoolState(tokenReady)), Details: tokenPath},
	}
	data := map[string]interface{}{
		"runtime": product,
		"cli_helper": map[string]interface{}{
			"container": helperContainerName,
			"state":     global.BoolState(cliHelperReady),
		},
		"token_cache": map[string]interface{}{
			"path":    tokenPath,
			"present": tokenReady,
		},
	}
	if state != "running" {
		return opErrorForTool("get_tfe_cli_status", runtimeCodeFromState(state), "tfe runtime not healthy; deploy terraform first", data, []string{"hal terraform create", "hal terraform status"}, checks, nil, []string{"https://developer.hashicorp.com/terraform/enterprise"})
	}
	if !cliHelperReady {
		return opErrorForTool("get_tfe_cli_status", codeNotDeployed, "tfe api helper is not ready; run hal terraform api-workflow", data, []string{"hal terraform api-workflow", "hal tf api-workflow enable"}, checks, nil, []string{"https://developer.hashicorp.com/terraform/enterprise"})
	}
	return opSuccessForTool("get_tfe_cli_status", "tfe api helper status collected", data, []string{"hal terraform api-workflow", "hal tf api-workflow enable"}, checks, nil, nil, []string{"https://developer.hashicorp.com/terraform/enterprise"})
}

// handleTFEVCSWorkflowStatus reports readiness of the Terraform VCS-driven workflow
// (shared GitLab + TFE workspace wiring) plus the endpoints, lab credentials, and the
// manual push trigger the user needs. v1 covers the primary target only; values reflect
// hal defaults unless overridden at `hal terraform vcs-workflow enable` time.
func handleTFEVCSWorkflowStatus() mcpToolCallResult {
	docs := []string{
		"https://developer.hashicorp.com/terraform/enterprise/workspaces/settings/vcs",
		"https://developer.hashicorp.com/terraform/tutorials/automation/git-patterns",
	}
	recommended := []string{"hal terraform vcs-workflow status", "hal terraform vcs-workflow enable"}

	// Canonical hal defaults for the primary VCS workflow (see vcs-workflow.go
	// configureWorkspaceTargetDefaults). Centralized here so HAL Plus reads them
	// from one authoritative place instead of duplicating them in its graph. These
	// are static defaults that do not depend on runtime state, so we compute them
	// up front and always surface them (even when the runtime is unavailable) so
	// HAL Plus can render the Access section deterministically.
	const (
		tfeBase      = "https://tfe.localhost:8443"
		tfeOrg       = "hal"
		tfeProject   = "Dave"
		tfeWorkspace = "tfe-agent-demo"
		vcsBranch    = "main"
		gitlabHost   = "http://127.0.0.1:8080"
		gitlabRepo   = "root/tfe-agent-demo"
	)
	workspaceURL := fmt.Sprintf("%s/app/organizations/%s/workspaces/%s", tfeBase, tfeOrg, tfeWorkspace)
	runsURL := workspaceURL + "/runs"
	gitlabWebURL := fmt.Sprintf("%s/%s", gitlabHost, gitlabRepo)

	homeDir, _ := os.UserHomeDir()
	tokenPath := filepath.Join(homeDir, ".hal", "tfe-app-api-token")
	_, tokenErr := os.Stat(tokenPath)
	tokenReady := tokenErr == nil

	// buildVCSWorkflowData assembles the contract data map for the given runtime
	// flags. Canonical endpoints/credentials are always present; only the live
	// running/ready flags vary.
	buildVCSWorkflowData := func(tfeRunning, gitlabRunning, ready bool) map[string]interface{} {
		return map[string]interface{}{
			"target": "primary",
			"gitlab": map[string]interface{}{
				"running":        gitlabRunning,
				"url":            gitlabHost,
				"project_path":   gitlabRepo,
				"web_url":        gitlabWebURL,
				"default_branch": vcsBranch,
				"seeded_files":   []string{"main.tf", ".gitlab-ci.yml"},
			},
			"tfe": map[string]interface{}{
				"running":       tfeRunning,
				"org":           tfeOrg,
				"project":       tfeProject,
				"workspace":     tfeWorkspace,
				"workspace_url": workspaceURL,
				"runs_url":      runsURL,
				"auto_apply":    true,
				"branch":        vcsBranch,
			},
			// lab_credentials are non-secret demo values already printed by the CLI
			// and are intentionally surfaced here (lab-scoped, redaction-exempt).
			"lab_credentials": map[string]interface{}{
				"gitlab":    map[string]string{"username": "root", "password": "hal9000FTW"},
				"tfe_admin": map[string]string{"username": "haladmin", "password": "hal9000FTW"},
			},
			"ready": ready,
			"notes": "Values reflect hal defaults; flags at 'hal terraform vcs-workflow enable' can override them. Twin target is not surfaced by this tool in v1.",
		}
	}

	engine, product, err := terraformRuntimeState()
	if err != nil {
		// Runtime is unavailable, but the canonical endpoints/credentials are static
		// hal defaults, so surface them anyway (flags default to not-running) so
		// HAL Plus can still render Access deterministically.
		data := buildVCSWorkflowData(false, false, false)
		return opErrorForTool("get_tfe_vcs_workflow_status", codeTimeout, err.Error(), data, []string{"hal status", "hal terraform status"}, []opCheck{{Name: "terraform_runtime", Status: "error", Details: "unable to resolve runtime state"}}, nil, docs)
	}

	state, _ := product["state"].(string)
	reason, _ := product["reason"].(string)
	tfeRunning := strings.EqualFold(strings.TrimSpace(state), "running")
	gitlabRunning := global.IsContainerRunning(engine, "hal-gitlab")
	ready := tfeRunning && gitlabRunning && tokenReady

	checks := []opCheck{
		{Name: "terraform_runtime", Status: checkStatusFromState(state), Details: reason},
		{Name: "shared_gitlab", Status: checkStatusFromState(global.BoolState(gitlabRunning)), Details: "hal-gitlab container availability"},
		{Name: "tfe_foundation_token", Status: checkStatusFromState(global.BoolState(tokenReady)), Details: tokenPath},
	}

	data := buildVCSWorkflowData(tfeRunning, gitlabRunning, ready)

	if !tfeRunning {
		return opErrorForTool("get_tfe_vcs_workflow_status", runtimeCodeFromState(state), "tfe runtime not healthy; deploy terraform first", data, []string{"hal terraform create", "hal terraform status"}, checks, nil, docs)
	}
	if !gitlabRunning {
		return opErrorForTool("get_tfe_vcs_workflow_status", codeNotDeployed, "shared GitLab is not running; run hal terraform vcs-workflow enable to bootstrap it", data, recommended, checks, nil, docs)
	}

	next := []opNextStep{{
		Order:           1,
		Title:           "Trigger an auto-applied run",
		ExpectedOutcome: "Pushing a commit to the main branch of the GitLab repo fires a webhook; TFE queues and auto-applies a run visible at the workspace runs page.",
	}}

	return opSuccessForTool("get_tfe_vcs_workflow_status", "tfe vcs workflow status collected", data, recommended, checks, next, nil, docs)
}
