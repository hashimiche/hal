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
			"name":        "enable_vault",
			"description": "Enable Vault deploy flow in dry_run/apply mode.",
			"inputSchema": modeSchema(),
		},
		{
			"name":        "get_terraform_status",
			"description": "Return Terraform Enterprise runtime status and checks.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "enable_terraform",
			"description": "Enable Terraform Enterprise deploy flow in dry_run/apply mode.",
			"inputSchema": modeSchema(),
		},
		{
			"name":        "get_capabilities",
			"description": "Return supported HAL commands/subcommands/flags, aliases, deprecations, and examples.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "hal_policy_profile",
			"description": "Return HAL-first runtime policy profile for clients (mandatory checks, fallback behavior, and contract versions).",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"profile": map[string]interface{}{"type": "string", "enum": []string{"strict", "standard"}},
				},
			},
		},
		{
			"name":        "get_help_for_topic",
			"description": "Input topic like 'vault oidc' or 'vault jwt'; returns usage, flags, and verified command examples.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"topic": map[string]interface{}{"type": "string"},
				},
				"required": []string{"topic"},
			},
		},
		{
			"name":        "plan_next_steps",
			"description": "Given intent and optional context, return ordered validated steps and expected outcomes.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"intent":  map[string]interface{}{"type": "string"},
					"context": map[string]interface{}{"type": "string"},
				},
				"required": []string{"intent"},
			},
		},
		{
			"name":        "hal_plan_deploy",
			"description": "Alias of plan_next_steps with deploy/setup intent focus.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"intent":  map[string]interface{}{"type": "string"},
					"context": map[string]interface{}{"type": "string"},
				},
				"required": []string{"intent"},
			},
		},
		{
			"name":        "hal_plan_verify",
			"description": "Return deterministic post-action verification commands for a HAL component/workflow.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"component": map[string]interface{}{"type": "string"},
				},
				"required": []string{"component"},
			},
		},
		{
			"name":        "validate_command",
			"description": "Validate a HAL command string and return valid/invalid with correction if needed.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{"type": "string"},
				},
				"required": []string{"command"},
			},
		},
		{
			"name":        "get_component_context",
			"description": "Return endpoint/auth context for a component without exposing secrets.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"component": map[string]interface{}{"type": "string", "enum": []string{"vault", "vault_k8s", "vault_vso", "vault_csi", "vault_oidc", "vault_jwt", "vault_ldap", "vault_database", "terraform", "terraform_workspace", "consul", "nomad", "boundary", "boundary_ssh", "boundary_mariadb", "obs"}},
				},
				"required": []string{"component"},
			},
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
			"name":        "enable_oidc",
			"description": "Enable OIDC in dry_run/apply mode and return post-check and rollback commands.",
			"inputSchema": modeSchema(),
		},
		{
			"name":        "get_jwt_status",
			"description": "Return JWT status, mount path, config completeness and missing fields.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "enable_jwt",
			"description": "Enable JWT in dry_run/apply mode and return post-check and rollback commands.",
			"inputSchema": modeSchema(),
		},
		{
			"name":        "get_boundary_status",
			"description": "Return Boundary lifecycle status and critical checks.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "enable_boundary",
			"description": "Enable Boundary deploy flow in dry_run/apply mode and return post-check commands.",
			"inputSchema": modeSchema(),
		},
		{
			"name":        "get_ssh_flow_status",
			"description": "Return Boundary SSH target readiness and key checks.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "get_tfe_status",
			"description": "Return Terraform Enterprise runtime and workspace wiring status.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "get_tfe_cli_status",
			"description": "Return Terraform CLI helper readiness for local TFE workflows.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "setup_tfe_workspace",
			"description": "Run Terraform workspace bootstrap in dry_run/apply mode.",
			"inputSchema": modeSchema(),
		},
		{
			"name":        "get_k8s_integration_status",
			"description": "Return Vault Kubernetes integration readiness including VSO/CSI checks.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "enable_vault_k8s_integration",
			"description": "Enable Vault Kubernetes integration in dry_run/apply mode.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"mode":  map[string]interface{}{"type": "string", "enum": []string{"dry_run", "apply"}},
					"csi":   map[string]interface{}{"type": "boolean"},
					"force": map[string]interface{}{"type": "boolean"},
				},
			},
		},
		{
			"name":        "get_cross_product_dependencies",
			"description": "Return deterministic prerequisite graph and execution ordering across products.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "get_ldap_status",
			"description": "Return Vault LDAP demo readiness and key checks.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "enable_ldap",
			"description": "Enable Vault LDAP lab flow in dry_run/apply mode.",
			"inputSchema": modeSchema(),
		},
		{
			"name":        "get_vault_database_status",
			"description": "Return Vault database secrets demo readiness and key checks.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "enable_vault_database",
			"description": "Enable Vault database lab flow in dry_run/apply mode.",
			"inputSchema": modeSchema(),
		},
		{
			"name":        "get_boundary_mariadb_status",
			"description": "Return Boundary MariaDB target readiness and key checks.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "enable_boundary_mariadb",
			"description": "Enable Boundary MariaDB target flow in dry_run/apply mode.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"mode":       map[string]interface{}{"type": "string", "enum": []string{"dry_run", "apply"}},
					"force":      map[string]interface{}{"type": "boolean"},
					"with_vault": map[string]interface{}{"type": "boolean"},
				},
			},
		},
		{
			"name":        "get_consul_status",
			"description": "Return Consul runtime status and checks.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "enable_consul",
			"description": "Enable Consul deploy flow in dry_run/apply mode.",
			"inputSchema": modeSchema(),
		},
		{
			"name":        "get_nomad_status",
			"description": "Return Nomad runtime status and checks.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "enable_nomad",
			"description": "Enable Nomad deploy flow in dry_run/apply mode.",
			"inputSchema": modeSchema(),
		},
		{
			"name":        "get_obs_status",
			"description": "Return observability stack status and checks.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "enable_obs",
			"description": "Enable observability deploy flow in dry_run/apply mode.",
			"inputSchema": modeSchema(),
		},
	}
}

func modeSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"mode":  map[string]interface{}{"type": "string", "enum": []string{"dry_run", "apply"}},
			"force": map[string]interface{}{"type": "boolean"},
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
		return handleStatusCommandTool("get_vault_status", []string{"vault", "status"}, []string{"hal vault status", "hal vault deploy"}, []string{"https://developer.hashicorp.com/vault"}), true

	case "enable_vault":
		return handleEnableScenarioMode("enable_vault", []string{"vault", "deploy"}, []string{"hal vault status"}, args), true

	case "get_terraform_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_terraform_status", codeParseError, err.Error(), nil, []string{"hal terraform status"}, nil, nil, nil), true
		}
		return handleTerraformRuntimeStatus("get_terraform_status"), true

	case "enable_terraform":
		return handleEnableScenarioMode("enable_terraform", []string{"terraform", "deploy"}, []string{"hal terraform status"}, args), true

	case "get_capabilities":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opError(codeParseError, err.Error(), nil, []string{"hal --help"}, nil), true
		}
		specs := allActionSpecs()
		commands := make([]string, 0, len(specs))
		for _, s := range specs {
			if len(s.Command) > 0 {
				commands = append(commands, "hal "+strings.Join(s.Command, " "))
			}
		}
		sort.Strings(commands)
		cap := map[string]interface{}{
			"actions":     specs,
			"commands":    commands,
			"tool_names":  collectToolNames(),
			"error_codes": contractErrorCodes(),
		}
		if idx, err := getSkillIndex(); err == nil && idx != nil {
			cap["skills"] = map[string]interface{}{
				"skills_count":        len(idx.Skills),
				"commands_count":      len(idx.Commands),
				"deprecated_commands": idx.DeprecatedCommands,
			}
		}
		return opSuccess("capabilities collected", cap, []string{"hal --help"}, nil), true

	case "hal_policy_profile":
		if err := ensureOnlyKeys(args, map[string]bool{"profile": true}); err != nil {
			return opErrorForTool("hal_policy_profile", codeParseError, err.Error(), nil, []string{"hal mcp policy --json"}, nil, nil, nil), true
		}
		profile := "strict"
		if raw, ok := args["profile"]; ok {
			parsed, ok := raw.(string)
			if !ok {
				return opErrorForTool("hal_policy_profile", codeParseError, "profile must be a string", nil, []string{"hal mcp policy --json"}, nil, nil, nil), true
			}
			if strings.TrimSpace(parsed) != "" {
				profile = strings.TrimSpace(parsed)
			}
		}
		policy, err := buildPolicyProfile(profile)
		if err != nil {
			return opErrorForTool("hal_policy_profile", codeParseError, err.Error(), nil, []string{"hal mcp policy --json"}, nil, nil, nil), true
		}
		checks := []opCheck{{Name: "policy", Status: "ok", Details: "runtime policy profile resolved"}}
		return opSuccessForTool("hal_policy_profile", "policy profile resolved", policy, []string{"hal mcp policy --json", "hal mcp status"}, checks, nil, nil, nil), true

	case "get_help_for_topic":
		if err := ensureOnlyKeys(args, map[string]bool{"topic": true}); err != nil {
			return opError(codeParseError, err.Error(), nil, []string{"hal --help"}, nil), true
		}
		topic, ok := args["topic"].(string)
		if !ok || strings.TrimSpace(topic) == "" {
			return opError(codeParseError, "topic is required", nil, []string{"hal --help"}, nil), true
		}
		parts := strings.Fields(strings.TrimSpace(topic))
		halArgs := append(parts, "--help")
		execRes := runHAL(halArgs...)
		if execRes.ExitCode != 0 {
			return opError(classifyContractError(execRes.Output), "unable to retrieve help for topic", map[string]interface{}{"execution": execRes}, []string{"hal --help"}, nil), true
		}
		help := parseHelpOutput(execRes.Output)
		help["topic"] = strings.Join(parts, " ")
		helpcmd := "hal " + strings.Join(halArgs, " ")
		help["recommended_commands"] = sortedUnique(append([]string{helpcmd}, extractCommandsFromHelp(execRes.Output)...))
		return opSuccess("topic help parsed", help, help["recommended_commands"].([]string), nil), true

	case "plan_next_steps", "hal_plan_deploy":
		if err := ensureOnlyKeys(args, map[string]bool{"intent": true, "context": true}); err != nil {
			return opError(codeParseError, err.Error(), nil, []string{"get_capabilities"}, nil), true
		}
		intent, ok := args["intent"].(string)
		if !ok || strings.TrimSpace(intent) == "" {
			return opError(codeParseError, "intent is required", nil, []string{"get_capabilities"}, nil), true
		}
		plan, ok := buildPlan(strings.TrimSpace(intent))
		if !ok {
			if featurePlan, featureOK := buildFeaturePlan(strings.TrimSpace(intent)); featureOK {
				recommended := extractPlanCommands(featurePlan)
				return opSuccess("plan generated", featurePlan, recommended, nil), true
			}
			return opError(codeUnsupportedOp, "unable to generate plan for intent", map[string]interface{}{"intent": intent}, []string{"get_capabilities", "get_help_for_topic"}, nil), true
		}
		recommended := extractPlanCommands(plan)
		return opSuccess("plan generated", plan, recommended, nil), true

	case "hal_plan_verify":
		if err := ensureOnlyKeys(args, map[string]bool{"component": true}); err != nil {
			return opErrorForTool("hal_plan_verify", codeParseError, err.Error(), nil, []string{"get_runtime_status"}, nil, nil, nil), true
		}
		component, ok := args["component"].(string)
		if !ok || strings.TrimSpace(component) == "" {
			return opErrorForTool("hal_plan_verify", codeParseError, "component is required", nil, []string{"get_capabilities"}, nil, nil, nil), true
		}
		comp := strings.ToLower(strings.TrimSpace(component))
		commands := []string{}
		docs := []string{}
		checks := []opCheck{{Name: "verification_plan", Status: "ok", Details: "component verification sequence generated"}}

		switch comp {
		case "vault":
			commands = []string{"hal vault status", "hal status"}
			docs = []string{"https://developer.hashicorp.com/vault"}
		case "vault_k8s", "vault_vso", "vault_csi", "k8s", "vso", "csi":
			commands = []string{"hal vault k8s", "kubectl get pods -A", "kubectl get svc -A"}
			docs = []string{"https://developer.hashicorp.com/vault/docs/platform/k8s/vso", "https://developer.hashicorp.com/vault/docs/platform/k8s/csi"}
		case "consul":
			commands = []string{"hal consul status", "hal status"}
			docs = []string{"https://developer.hashicorp.com/consul"}
		case "nomad":
			commands = []string{"hal nomad status", "hal status"}
			docs = []string{"https://developer.hashicorp.com/nomad"}
		case "boundary", "boundary_ssh", "boundary_mariadb":
			commands = []string{"hal boundary status", "hal boundary ssh"}
			docs = []string{"https://developer.hashicorp.com/boundary"}
		case "terraform", "terraform_workspace", "tfe":
			commands = []string{"hal terraform status", "hal terraform workspace"}
			docs = []string{"https://developer.hashicorp.com/terraform/enterprise"}
		case "terraform_cli", "tfe_cli":
			commands = []string{"hal terraform cli", "hal tf cli -c", "hal terraform status"}
			docs = []string{"https://developer.hashicorp.com/terraform/enterprise"}
		case "obs", "observability":
			commands = []string{"hal obs status", "hal status"}
			docs = []string{"https://grafana.com/docs/", "https://prometheus.io/docs/", "https://grafana.com/oss/loki/"}
		default:
			return opErrorForTool("hal_plan_verify", codeUnsupportedOp, "unsupported component", map[string]interface{}{"component": comp}, []string{"get_capabilities", "get_component_context"}, []opCheck{{Name: "verification_plan", Status: "error", Details: "unsupported component"}}, nil, nil), true
		}

		data := map[string]interface{}{
			"component":             comp,
			"verification_commands": commands,
			"notes":                 []string{"Run commands in order and confirm expected healthy states before next operations."},
		}
		return opSuccessForTool("hal_plan_verify", "verification plan generated", data, commands, checks, nil, nil, docs), true

	case "validate_command":
		if err := ensureOnlyKeys(args, map[string]bool{"command": true}); err != nil {
			return opError(codeParseError, err.Error(), nil, []string{"hal --help"}, nil), true
		}
		cmdText, ok := args["command"].(string)
		if !ok || strings.TrimSpace(cmdText) == "" {
			return opError(codeParseError, "command is required", nil, []string{"hal --help"}, nil), true
		}
		res := validateCommand(cmdText)
		valid, _ := res["valid"].(bool)
		if valid {
			normalized, _ := res["normalized_command"].(string)
			return opSuccess("command is valid", res, []string{normalized}, nil), true
		}
		recommended := []string{}
		if suggestions, ok := res["suggestions"].([]string); ok {
			recommended = append(recommended, suggestions...)
		}
		code := codeCommandNotFound
		if errs, ok := res["errors"].([]string); ok && len(errs) > 0 {
			code = classifyContractError(errs[0])
		}
		return opError(code, "command is invalid", res, sortedUnique(recommended), nil), true

	case "get_component_context":
		if err := ensureOnlyKeys(args, map[string]bool{"component": true}); err != nil {
			return opError(codeParseError, err.Error(), nil, []string{"get_runtime_status"}, nil), true
		}
		component, ok := args["component"].(string)
		if !ok || strings.TrimSpace(component) == "" {
			return opError(codeParseError, "component is required", nil, []string{"get_runtime_status"}, nil), true
		}
		ctx, cmds, err := componentContext(strings.ToLower(strings.TrimSpace(component)))
		if err != nil {
			return opError(codeUnsupportedOp, err.Error(), nil, []string{"get_capabilities"}, nil), true
		}
		return opSuccess("component context resolved", ctx, cmds, []string{"https://developer.hashicorp.com"}), true

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

	case "enable_oidc":
		return handleEnableAuthMode("oidc", args), true

	case "get_jwt_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opError(codeParseError, err.Error(), nil, []string{"hal vault jwt --help"}, nil), true
		}
		status, rec, err := buildOIDCOrJWTStatus("jwt")
		if err != nil {
			return opError(classifyContractError(err.Error()), err.Error(), nil, rec, []string{"https://developer.hashicorp.com/vault/docs/auth/jwt"}), true
		}
		return opSuccess("jwt status collected", status, rec, []string{"https://developer.hashicorp.com/vault/docs/auth/jwt"}), true

	case "enable_jwt":
		return handleEnableAuthMode("jwt", args), true

	case "get_boundary_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_boundary_status", codeParseError, err.Error(), nil, []string{"hal boundary status"}, nil, nil, nil), true
		}
		execRes := runHAL("boundary", "status")
		checks := []opCheck{{Name: "boundary_status_command", Status: statusFromExecution(execRes), Details: strings.TrimSpace(execRes.Output)}}
		if execRes.ExitCode != 0 {
			return opErrorForTool("get_boundary_status", classifyContractError(execRes.Output), "boundary status check failed; run recovery commands", map[string]interface{}{"execution": execRes}, []string{"hal boundary deploy", "hal boundary status"}, checks, nil, []string{"https://developer.hashicorp.com/boundary"}), true
		}
		return opSuccessForTool("get_boundary_status", "boundary status collected", map[string]interface{}{"execution": execRes}, []string{"hal boundary status", "hal boundary ssh"}, checks, nil, nil, []string{"https://developer.hashicorp.com/boundary"}), true

	case "enable_boundary":
		return handleEnableScenarioMode("enable_boundary", []string{"boundary", "deploy"}, []string{"hal boundary status"}, args), true

	case "get_ssh_flow_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_ssh_flow_status", codeParseError, err.Error(), nil, []string{"hal boundary ssh"}, nil, nil, nil), true
		}
		execRes := runHAL("boundary", "ssh")
		checks := []opCheck{{Name: "boundary_ssh_status", Status: statusFromExecution(execRes), Details: strings.TrimSpace(execRes.Output)}}
		if execRes.ExitCode != 0 {
			return opErrorForTool("get_ssh_flow_status", classifyContractError(execRes.Output), "boundary ssh status failed; run recovery commands", map[string]interface{}{"execution": execRes}, []string{"hal boundary ssh --enable", "hal boundary status"}, checks, nil, nil), true
		}
		return opSuccessForTool("get_ssh_flow_status", "boundary ssh status collected", map[string]interface{}{"execution": execRes}, []string{"hal boundary ssh", "hal boundary ssh --enable"}, checks, nil, nil, nil), true

	case "get_tfe_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_tfe_status", codeParseError, err.Error(), nil, []string{"hal terraform status"}, nil, nil, nil), true
		}
		return handleTFEStatus(), true

	case "get_tfe_cli_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_tfe_cli_status", codeParseError, err.Error(), nil, []string{"hal terraform cli"}, nil, nil, nil), true
		}
		return handleTFECLIStatus(), true

	case "setup_tfe_workspace":
		return handleEnableScenarioMode("setup_tfe_workspace", []string{"terraform", "workspace", "--enable"}, []string{"hal terraform workspace", "hal terraform status"}, args), true

	case "get_k8s_integration_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_k8s_integration_status", codeParseError, err.Error(), nil, []string{"hal vault k8s"}, nil, nil, nil), true
		}
		execRes := runHAL("vault", "k8s")
		checks := []opCheck{{Name: "vault_k8s_status", Status: statusFromExecution(execRes), Details: "vault k8s/vso/csi check"}}
		if execRes.ExitCode != 0 {
			return opErrorForTool("get_k8s_integration_status", classifyContractError(execRes.Output), "k8s integration check failed; inspect vault and kind prerequisites", map[string]interface{}{"execution": execRes}, []string{"hal vault status", "hal vault k8s --enable"}, checks, nil, nil), true
		}
		return opSuccessForTool("get_k8s_integration_status", "vault k8s integration status collected", map[string]interface{}{"execution": execRes}, []string{"hal vault k8s", "hal vault k8s --enable", "hal vault k8s --enable --csi"}, checks, nil, nil, nil), true

	case "enable_vault_k8s_integration":
		return handleEnableVaultK8sIntegration(args), true

	case "get_cross_product_dependencies":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_cross_product_dependencies", codeParseError, err.Error(), nil, []string{"hal status"}, nil, nil, nil), true
		}
		deps := map[string]interface{}{
			"graph": []map[string]interface{}{
				{"component": "vault", "depends_on": []string{}},
				{"component": "vault_k8s_vso_csi", "depends_on": []string{"vault"}},
				{"component": "consul", "depends_on": []string{}},
				{"component": "nomad", "depends_on": []string{"consul"}},
				{"component": "boundary", "depends_on": []string{"vault"}},
				{"component": "boundary_ssh", "depends_on": []string{"boundary"}},
				{"component": "terraform_workspace", "depends_on": []string{"terraform", "gitlab"}},
				{"component": "observability", "depends_on": []string{}},
			},
			"recommended_order": []string{"vault", "consul", "nomad", "boundary", "boundary_ssh", "terraform", "terraform_workspace", "observability", "vault_k8s_vso_csi"},
		}
		checks := []opCheck{{Name: "dependency_graph", Status: "ok", Details: "static deterministic graph"}}
		return opSuccessForTool("get_cross_product_dependencies", "cross-product dependencies resolved", deps, []string{"hal status", "hal vault status", "hal consul status", "hal nomad status", "hal boundary status", "hal terraform status", "hal obs status"}, checks, nil, nil, nil), true

	case "get_ldap_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_ldap_status", codeParseError, err.Error(), nil, []string{"hal vault ldap"}, nil, nil, nil), true
		}
		return handleStatusCommandTool("get_ldap_status", []string{"vault", "ldap"}, []string{"hal vault ldap", "hal vault ldap --enable"}, []string{"https://developer.hashicorp.com/vault/docs/auth/ldap"}), true

	case "enable_ldap":
		return handleEnableScenarioMode("enable_ldap", []string{"vault", "ldap", "--enable"}, []string{"hal vault ldap", "hal vault status"}, args), true

	case "get_vault_database_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_vault_database_status", codeParseError, err.Error(), nil, []string{"hal vault database"}, nil, nil, nil), true
		}
		return handleStatusCommandTool("get_vault_database_status", []string{"vault", "database"}, []string{"hal vault database", "hal vault database --enable --backend mariadb"}, []string{"https://developer.hashicorp.com/vault/docs/secrets/databases"}), true

	case "enable_vault_database":
		return handleEnableScenarioMode("enable_vault_database", []string{"vault", "database", "--enable"}, []string{"hal vault database", "hal vault status"}, args), true

	case "get_boundary_mariadb_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_boundary_mariadb_status", codeParseError, err.Error(), nil, []string{"hal boundary mariadb"}, nil, nil, nil), true
		}
		return handleStatusCommandTool("get_boundary_mariadb_status", []string{"boundary", "mariadb"}, []string{"hal boundary mariadb", "hal boundary mariadb --enable"}, []string{"https://developer.hashicorp.com/boundary"}), true

	case "enable_boundary_mariadb":
		return handleEnableBoundaryMariaDB(args), true

	case "get_consul_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_consul_status", codeParseError, err.Error(), nil, []string{"hal consul status"}, nil, nil, nil), true
		}
		return handleStatusCommandTool("get_consul_status", []string{"consul", "status"}, []string{"hal consul status", "hal consul deploy"}, []string{"https://developer.hashicorp.com/consul"}), true

	case "enable_consul":
		return handleEnableScenarioMode("enable_consul", []string{"consul", "deploy"}, []string{"hal consul status"}, args), true

	case "get_nomad_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_nomad_status", codeParseError, err.Error(), nil, []string{"hal nomad status"}, nil, nil, nil), true
		}
		return handleStatusCommandTool("get_nomad_status", []string{"nomad", "status"}, []string{"hal nomad status", "hal nomad deploy"}, []string{"https://developer.hashicorp.com/nomad"}), true

	case "enable_nomad":
		return handleEnableScenarioMode("enable_nomad", []string{"nomad", "deploy"}, []string{"hal nomad status"}, args), true

	case "get_obs_status":
		if err := ensureOnlyKeys(args, map[string]bool{}); err != nil {
			return opErrorForTool("get_obs_status", codeParseError, err.Error(), nil, []string{"hal obs status"}, nil, nil, nil), true
		}
		return handleStatusCommandTool("get_obs_status", []string{"obs", "status"}, []string{"hal obs status", "hal obs deploy"}, []string{"https://grafana.com/docs/", "https://prometheus.io/docs/", "https://grafana.com/oss/loki/"}), true

	case "enable_obs":
		return handleEnableScenarioMode("enable_obs", []string{"obs", "deploy"}, []string{"hal obs status"}, args), true

	default:
		return mcpToolCallResult{}, false
	}
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

func buildPolicyProfile(profile string) (map[string]interface{}, error) {
	selected := strings.ToLower(strings.TrimSpace(profile))
	if selected == "" {
		selected = "strict"
	}
	if selected != "strict" && selected != "standard" {
		return nil, fmt.Errorf("unsupported profile: %s", selected)
	}

	requiredPrefetch := []string{"hal_status_baseline", "get_capabilities", "hal_policy_profile"}
	if selected == "strict" {
		requiredPrefetch = append(requiredPrefetch, "validate_command")
	}

	return map[string]interface{}{
		"policy_version":   mcpPolicyVersion,
		"contract_version": mcpContractVersion,
		"profile":          selected,
		"answer_policy": map[string]interface{}{
			"mode":                           "hal_first",
			"disallow_unverified_claims":     true,
			"disallow_non_hal_primary_paths": true,
			"include_verification_commands":  true,
			"include_official_docs":          true,
		},
		"tool_policy": map[string]interface{}{
			"required_prefetch_tools": requiredPrefetch,
			"on_uncertain_then_call":  []string{"validate_command", "get_help_for_topic"},
			"fallback": map[string]interface{}{
				"mode":         "fail_closed",
				"allow_answer": false,
				"message":      "HAL MCP policy unavailable; run hal mcp status and retry.",
			},
		},
		"recommended_bootstrap": []string{"hal mcp status", "hal status", "hal --help"},
	}, nil
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

func parseHelpOutput(output string) map[string]interface{} {
	lines := strings.Split(output, "\n")
	usage := ""
	flags := []map[string]string{}
	inFlags := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Usage:") {
			usage = strings.TrimSpace(strings.TrimPrefix(trimmed, "Usage:"))
		}
		if trimmed == "Flags:" || trimmed == "Global Flags:" {
			inFlags = true
			continue
		}
		if inFlags {
			if trimmed == "" {
				inFlags = false
				continue
			}
			if strings.HasPrefix(trimmed, "Use \"") {
				inFlags = false
				continue
			}
			if strings.HasPrefix(trimmed, "-") {
				parts := strings.SplitN(trimmed, "   ", 2)
				name := strings.TrimSpace(parts[0])
				desc := ""
				if len(parts) > 1 {
					desc = strings.TrimSpace(parts[1])
				}
				flags = append(flags, map[string]string{"name": name, "description": desc})
			}
		}
	}
	sort.Slice(flags, func(i, j int) bool { return flags[i]["name"] < flags[j]["name"] })
	return map[string]interface{}{"usage": usage, "flags": flags}
}

func extractCommandsFromHelp(output string) []string {
	lines := strings.Split(output, "\n")
	commands := []string{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Use \"") && strings.Contains(trimmed, "--help\"") {
			trimmed = strings.TrimPrefix(trimmed, "Use \"")
			trimmed = strings.TrimSuffix(trimmed, "\" for more information about a command.")
			commands = append(commands, trimmed)
		}
	}
	return sortedUnique(commands)
}

func extractPlanCommands(plan map[string]interface{}) []string {
	out := []string{}
	if pre, ok := plan["prechecks"].([]string); ok {
		out = append(out, pre...)
	}
	if steps, ok := plan["steps"].([]map[string]string); ok {
		for _, step := range steps {
			if cmd, ok := step["command"]; ok {
				out = append(out, cmd)
			}
		}
	}
	if post, ok := plan["postchecks"].([]string); ok {
		out = append(out, post...)
	}
	return sortedUnique(out)
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
				"vault_container_running": checkContainer("podman", "hal-vault") || checkContainer("docker", "hal-vault"),
			},
		}, []string{"hal vault status", "hal vault deploy", "hal vault audit"}, nil
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
			"flags":     []string{"--enable", "--disable", "--force", "--csi", "--jwt-auth"},
			"endpoint":  "http://web.localhost:8088",
		}, []string{"hal vault k8s", "hal vault k8s --enable", "hal vault k8s --enable --csi", "hal vault k8s --disable"}, nil
	case "vault_vso":
		return map[string]interface{}{
			"component":   "vault_vso",
			"implemented": "hal vault k8s workflow",
			"runtime":     "vault-secrets-operator in namespace vso",
			"health_hint": "helm list -n vso",
		}, []string{"hal vault k8s", "hal vault k8s --enable"}, nil
	case "vault_csi":
		return map[string]interface{}{
			"component":   "vault_csi",
			"implemented": "hal vault k8s --csi",
			"runtime":     "VSO CSI projection mode",
			"health_hint": "kubectl get pods -n vso",
		}, []string{"hal vault k8s --enable --csi", "hal vault k8s"}, nil
	case "vault_ldap":
		return map[string]interface{}{
			"component":  "vault_ldap",
			"status_cmd": "hal vault ldap",
			"notes":      "Use command without flags for smart status and next-step guidance",
		}, []string{"hal vault ldap", "hal vault ldap --enable", "hal vault ldap --disable"}, nil
	case "vault_database":
		return map[string]interface{}{
			"component":  "vault_database",
			"status_cmd": "hal vault database",
			"notes":      "Use command without flags for smart status and next-step guidance",
		}, []string{"hal vault database", "hal vault database --enable", "hal vault database --disable"}, nil
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
		}, []string{"hal terraform status", "hal terraform deploy", "hal terraform workspace", "hal terraform cli"}, nil
	case "terraform_workspace":
		return map[string]interface{}{
			"component":  "terraform_workspace",
			"depends_on": []string{"hal-tfe", "hal-gitlab"},
			"workflow":   "prepare gitlab repo + wire TFE workspace VCS",
			"trigger":    "push commit to main branch",
		}, []string{"hal terraform workspace", "hal terraform workspace --enable", "hal terraform status"}, nil
	case "terraform_cli":
		return map[string]interface{}{
			"component":        "terraform_cli",
			"helper_container": "hal-tfe-cli",
			"default_org":      "hal",
			"auth_files":       []string{"/root/.tfx.hcl", "/root/.terraform.d/credentials.tfrc.json"},
			"seeded_projects":  []string{"Dave", "Frank"},
			"workflow":         "build helper image, then open console against local TFE",
		}, []string{"hal terraform cli", "hal tf cli -e", "hal tf cli -c", "hal terraform status"}, nil
	case "consul":
		return map[string]interface{}{"component": component, "endpoint": "http://consul.localhost:8500"}, []string{"hal consul status"}, nil
	case "nomad":
		return map[string]interface{}{"component": component, "endpoint": "multipass://hal-nomad"}, []string{"hal nomad status"}, nil
	case "boundary":
		return map[string]interface{}{"component": component, "endpoint": "http://boundary.localhost:9200"}, []string{"hal boundary status", "hal boundary deploy", "hal boundary ssh"}, nil
	case "boundary_ssh":
		return map[string]interface{}{
			"component": "boundary_ssh",
			"platform":  "multipass",
			"vm":        "hal-boundary-ssh",
			"flags":     []string{"--enable", "--disable", "--force"},
		}, []string{"hal boundary ssh", "hal boundary ssh --enable", "hal boundary ssh --disable"}, nil
	case "boundary_mariadb":
		return map[string]interface{}{
			"component":  "boundary_mariadb",
			"status_cmd": "hal boundary mariadb",
			"notes":      "Use command without flags for smart status and next-step guidance",
		}, []string{"hal boundary mariadb", "hal boundary mariadb --enable", "hal boundary mariadb --disable"}, nil
	case "obs":
		return map[string]interface{}{"component": component, "endpoints": []string{"http://grafana.localhost:3000", "http://prometheus.localhost:9090", "http://loki.localhost:3100/ready"}}, []string{"hal obs status"}, nil
	default:
		return nil, nil, fmt.Errorf("unsupported component: %s", component)
	}
}

func buildFeaturePlan(intent string) (map[string]interface{}, bool) {
	lower := strings.ToLower(strings.TrimSpace(intent))
	switch {
	case strings.Contains(lower, "boundary") && strings.Contains(lower, "ssh"):
		return map[string]interface{}{
			"intent":       intent,
			"action":       "boundary_ssh_workflow",
			"prechecks":    []string{"hal boundary status", "hal boundary ssh"},
			"steps":        []map[string]string{{"command": "hal boundary ssh --enable", "reason": "Deploy SSH target VM and wire Boundary resources"}},
			"postchecks":   []string{"hal boundary ssh", "hal boundary status"},
			"rollback":     []string{"hal boundary ssh --disable"},
			"expectations": []string{"Multipass VM hal-boundary-ssh running", "Boundary target is reachable"},
		}, true
	case strings.Contains(lower, "terraform") && (strings.Contains(lower, "workspace") || strings.Contains(lower, "tfe") || strings.Contains(lower, "vcs")):
		return map[string]interface{}{
			"intent":       intent,
			"action":       "terraform_workspace_setup",
			"prechecks":    []string{"hal terraform status", "hal terraform workspace"},
			"steps":        []map[string]string{{"command": "hal terraform workspace --enable", "reason": "Prepare GitLab repo and wire TFE workspace"}},
			"postchecks":   []string{"hal terraform workspace", "hal terraform status"},
			"next_trigger": []string{"Push a commit to main to validate end-to-end VCS run"},
			"rollback":     []string{"hal terraform destroy"},
		}, true
	case strings.Contains(lower, "terraform") && (strings.Contains(lower, "cli") || strings.Contains(lower, "tfx") || strings.Contains(lower, "helper")):
		return map[string]interface{}{
			"intent":    intent,
			"action":    "terraform_cli_helper",
			"prechecks": []string{"hal terraform status", "hal terraform cli"},
			"steps": []map[string]string{
				{"command": "hal tf cli -e", "reason": "Build or refresh the Terraform/TFX helper image"},
				{"command": "hal tf cli -c", "reason": "Enter the helper container with trust and auth preloaded"},
			},
			"postchecks": []string{"hal terraform cli", "hal terraform status"},
			"notes":      []string{"Use the helper instead of changing the host trust store."},
		}, true
	case strings.Contains(lower, "vault") && (strings.Contains(lower, "k8s") || strings.Contains(lower, "vso") || strings.Contains(lower, "csi")):
		stepCmd := "hal vault k8s --enable"
		reason := "Deploy KinD + VSO workflow"
		if strings.Contains(lower, "csi") {
			stepCmd = "hal vault k8s --enable --csi"
			reason = "Deploy KinD + VSO CSI workflow"
		}
		return map[string]interface{}{
			"intent":     intent,
			"action":     "vault_k8s_workflow",
			"prechecks":  []string{"hal vault status", "hal vault k8s"},
			"steps":      []map[string]string{{"command": stepCmd, "reason": reason}},
			"postchecks": []string{"hal vault k8s", "hal vault status"},
			"rollback":   []string{"hal vault k8s --disable"},
		}, true
	case strings.Contains(lower, "vault") && strings.Contains(lower, "ldap"):
		return map[string]interface{}{
			"intent":     intent,
			"action":     "vault_ldap_workflow",
			"prechecks":  []string{"hal vault status", "hal vault ldap"},
			"steps":      []map[string]string{{"command": "hal vault ldap --enable", "reason": "Enable LDAP auth integration in local lab"}},
			"postchecks": []string{"hal vault ldap", "hal vault status"},
			"rollback":   []string{"hal vault ldap --disable"},
		}, true
	case strings.Contains(lower, "vault") && (strings.Contains(lower, "database") || strings.Contains(lower, " db") || strings.Contains(lower, "db ") || strings.Contains(lower, "mariadb")):
		return map[string]interface{}{
			"intent":     intent,
			"action":     "vault_database_workflow",
			"prechecks":  []string{"hal vault status", "hal vault database"},
			"steps":      []map[string]string{{"command": "hal vault database --enable --backend mariadb", "reason": "Enable Vault database secrets lab (default backend: MariaDB)"}},
			"postchecks": []string{"hal vault database", "hal vault status"},
			"rollback":   []string{"hal vault database --disable"},
		}, true
	case strings.Contains(lower, "boundary") && strings.Contains(lower, "mariadb"):
		return map[string]interface{}{
			"intent":     intent,
			"action":     "boundary_mariadb_workflow",
			"prechecks":  []string{"hal boundary status", "hal boundary mariadb"},
			"steps":      []map[string]string{{"command": "hal boundary mariadb --enable", "reason": "Enable Boundary database-backed target workflow"}},
			"postchecks": []string{"hal boundary mariadb", "hal boundary status"},
			"rollback":   []string{"hal boundary mariadb --disable"},
		}, true
	default:
		return nil, false
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
	if !checkContainer(engine, "hal-vault") {
		return map[string]interface{}{"enabled": false, "mount_path": mode + "/", "config_complete": false, "missing_fields": []string{"vault_not_running"}}, []string{"hal vault deploy", "hal vault status"}, fmt.Errorf("vault is not deployed")
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
	if mode == "oidc" && !checkContainer(engine, "hal-keycloak") {
		missing = append(missing, "keycloak_provider")
	}
	if mode == "jwt" && !checkContainer(engine, "hal-gitlab") {
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
		recommended = append(recommended, "hal vault oidc --enable")
	} else {
		recommended = append(recommended, "hal vault jwt --enable")
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

func handleEnableAuthMode(mode string, args map[string]interface{}) mcpToolCallResult {
	if err := ensureOnlyKeys(args, map[string]bool{"mode": true, "force": true}); err != nil {
		return opError(codeParseError, err.Error(), nil, []string{"hal vault " + mode + " --enable"}, nil)
	}
	runMode := "dry_run"
	if raw, ok := args["mode"]; ok {
		parsed, ok := raw.(string)
		if !ok {
			return opError(codeParseError, "mode must be string", nil, []string{"hal vault " + mode + " --enable"}, nil)
		}
		runMode = strings.TrimSpace(strings.ToLower(parsed))
	}
	if runMode != "dry_run" && runMode != "apply" {
		return opError(codeParseError, "mode must be dry_run or apply", nil, []string{"hal vault " + mode + " --enable"}, nil)
	}
	force := false
	if raw, ok := args["force"]; ok {
		parsed, ok := raw.(bool)
		if !ok {
			return opError(codeParseError, "force must be boolean", nil, []string{"hal vault " + mode + " --enable"}, nil)
		}
		force = parsed
	}

	baseCmd := []string{"vault", mode, "--enable"}
	if force {
		baseCmd = append(baseCmd, "--force")
	}
	base := "hal " + strings.Join(baseCmd, " ")
	recommended := []string{base, "hal vault status", "hal vault audit"}

	if runMode == "dry_run" {
		dryRunCommand := "hal --dry-run " + strings.Join(baseCmd, " ")
		data := map[string]interface{}{
			"mode":    runMode,
			"applied": false,
			"plan": map[string]interface{}{
				"command":       base,
				"dry_run":       dryRunCommand,
				"post_checks":   []string{"hal vault status", "hal vault " + mode + " --help"},
				"rollback_hint": "vault auth disable " + mode,
			},
		}
		return opSuccess(mode+" enable plan generated (dry_run)", data, append(recommended, dryRunCommand), []string{"https://developer.hashicorp.com/vault/docs/auth/jwt"})
	}

	execRes := runHAL(baseCmd...)
	data := map[string]interface{}{
		"mode":      runMode,
		"applied":   execRes.ExitCode == 0,
		"execution": execRes,
		"post_checks": []string{
			"hal vault status",
			"hal vault " + mode + " --help",
		},
		"rollback_hint": "vault auth disable " + mode,
	}

	if execRes.ExitCode != 0 {
		return opError(classifyContractError(execRes.Output), "failed to enable "+mode, data, recommended, []string{"https://developer.hashicorp.com/vault/docs/auth/jwt"})
	}

	return opSuccess(mode+" enabled", data, recommended, []string{"https://developer.hashicorp.com/vault/docs/auth/jwt"})
}

func handleEnableScenarioMode(toolName string, baseCmd []string, postChecks []string, args map[string]interface{}) mcpToolCallResult {
	if err := ensureOnlyKeys(args, map[string]bool{"mode": true, "force": true}); err != nil {
		return opErrorForTool(toolName, codeParseError, err.Error(), nil, []string{"hal " + strings.Join(baseCmd, " ")}, nil, nil, nil)
	}
	runMode := "dry_run"
	if raw, ok := args["mode"]; ok {
		parsed, ok := raw.(string)
		if !ok {
			return opErrorForTool(toolName, codeParseError, "mode must be string", nil, []string{"hal " + strings.Join(baseCmd, " ")}, nil, nil, nil)
		}
		runMode = strings.TrimSpace(strings.ToLower(parsed))
	}
	if runMode != "dry_run" && runMode != "apply" {
		return opErrorForTool(toolName, codeParseError, "mode must be dry_run or apply", nil, []string{"hal " + strings.Join(baseCmd, " ")}, nil, nil, nil)
	}
	force := false
	if raw, ok := args["force"]; ok {
		parsed, ok := raw.(bool)
		if !ok {
			return opErrorForTool(toolName, codeParseError, "force must be boolean", nil, []string{"hal " + strings.Join(baseCmd, " ")}, nil, nil, nil)
		}
		force = parsed
	}
	finalCmd := append([]string{}, baseCmd...)
	if force {
		finalCmd = append(finalCmd, "--force")
	}
	full := "hal " + strings.Join(finalCmd, " ")
	recommended := append([]string{full}, postChecks...)
	checks := []opCheck{{Name: "mode", Status: "ok", Details: runMode}}
	if runMode == "dry_run" {
		next := []opNextStep{{Order: 1, Title: "Apply scenario", ExpectedOutcome: "Scenario resources are configured", Commands: []string{full}}}
		data := map[string]interface{}{"mode": runMode, "applied": false, "command": full, "post_checks": postChecks}
		return opSuccessForTool(toolName, "dry_run plan generated", data, recommended, checks, next, nil, nil)
	}
	execRes := runHAL(finalCmd...)
	checks = append(checks, opCheck{Name: "execution", Status: statusFromExecution(execRes), Details: "command execution result"})
	data := map[string]interface{}{"mode": runMode, "applied": execRes.ExitCode == 0, "execution": execRes, "post_checks": postChecks}
	if execRes.ExitCode != 0 {
		return opErrorForTool(toolName, classifyContractError(execRes.Output), "scenario apply failed; run recovery commands", data, recommended, checks, nil, nil)
	}
	return opSuccessForTool(toolName, "scenario applied", data, recommended, checks, nil, nil, nil)
}

func handleEnableVaultK8sIntegration(args map[string]interface{}) mcpToolCallResult {
	if err := ensureOnlyKeys(args, map[string]bool{"mode": true, "force": true, "csi": true}); err != nil {
		return opErrorForTool("enable_vault_k8s_integration", codeParseError, err.Error(), nil, []string{"hal vault k8s --enable"}, nil, nil, nil)
	}
	baseCmd := []string{"vault", "k8s", "--enable"}
	if raw, ok := args["csi"]; ok {
		parsed, ok := raw.(bool)
		if !ok {
			return opErrorForTool("enable_vault_k8s_integration", codeParseError, "csi must be boolean", nil, []string{"hal vault k8s --enable"}, nil, nil, nil)
		}
		if parsed {
			baseCmd = append(baseCmd, "--csi")
		}
	}
	return handleEnableScenarioMode("enable_vault_k8s_integration", baseCmd, []string{"hal vault k8s", "hal vault status"}, args)
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
		return opErrorForTool(toolName, runtimeCodeFromState(state), "terraform enterprise runtime not healthy; deploy terraform first", data, []string{"hal terraform deploy", "hal terraform status"}, checks, nil, []string{"https://developer.hashicorp.com/terraform/enterprise"})
	}
	return opSuccessForTool(toolName, "terraform runtime status collected", data, []string{"hal terraform status", "hal terraform deploy"}, checks, nil, nil, []string{"https://developer.hashicorp.com/terraform/enterprise"})
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
		"workspace_hint":  "Use get_help_for_topic(terraform workspace) and get_component_context(terraform_workspace) for workspace-specific guidance.",
	}
	if state != "running" {
		return opErrorForTool("get_tfe_status", runtimeCodeFromState(state), "tfe runtime not healthy; deploy terraform first", data, []string{"hal terraform deploy", "hal terraform status"}, checks, nil, []string{"https://developer.hashicorp.com/terraform/enterprise"})
	}
	return opSuccessForTool("get_tfe_status", "tfe status collected", data, []string{"hal terraform status", "hal terraform workspace --enable"}, checks, nil, nil, []string{"https://developer.hashicorp.com/terraform/enterprise"})
}

func handleTFECLIStatus() mcpToolCallResult {
	engine, product, err := terraformRuntimeState()
	if err != nil {
		return opErrorForTool("get_tfe_cli_status", codeTimeout, err.Error(), nil, []string{"hal status", "hal terraform status"}, []opCheck{{Name: "terraform_runtime", Status: "error", Details: "unable to resolve runtime state"}}, nil, []string{"https://developer.hashicorp.com/terraform/enterprise"})
	}
	state, _ := product["state"].(string)
	reason, _ := product["reason"].(string)
	cliHelperReady := checkContainer(engine, "hal-tfe-cli")
	homeDir, _ := os.UserHomeDir()
	tokenPath := filepath.Join(homeDir, ".hal", "tfe-app-api-token")
	_, tokenErr := os.Stat(tokenPath)
	tokenReady := tokenErr == nil
	checks := []opCheck{
		{Name: "terraform_runtime", Status: checkStatusFromState(state), Details: reason},
		{Name: "terraform_cli_helper", Status: checkStatusFromState(boolState(cliHelperReady)), Details: "hal-tfe-cli helper availability"},
		{Name: "terraform_cli_token_cache", Status: checkStatusFromState(boolState(tokenReady)), Details: tokenPath},
	}
	data := map[string]interface{}{
		"runtime": product,
		"cli_helper": map[string]interface{}{
			"container": "hal-tfe-cli",
			"state":     boolState(cliHelperReady),
		},
		"token_cache": map[string]interface{}{
			"path":    tokenPath,
			"present": tokenReady,
		},
	}
	if state != "running" {
		return opErrorForTool("get_tfe_cli_status", runtimeCodeFromState(state), "tfe runtime not healthy; deploy terraform first", data, []string{"hal terraform deploy", "hal terraform status"}, checks, nil, []string{"https://developer.hashicorp.com/terraform/enterprise"})
	}
	if !cliHelperReady {
		return opErrorForTool("get_tfe_cli_status", codeNotDeployed, "tfe cli helper is not ready; run hal terraform cli", data, []string{"hal terraform cli", "hal tf cli -e", "hal tf cli -c"}, checks, nil, []string{"https://developer.hashicorp.com/terraform/enterprise"})
	}
	return opSuccessForTool("get_tfe_cli_status", "tfe cli helper status collected", data, []string{"hal terraform cli", "hal tf cli -e", "hal tf cli -c"}, checks, nil, nil, []string{"https://developer.hashicorp.com/terraform/enterprise"})
}

func handleEnableBoundaryMariaDB(args map[string]interface{}) mcpToolCallResult {
	if err := ensureOnlyKeys(args, map[string]bool{"mode": true, "force": true, "with_vault": true}); err != nil {
		return opErrorForTool("enable_boundary_mariadb", codeParseError, err.Error(), nil, []string{"hal boundary mariadb --enable"}, nil, nil, nil)
	}
	baseCmd := []string{"boundary", "mariadb", "--enable"}
	if raw, ok := args["with_vault"]; ok {
		parsed, ok := raw.(bool)
		if !ok {
			return opErrorForTool("enable_boundary_mariadb", codeParseError, "with_vault must be boolean", nil, []string{"hal boundary mariadb --enable"}, nil, nil, nil)
		}
		if parsed {
			baseCmd = append(baseCmd, "--with-vault")
		}
	}
	return handleEnableScenarioMode("enable_boundary_mariadb", baseCmd, []string{"hal boundary mariadb", "hal boundary status"}, args)
}
