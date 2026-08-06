package terraform

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	vault "github.com/hashicorp/vault/api"
)

func TestNewTFEVaultClientLive(t *testing.T) {
	if os.Getenv("HAL_LIVE_VAULT_TEST") != "1" {
		t.Skip("set HAL_LIVE_VAULT_TEST=1 with a local hal-vault instance")
	}
	client, cfg, err := newTFEVaultClient()
	if err != nil {
		t.Fatal(err)
	}
	if client.Address() != cfg.HostAddress {
		t.Fatalf("client address = %q, want %q", client.Address(), cfg.HostAddress)
	}
	if cfg.RunAddress == "" {
		t.Fatal("run address is empty")
	}
}

func TestBuildTFEVaultVariablesDev(t *testing.T) {
	vars := buildTFEVaultVariables(tfeVaultRuntimeConfig{RunAddress: defaultTFEExternalVaultAddr})

	want := map[string]string{
		"TFC_VAULT_PROVIDER_AUTH":              "true",
		"TFC_VAULT_ADDR":                       defaultTFEExternalVaultAddr,
		"TFC_VAULT_RUN_ROLE":                   defaultTFEVaultRole,
		"TFC_VAULT_AUTH_PATH":                  defaultTFEVaultJWTMount,
		"TFC_VAULT_WORKLOAD_IDENTITY_AUDIENCE": defaultTFEVaultAudience,
	}
	if len(vars) != len(want) {
		t.Fatalf("got %d variables, want %d: %#v", len(vars), len(want), vars)
	}
	for key, value := range want {
		if vars[key] != value {
			t.Errorf("%s = %q, want %q", key, vars[key], value)
		}
	}
	if _, exists := vars["VAULT_ADDR"]; exists {
		t.Fatal("raw VAULT_ADDR should be derived by TFE from TFC_VAULT_ADDR")
	}
}

func TestDevTFEVaultRuntimeUsesHostLoopback(t *testing.T) {
	cfg := devTFEVaultRuntimeConfig()
	if cfg.HostAddress != "http://127.0.0.1:8200" {
		t.Fatalf("host address = %q", cfg.HostAddress)
	}
	if cfg.RunAddress != "http://hal-vault:8200" {
		t.Fatalf("run address = %q", cfg.RunAddress)
	}
}

func TestConnectTFEVaultCandidatesFallsBackFromStaleProdState(t *testing.T) {
	prodCfg := tfeVaultRuntimeConfig{HostAddress: "https://127.0.0.1:8200", Prod: true}
	devCfg := devTFEVaultRuntimeConfig()
	wantClient := new(vault.Client)
	var attempted []string

	client, gotCfg, err := connectTFEVaultCandidates(
		[]tfeVaultRuntimeConfig{prodCfg, devCfg},
		func(cfg tfeVaultRuntimeConfig) (*vault.Client, error) {
			attempted = append(attempted, cfg.HostAddress)
			if cfg.Prod {
				return nil, errors.New("HTTP response to HTTPS client")
			}
			return wantClient, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if client != wantClient || gotCfg.Prod {
		t.Fatalf("selected client/config = %p/%#v, want dev fallback", client, gotCfg)
	}
	if len(attempted) != 2 || attempted[0] != prodCfg.HostAddress || attempted[1] != devCfg.HostAddress {
		t.Fatalf("attempted endpoints = %#v", attempted)
	}
}

func TestConnectTFEVaultCandidatesReportsEveryEndpoint(t *testing.T) {
	configs := []tfeVaultRuntimeConfig{
		{HostAddress: "https://127.0.0.1:8200", Prod: true},
		devTFEVaultRuntimeConfig(),
	}
	_, _, err := connectTFEVaultCandidates(configs, func(cfg tfeVaultRuntimeConfig) (*vault.Client, error) {
		return nil, errors.New("unreachable")
	})
	if err == nil {
		t.Fatal("expected connection failure")
	}
	for _, cfg := range configs {
		if !strings.Contains(err.Error(), cfg.HostAddress) {
			t.Fatalf("error %q does not include %q", err, cfg.HostAddress)
		}
	}
}

func TestConnectTFEVaultClientIgnoresStaleAmbientCACert(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sys/health" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"initialized":true,"sealed":false,"standby":false,"version":"2.0.1"}`))
	}))
	defer server.Close()

	missingCA := filepath.Join(t.TempDir(), "missing-cert.pem")
	t.Setenv(vault.EnvVaultCACert, missingCA)
	t.Setenv(vault.EnvVaultAddress, "https://wrong.example.invalid:8200")

	cfg := devTFEVaultRuntimeConfig()
	cfg.HostAddress = server.URL
	client, err := connectTFEVaultClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if client.Address() != server.URL {
		t.Fatalf("client address = %q, want %q", client.Address(), server.URL)
	}
	if got := os.Getenv(vault.EnvVaultCACert); got != missingCA {
		t.Fatalf("VAULT_CACERT was not restored: %q", got)
	}
	if got := os.Getenv(vault.EnvVaultAddress); got != "https://wrong.example.invalid:8200" {
		t.Fatalf("VAULT_ADDR was not restored: %q", got)
	}
}

func TestBuildTFEVaultVariablesProdIncludesEncodedCA(t *testing.T) {
	const caPEM = "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"
	vars := buildTFEVaultVariables(tfeVaultRuntimeConfig{
		RunAddress: defaultTFEExternalVaultProdAddr,
		CACertPEM:  caPEM,
		Prod:       true,
	})

	if vars["TFC_VAULT_ADDR"] != defaultTFEExternalVaultProdAddr {
		t.Fatalf("TFC_VAULT_ADDR = %q, want %q", vars["TFC_VAULT_ADDR"], defaultTFEExternalVaultProdAddr)
	}
	wantCA := base64.StdEncoding.EncodeToString([]byte(caPEM))
	if vars["TFC_VAULT_ENCODED_CACERT"] != wantCA {
		t.Fatalf("encoded CA = %q, want %q", vars["TFC_VAULT_ENCODED_CACERT"], wantCA)
	}
}

func TestBuildTFEVaultJWTConfigUsesDiscoveryBaseURL(t *testing.T) {
	config := buildTFEVaultJWTConfig(
		"https://tfe.localhost/",
		"test-ca",
	)

	if got := config["oidc_discovery_url"]; got != "https://tfe.localhost" {
		t.Fatalf("oidc_discovery_url = %q", got)
	}
	if strings.Contains(config["oidc_discovery_url"].(string), ".well-known") {
		t.Fatal("oidc_discovery_url must not contain a .well-known component")
	}
	if got := config["bound_issuer"]; got != "https://tfe.localhost" {
		t.Fatalf("bound_issuer = %q", got)
	}
	if got := config["oidc_discovery_ca_pem"]; got != "test-ca" {
		t.Fatalf("oidc_discovery_ca_pem = %q", got)
	}
}

func TestBuildTFEVaultJWTRoleScopesOrganizationAndAudience(t *testing.T) {
	role, err := buildTFEVaultJWTRole("HAL")
	if err != nil {
		t.Fatal(err)
	}

	audiences, ok := role["bound_audiences"].([]string)
	if !ok || len(audiences) != 1 || audiences[0] != defaultTFEVaultAudience {
		t.Fatalf("bound_audiences = %#v", role["bound_audiences"])
	}
	claims, ok := role["bound_claims"].(map[string]interface{})
	if !ok {
		t.Fatalf("bound_claims = %#v", role["bound_claims"])
	}
	wantSubject := "organization:hal:project:*:workspace:*:run_phase:*"
	if claims["sub"] != wantSubject {
		t.Fatalf("subject claim = %q, want %q", claims["sub"], wantSubject)
	}
	if role["bound_claims_type"] != "glob" {
		t.Fatalf("bound_claims_type = %q", role["bound_claims_type"])
	}
	if role["user_claim"] != "terraform_full_workspace" {
		t.Fatalf("user_claim = %q", role["user_claim"])
	}
}

func TestTFEVaultPolicySupportsManagedTokenLifecycle(t *testing.T) {
	for _, path := range []string{
		`path "auth/token/lookup-self"`,
		`path "auth/token/renew-self"`,
		`path "auth/token/revoke-self"`,
		`path "secret/data/*"`,
		`path "secret/metadata/*"`,
	} {
		if !strings.Contains(tfeVaultWorkspacePolicy, path) {
			t.Errorf("policy missing %s", path)
		}
	}
}

func TestValidateTFEVaultTarget(t *testing.T) {
	if err := validateTFEVaultTarget(tfeTargetPrimary); err != nil {
		t.Fatalf("primary target rejected: %v", err)
	}
	if err := validateTFEVaultTarget(tfeTargetBoth); err != nil {
		t.Fatalf("both target rejected: %v", err)
	}
	if err := validateTFEVaultTarget(tfeTargetTwin); err == nil {
		t.Fatal("twin target should be rejected until it has a separate issuer design")
	}
}
