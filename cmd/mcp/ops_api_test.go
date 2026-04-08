package mcp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
		"enable_vault",
		"get_terraform_status",
		"enable_terraform",
		"get_capabilities",
		"get_help_for_topic",
		"plan_next_steps",
		"validate_command",
		"get_component_context",
		"get_audit_summary",
		"get_oidc_status",
		"enable_oidc",
		"get_jwt_status",
		"enable_jwt",
		"get_boundary_status",
		"enable_boundary",
		"get_ssh_flow_status",
		"get_tfe_status",
		"setup_tfe_workspace",
		"get_k8s_integration_status",
		"enable_vault_k8s_integration",
		"get_cross_product_dependencies",
		"get_ldap_status",
		"enable_ldap",
		"get_vault_mariadb_status",
		"enable_vault_mariadb",
		"get_boundary_mariadb_status",
		"enable_boundary_mariadb",
		"get_consul_status",
		"enable_consul",
		"get_nomad_status",
		"enable_nomad",
		"get_obs_status",
		"enable_obs",
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
	invocations := []toolInvocation{
		{name: "get_runtime_status", args: map[string]interface{}{}},
		{name: "get_vault_status", args: map[string]interface{}{}},
		{name: "enable_vault", args: map[string]interface{}{"mode": "dry_run"}},
		{name: "get_terraform_status", args: map[string]interface{}{}},
		{name: "enable_terraform", args: map[string]interface{}{"mode": "dry_run"}},
		{name: "get_capabilities", args: map[string]interface{}{}},
		{name: "get_help_for_topic", args: map[string]interface{}{"topic": "vault oidc"}},
		{name: "plan_next_steps", args: map[string]interface{}{"intent": "setup terraform workspace"}},
		{name: "validate_command", args: map[string]interface{}{"command": "hal status"}},
		{name: "get_component_context", args: map[string]interface{}{"component": "vault_k8s"}},
		{name: "get_component_context", args: map[string]interface{}{"component": "vault_ldap"}},
		{name: "get_audit_summary", args: map[string]interface{}{}},
		{name: "get_oidc_status", args: map[string]interface{}{}},
		{name: "enable_oidc", args: map[string]interface{}{"mode": "dry_run"}},
		{name: "get_jwt_status", args: map[string]interface{}{}},
		{name: "enable_jwt", args: map[string]interface{}{"mode": "dry_run"}},
		{name: "get_boundary_status", args: map[string]interface{}{}},
		{name: "enable_boundary", args: map[string]interface{}{"mode": "dry_run"}},
		{name: "get_ssh_flow_status", args: map[string]interface{}{}},
		{name: "get_tfe_status", args: map[string]interface{}{}},
		{name: "setup_tfe_workspace", args: map[string]interface{}{"mode": "dry_run"}},
		{name: "get_k8s_integration_status", args: map[string]interface{}{}},
		{name: "enable_vault_k8s_integration", args: map[string]interface{}{"mode": "dry_run", "csi": true}},
		{name: "get_cross_product_dependencies", args: map[string]interface{}{}},
		{name: "get_ldap_status", args: map[string]interface{}{}},
		{name: "enable_ldap", args: map[string]interface{}{"mode": "dry_run"}},
		{name: "get_vault_mariadb_status", args: map[string]interface{}{}},
		{name: "enable_vault_mariadb", args: map[string]interface{}{"mode": "dry_run"}},
		{name: "get_boundary_mariadb_status", args: map[string]interface{}{}},
		{name: "enable_boundary_mariadb", args: map[string]interface{}{"mode": "dry_run", "with_vault": true}},
		{name: "get_consul_status", args: map[string]interface{}{}},
		{name: "enable_consul", args: map[string]interface{}{"mode": "dry_run"}},
		{name: "get_nomad_status", args: map[string]interface{}{}},
		{name: "enable_nomad", args: map[string]interface{}{"mode": "dry_run"}},
		{name: "get_obs_status", args: map[string]interface{}{}},
		{name: "enable_obs", args: map[string]interface{}{"mode": "dry_run"}},
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

		requiredKeys := []string{"status", "code", "message", "domain", "capability", "resource", "data", "recommended_commands", "checks", "docs"}
		for _, k := range requiredKeys {
			if _, ok := envelope[k]; !ok {
				t.Fatalf("missing key %s for %s", k, tc.name)
			}
		}
	}
}

func TestRecommendedCommandsAreExecutableSyntax(t *testing.T) {
	invocations := []toolInvocation{
		{name: "get_runtime_status", args: map[string]interface{}{}},
		{name: "enable_vault", args: map[string]interface{}{"mode": "dry_run"}},
		{name: "enable_terraform", args: map[string]interface{}{"mode": "dry_run"}},
		{name: "get_help_for_topic", args: map[string]interface{}{"topic": "vault jwt"}},
		{name: "get_help_for_topic", args: map[string]interface{}{"topic": "vault ldap"}},
		{name: "plan_next_steps", args: map[string]interface{}{"intent": "boundary ssh"}},
		{name: "plan_next_steps", args: map[string]interface{}{"intent": "vault ldap"}},
		{name: "enable_oidc", args: map[string]interface{}{"mode": "dry_run"}},
		{name: "enable_jwt", args: map[string]interface{}{"mode": "dry_run"}},
		{name: "enable_boundary", args: map[string]interface{}{"mode": "dry_run"}},
		{name: "setup_tfe_workspace", args: map[string]interface{}{"mode": "dry_run"}},
		{name: "enable_vault_k8s_integration", args: map[string]interface{}{"mode": "dry_run"}},
		{name: "enable_ldap", args: map[string]interface{}{"mode": "dry_run"}},
		{name: "enable_vault_mariadb", args: map[string]interface{}{"mode": "dry_run"}},
		{name: "enable_boundary_mariadb", args: map[string]interface{}{"mode": "dry_run", "with_vault": true}},
		{name: "enable_consul", args: map[string]interface{}{"mode": "dry_run"}},
		{name: "enable_nomad", args: map[string]interface{}{"mode": "dry_run"}},
		{name: "enable_obs", args: map[string]interface{}{"mode": "dry_run"}},
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

func TestHelpSnapshotsAcrossProducts(t *testing.T) {
	cases := []struct {
		name    string
		topic   string
		fixture string
	}{
		{name: "vault", topic: "vault", fixture: "vault_help_snapshot.json"},
		{name: "oidc", topic: "vault oidc", fixture: "oidc_help_snapshot.json"},
		{name: "jwt", topic: "vault jwt", fixture: "jwt_help_snapshot.json"},
		{name: "ldap", topic: "vault ldap", fixture: "ldap_help_snapshot.json"},
		{name: "vault_mariadb", topic: "vault mariadb", fixture: "vault_mariadb_help_snapshot.json"},
		{name: "vault_k8s", topic: "vault k8s", fixture: "vault_k8s_help_snapshot.json"},
		{name: "boundary", topic: "boundary", fixture: "boundary_help_snapshot.json"},
		{name: "boundary_ssh", topic: "boundary ssh", fixture: "boundary_ssh_help_snapshot.json"},
		{name: "boundary_mariadb", topic: "boundary mariadb", fixture: "boundary_mariadb_help_snapshot.json"},
		{name: "terraform", topic: "terraform", fixture: "terraform_help_snapshot.json"},
		{name: "terraform_workspace", topic: "terraform workspace", fixture: "terraform_workspace_help_snapshot.json"},
		{name: "consul", topic: "consul", fixture: "consul_help_snapshot.json"},
		{name: "nomad", topic: "nomad", fixture: "nomad_help_snapshot.json"},
		{name: "obs", topic: "obs", fixture: "obs_help_snapshot.json"},
	}

	for _, tc := range cases {
		res, handled := handleOpsTool("get_help_for_topic", map[string]interface{}{"topic": tc.topic})
		if !handled {
			t.Fatalf("tool not handled for %s", tc.name)
		}
		if res.IsError {
			t.Fatalf("get_help_for_topic returned error for %s", tc.name)
		}
		var payload opContractResponse
		raw, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("decode failed for %s: %v", tc.name, err)
		}
		actual, err := json.MarshalIndent(payload.Data, "", "  ")
		if err != nil {
			t.Fatalf("marshal data failed for %s: %v", tc.name, err)
		}

		fixturePath := filepath.Join("testdata", tc.fixture)
		if os.Getenv("UPDATE_SNAPSHOTS") == "1" {
			if err := os.MkdirAll("testdata", 0o755); err != nil {
				t.Fatalf("mkdir testdata failed: %v", err)
			}
			if err := os.WriteFile(fixturePath, append(actual, '\n'), 0o644); err != nil {
				t.Fatalf("write snapshot failed: %v", err)
			}
		}

		expected, err := os.ReadFile(fixturePath)
		if err != nil {
			t.Fatalf("read snapshot failed for %s: %v", tc.name, err)
		}
		if strings.TrimSpace(string(expected)) != strings.TrimSpace(string(actual)) {
			t.Fatalf("snapshot mismatch for %s", tc.name)
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
		{name: "enable_vault", args: map[string]interface{}{"mode": 123}},
		{name: "get_terraform_status", args: map[string]interface{}{"bad": true}},
		{name: "enable_terraform", args: map[string]interface{}{"mode": 123}},
		{name: "get_boundary_status", args: map[string]interface{}{"bad": true}},
		{name: "enable_boundary", args: map[string]interface{}{"mode": 123}},
		{name: "get_consul_status", args: map[string]interface{}{"bad": true}},
		{name: "enable_consul", args: map[string]interface{}{"mode": 123}},
		{name: "get_nomad_status", args: map[string]interface{}{"bad": true}},
		{name: "enable_nomad", args: map[string]interface{}{"mode": 123}},
		{name: "get_obs_status", args: map[string]interface{}{"bad": true}},
		{name: "enable_obs", args: map[string]interface{}{"mode": 123}},
		{name: "get_ldap_status", args: map[string]interface{}{"bad": true}},
		{name: "enable_ldap", args: map[string]interface{}{"mode": 123}},
		{name: "get_vault_mariadb_status", args: map[string]interface{}{"bad": true}},
		{name: "enable_vault_mariadb", args: map[string]interface{}{"mode": 123}},
		{name: "get_boundary_mariadb_status", args: map[string]interface{}{"bad": true}},
		{name: "enable_boundary_mariadb", args: map[string]interface{}{"mode": 123}},
		{name: "get_k8s_integration_status", args: map[string]interface{}{"bad": true}},
		{name: "enable_vault_k8s_integration", args: map[string]interface{}{"mode": 123}},
		{name: "get_tfe_status", args: map[string]interface{}{"bad": true}},
		{name: "setup_tfe_workspace", args: map[string]interface{}{"mode": 123}},
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
