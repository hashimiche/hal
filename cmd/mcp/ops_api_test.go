package mcp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type toolInvocation struct {
	name string
	args map[string]interface{}
}

func TestRequiredOpsToolsRegistered(t *testing.T) {
	required := []string{
		"get_runtime_status",
		"get_vault_status",
		"get_terraform_status",
		"get_audit_summary",
		"get_oidc_status",
		"get_jwt_status",
		"get_boundary_status",
		"get_tfe_status",
		"get_tfe_api_workflow_status",
		"get_tfe_vcs_workflow_status",
		"get_k8s_integration_status",
		"get_vault_pki_status",
		"get_ldap_status",
		"get_vault_database_status",
		"get_boundary_mariadb_status",
		"get_consul_status",
		"get_nomad_status",
		"get_obs_status",
		"get_active_credentials",
		"get_capabilities",
		"hal_policy_profile",
		"validate_command",
		"get_boundary_ssh_status",
		"get_tfe_agent_status",
		"get_tfe_twin_status",
		"get_tfe_saml_status",
		"get_vault_userpass_status",
		"get_vault_os_status",
		"get_product_obs_status",
		"get_plus_status",
		"get_version",
	}
	tools := mcpOpsTools()
	seen := map[string]bool{}
	for _, tool := range tools {
		name, _ := tool["name"].(string)
		seen[name] = true
	}
	for _, name := range required {
		if !seen[name] {
			t.Fatalf("required tool not registered: %s", name)
		}
	}
}

func TestOpsResponsesContainContractFields(t *testing.T) {
	// Status tools shell out to a real `hal` binary via runHAL, which is absent
	// in CI. Substitute a deterministic successful execution so this test
	// validates success-path envelope construction hermetically, everywhere —
	// rather than silently exercising only the "hal not found" error path.
	restore := runHAL
	runHAL = func(args ...string) toolExecution {
		return toolExecution{
			Command:   "hal " + strings.Join(args, " "),
			ExitCode:  0,
			Output:    "status: ok",
			Timestamp: "2026-01-01T00:00:00Z",
		}
	}
	defer func() { runHAL = restore }()

	invocations := []toolInvocation{
		{name: "get_runtime_status", args: map[string]interface{}{}},
		{name: "get_vault_status", args: map[string]interface{}{}},
		{name: "get_terraform_status", args: map[string]interface{}{}},
		{name: "get_audit_summary", args: map[string]interface{}{}},
		{name: "get_oidc_status", args: map[string]interface{}{}},
		{name: "get_jwt_status", args: map[string]interface{}{}},
		{name: "get_boundary_status", args: map[string]interface{}{}},
		{name: "get_tfe_status", args: map[string]interface{}{}},
		{name: "get_tfe_vcs_workflow_status", args: map[string]interface{}{}},
		{name: "get_k8s_integration_status", args: map[string]interface{}{}},
		{name: "get_vault_pki_status", args: map[string]interface{}{}},
		{name: "get_ldap_status", args: map[string]interface{}{}},
		{name: "get_vault_database_status", args: map[string]interface{}{}},
		{name: "get_boundary_mariadb_status", args: map[string]interface{}{}},
		{name: "get_consul_status", args: map[string]interface{}{}},
		{name: "get_nomad_status", args: map[string]interface{}{}},
		{name: "get_obs_status", args: map[string]interface{}{}},
		{name: "get_active_credentials", args: map[string]interface{}{}},
		{name: "get_capabilities", args: map[string]interface{}{}},
		{name: "hal_policy_profile", args: map[string]interface{}{}},
		{name: "validate_command", args: map[string]interface{}{"command": "hal vault status"}},
		{name: "get_boundary_ssh_status", args: map[string]interface{}{}},
		{name: "get_tfe_agent_status", args: map[string]interface{}{}},
		{name: "get_tfe_twin_status", args: map[string]interface{}{}},
		{name: "get_tfe_saml_status", args: map[string]interface{}{}},
		{name: "get_vault_userpass_status", args: map[string]interface{}{}},
		{name: "get_vault_os_status", args: map[string]interface{}{}},
		{name: "get_product_obs_status", args: map[string]interface{}{"product": "vault"}},
		{name: "get_plus_status", args: map[string]interface{}{}},
		{name: "get_version", args: map[string]interface{}{}},
	}

	for _, tc := range invocations {
		res, handled := handleOpsTool(tc.name, tc.args)
		if !handled {
			t.Fatalf("tool not handled: %s", tc.name)
		}
		if len(res.Content) == 0 || strings.TrimSpace(res.Content[0].Text) == "" {
			t.Fatalf("empty content for %s", tc.name)
		}

		var envelope map[string]interface{}
		if err := json.Unmarshal([]byte(res.Content[0].Text), &envelope); err != nil {
			t.Fatalf("invalid json content for %s: %v", tc.name, err)
		}

		requiredKeys := []string{"contract_version", "status", "code", "message", "domain", "capability", "resource", "data", "recommended_commands", "checks", "docs"}
		for _, k := range requiredKeys {
			if _, ok := envelope[k]; !ok {
				t.Fatalf("missing key %s for %s", k, tc.name)
			}
		}
	}
}

// TestStatusToolEnvelopeSuccessAndError verifies that a runHAL-backed status
// tool maps a clean execution to a success envelope and a failed execution to
// an error envelope. Using the runHAL seam makes this deterministic and
// independent of whether a hal binary is installed.
func TestStatusToolEnvelopeSuccessAndError(t *testing.T) {
	restore := runHAL
	defer func() { runHAL = restore }()

	decodeStatus := func(res mcpToolCallResult) string {
		t.Helper()
		if len(res.Content) == 0 {
			t.Fatal("tool returned no content")
		}
		var env map[string]interface{}
		if err := json.Unmarshal([]byte(res.Content[0].Text), &env); err != nil {
			t.Fatalf("invalid json content: %v", err)
		}
		status, _ := env["status"].(string)
		return status
	}

	// Clean execution -> success envelope.
	runHAL = func(args ...string) toolExecution {
		return toolExecution{Command: "hal " + strings.Join(args, " "), ExitCode: 0, Output: "vault: up", Timestamp: "2026-01-01T00:00:00Z"}
	}
	res, handled := handleOpsTool("get_vault_status", map[string]interface{}{})
	if !handled {
		t.Fatal("get_vault_status not handled")
	}
	if got := decodeStatus(res); got != statusSuccess {
		t.Errorf("clean execution: status = %q, want %q", got, statusSuccess)
	}

	// Failed execution -> error envelope.
	runHAL = func(args ...string) toolExecution {
		return toolExecution{Command: "hal " + strings.Join(args, " "), ExitCode: 1, Output: "vault unreachable", Timestamp: "2026-01-01T00:00:00Z"}
	}
	res, _ = handleOpsTool("get_vault_status", map[string]interface{}{})
	if got := decodeStatus(res); got != statusError {
		t.Errorf("failed execution: status = %q, want %q", got, statusError)
	}
}

func TestRecommendedCommandsAreExecutableSyntax(t *testing.T) {
	restore := runHAL
	runHAL = func(args ...string) toolExecution {
		return toolExecution{
			Command:   "hal " + strings.Join(args, " "),
			ExitCode:  0,
			Output:    "status: ok",
			Timestamp: "2026-01-01T00:00:00Z",
		}
	}
	defer func() { runHAL = restore }()

	invocations := []toolInvocation{
		{name: "get_runtime_status", args: map[string]interface{}{}},
		{name: "get_vault_status", args: map[string]interface{}{}},
		{name: "get_terraform_status", args: map[string]interface{}{}},
		{name: "get_oidc_status", args: map[string]interface{}{}},
		{name: "get_jwt_status", args: map[string]interface{}{}},
		{name: "get_boundary_status", args: map[string]interface{}{}},
		{name: "get_tfe_status", args: map[string]interface{}{}},
		{name: "get_k8s_integration_status", args: map[string]interface{}{}},
		{name: "get_vault_pki_status", args: map[string]interface{}{}},
		{name: "get_ldap_status", args: map[string]interface{}{}},
		{name: "get_vault_database_status", args: map[string]interface{}{}},
		{name: "get_boundary_mariadb_status", args: map[string]interface{}{}},
		{name: "get_consul_status", args: map[string]interface{}{}},
		{name: "get_nomad_status", args: map[string]interface{}{}},
		{name: "get_obs_status", args: map[string]interface{}{}},
	}

	for _, tc := range invocations {
		res, handled := handleOpsTool(tc.name, tc.args)
		if !handled {
			t.Fatalf("tool not handled: %s", tc.name)
		}
		var payload opContractResponse
		raw, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("failed to decode payload for %s: %v", tc.name, err)
		}
		for _, cmd := range payload.RecommendedCommands {
			if strings.HasPrefix(cmd, "hal ") {
				check := validateCommand(cmd)
				valid, _ := check["valid"].(bool)
				if !valid {
					t.Fatalf("invalid recommended hal command for %s: %s", tc.name, cmd)
				}
			}
		}
	}
}

func TestScenarioCodesRunningNotDeployedAuthMissing(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		code string
	}{
		{name: "running", msg: "service is up and healthy", code: codeUnsupportedOp},
		{name: "not_deployed", msg: "vault is not deployed", code: codeNotDeployed},
		{name: "auth_missing", msg: "token unauthorized", code: codeNotAuthenticated},
	}
	for _, tc := range cases {
		got := classifyContractError(tc.msg)
		if got != tc.code {
			t.Fatalf("%s expected %s got %s", tc.name, tc.code, got)
		}
	}
}

func TestContractValidatorRejectsInvalidEnvelope(t *testing.T) {
	bad := opContractResponse{
		Status:              statusSuccess,
		Code:                "ok",
		Message:             "bad",
		Domain:              "invalid-domain",
		Capability:          "x",
		Resource:            "y",
		Data:                map[string]interface{}{},
		RecommendedCommands: []string{"badcmd"},
		Checks:              []opCheck{{Name: "c1", Status: "ok"}},
		Docs:                []string{"not-a-url"},
	}
	if err := validateContractEnvelope(bad); err == nil {
		t.Fatalf("expected validation error for invalid envelope")
	}
}

func TestContractValidationFailureShape(t *testing.T) {
	resp := contractValidationFailure("get_vault_status", errors.New("bad contract"))
	if resp.Status != statusError {
		t.Fatalf("expected error status")
	}
	if resp.Code != codeParseError {
		t.Fatalf("expected parse_error code")
	}
	if len(resp.RecommendedCommands) == 0 {
		t.Fatalf("expected recovery command")
	}
}

func TestInvalidArgsReturnErrorAndRecoveryCommands(t *testing.T) {
	cases := []toolInvocation{
		{name: "get_vault_status", args: map[string]interface{}{"bad": true}},
		{name: "get_terraform_status", args: map[string]interface{}{"bad": true}},
		{name: "get_boundary_status", args: map[string]interface{}{"bad": true}},
		{name: "get_consul_status", args: map[string]interface{}{"bad": true}},
		{name: "get_nomad_status", args: map[string]interface{}{"bad": true}},
		{name: "get_obs_status", args: map[string]interface{}{"bad": true}},
		{name: "get_ldap_status", args: map[string]interface{}{"bad": true}},
		{name: "get_vault_database_status", args: map[string]interface{}{"bad": true}},
		{name: "get_boundary_mariadb_status", args: map[string]interface{}{"bad": true}},
		{name: "get_k8s_integration_status", args: map[string]interface{}{"bad": true}},
		{name: "get_tfe_status", args: map[string]interface{}{"bad": true}},
	}

	for _, tc := range cases {
		res, handled := handleOpsTool(tc.name, tc.args)
		if !handled {
			t.Fatalf("tool not handled: %s", tc.name)
		}
		if !res.IsError {
			t.Fatalf("expected error for invalid args: %s", tc.name)
		}

		var payload opContractResponse
		raw, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("decode failed for %s: %v", tc.name, err)
		}
		if payload.Status != statusError {
			t.Fatalf("expected status=error for %s", tc.name)
		}
		if len(payload.RecommendedCommands) == 0 {
			t.Fatalf("expected recovery commands for %s", tc.name)
		}
	}
}

func TestValidateCommandReflectsCurrentSurface(t *testing.T) {
	valid := []string{
		"hal terraform api-workflow", "hal terraform vcs-workflow", "hal terraform twin",
		"hal terraform saml", "hal terraform agent", "hal terraform obs",
		"hal terraform workspace", "hal terraform api",
		"hal vault pki", "hal vault userpass", "hal vault os", "hal vault obs", "hal vault db",
		"hal boundary ssh", "hal boundary obs", "hal consul obs", "hal nomad obs",
		"hal plus status", "hal health create", "hal creds status",
	}
	for _, c := range valid {
		res := validateCommand(c)
		if ok, _ := res["valid"].(bool); !ok {
			t.Fatalf("expected %q to be valid against current surface", c)
		}
	}

	invalid := []string{"hal terraform cli", "hal vault bogus", "hal boundary nope"}
	for _, c := range invalid {
		res := validateCommand(c)
		if ok, _ := res["valid"].(bool); ok {
			t.Fatalf("expected %q to be rejected", c)
		}
	}
}

func TestProductObsStatusRejectsUnknownProduct(t *testing.T) {
	res, handled := handleOpsTool("get_product_obs_status", map[string]interface{}{"product": "bogus"})
	if !handled {
		t.Fatalf("get_product_obs_status not handled")
	}
	if !res.IsError {
		t.Fatalf("expected error for unknown obs product")
	}
}
