package terraform

// vault.go wires the running hal-vault container into TFE workspace runs via
// the Dynamic Provider Credentials (JWT auth) pattern:
//
//  1. Enable the JWT auth method at defaultTFEVaultJWTMount ("jwt-tfe").
//  2. Configure it to trust TFE's OIDC issuer so TFE can issue short-lived
//     workload identity JWTs that workspace runs exchange for Vault tokens.
//  3. Write an organization-scoped Vault policy and JWT role.
//  4. Reconcile the TFC_VAULT_* environment variables TFE requires.

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"hal/internal/global"
	"hal/internal/integrations"

	vault "github.com/hashicorp/vault/api"
)

type tfeVaultRuntimeConfig struct {
	HostAddress string
	RunAddress  string
	CACertPEM   string
	Token       string
	Prod        bool
}

func validateTFEVaultTarget(target string) error {
	if target == tfeTargetTwin {
		return fmt.Errorf("--vault-enabled currently supports the primary TFE issuer only; use '--target primary' or omit --vault-enabled")
	}
	return nil
}

const tfeVaultWorkspacePolicy = `path "auth/token/lookup-self" {
  capabilities = ["read"]
}

path "auth/token/renew-self" {
  capabilities = ["update"]
}

path "auth/token/revoke-self" {
  capabilities = ["update"]
}

path "secret/data/*" {
  capabilities = ["read"]
}

path "secret/metadata/*" {
  capabilities = ["read", "list"]
}`

// runVaultEnabledWiring applies --vault-enabled wiring to an already-running
// primary TFE instance. It bootstraps a TFE token from cache or the API, then
// configures Vault JWT auth and the TFE variable set without requiring a TFE
// redeploy or license lookup.
func runVaultEnabledWiring(engine string) {
	uiURL := tfePrimaryBaseURL

	cfg := tfeFoundationConfig{
		BaseURL:       uiURL,
		OrgName:       deployTFEOrg,
		AdminUsername: deployTFEAdminUser,
		AdminEmail:    deployTFEAdminEmail,
		AdminPassword: deployTFEAdminPass,
	}
	token, _, foundationErr := ensureTFEFoundation(engine, cfg)
	if token == "" {
		fmt.Printf("❌ --vault-enabled: could not obtain TFE API token: %v\n", foundationErr)
		fmt.Println("   💡 Make sure TFE is fully initialized: hal terraform status")
		return
	}

	if !global.IsContainerRunning(engine, defaultTFEExternalVaultContainer) {
		fmt.Println("❌ --vault-enabled: hal-vault is not running.")
		fmt.Println("   💡 Run 'hal vault create' first.")
		return
	}

	fmt.Println("⚙️  Wiring hal-vault as external secrets backend (Dynamic Provider Credentials)...")
	runtimeCfg, err := applyTFEVaultWiring(uiURL, token, deployTFEOrg)
	if err != nil {
		fmt.Printf("❌ --vault-enabled: %v\n", err)
		return
	}

	printTFEVaultWiringSummary(runtimeCfg.RunAddress)
	global.RefreshHalHealth(engine)
}

func applyTFEVaultWiring(tfeBaseURL, token, orgName string) (tfeVaultRuntimeConfig, error) {
	vaultClient, runtimeCfg, err := newTFEVaultClient()
	if err != nil {
		return tfeVaultRuntimeConfig{}, fmt.Errorf("could not connect to hal-vault: %w", err)
	}

	if err := ensureTFEVaultJWT(vaultClient, tfeBaseURL, orgName); err != nil {
		return tfeVaultRuntimeConfig{}, fmt.Errorf("vault JWT configuration failed: %w", err)
	}

	if err := integrations.EnsureTFEVariableSet(
		tfeBaseURL,
		token,
		orgName,
		defaultTFEVaultVarSet,
		buildTFEVaultVariables(runtimeCfg),
	); err != nil {
		return tfeVaultRuntimeConfig{}, fmt.Errorf("TFE variable set creation failed: %w", err)
	}
	if err := integrations.DeleteTFEVariableSetVariable(
		tfeBaseURL,
		token,
		orgName,
		defaultTFEVaultVarSet,
		"VAULT_ADDR",
	); err != nil {
		return tfeVaultRuntimeConfig{}, fmt.Errorf("remove legacy VAULT_ADDR variable: %w", err)
	}

	return runtimeCfg, nil
}

func printTFEVaultWiringSummary(vaultAddress string) {
	fmt.Printf("✅ JWT auth mount  : %s\n", defaultTFEVaultJWTMount)
	fmt.Printf("✅ Vault policy    : %s\n", defaultTFEVaultPolicy)
	fmt.Printf("✅ Vault role      : %s\n", defaultTFEVaultRole)
	fmt.Printf("✅ TFE variable set: %s  (TFC_VAULT_ADDR=%s)\n", defaultTFEVaultVarSet, vaultAddress)
	fmt.Println("   Workspaces can now authenticate to Vault via Dynamic Provider Credentials.")
}

func buildTFEVaultVariables(cfg tfeVaultRuntimeConfig) map[string]string {
	vars := map[string]string{
		"TFC_VAULT_PROVIDER_AUTH":              "true",
		"TFC_VAULT_ADDR":                       cfg.RunAddress,
		"TFC_VAULT_RUN_ROLE":                   defaultTFEVaultRole,
		"TFC_VAULT_AUTH_PATH":                  defaultTFEVaultJWTMount,
		"TFC_VAULT_WORKLOAD_IDENTITY_AUDIENCE": defaultTFEVaultAudience,
	}
	if cfg.CACertPEM != "" {
		vars["TFC_VAULT_ENCODED_CACERT"] = base64.StdEncoding.EncodeToString([]byte(cfg.CACertPEM))
	}
	return vars
}

// resolveTFEVaultRuntimeConfig builds the persisted production candidate when
// HAL-forged state exists under ~/.hal/vault-prod. newTFEVaultClient validates
// that candidate against the live endpoint and falls back to dev mode when the
// state is stale.
func resolveTFEVaultRuntimeConfig() (tfeVaultRuntimeConfig, error) {
	prodCertPath := global.VaultProdCertPath()
	if prodCertPath == "" {
		return devTFEVaultRuntimeConfig(), nil
	}

	if _, err := os.Stat(prodCertPath); err != nil {
		if os.IsNotExist(err) {
			return devTFEVaultRuntimeConfig(), nil
		}
		return tfeVaultRuntimeConfig{}, fmt.Errorf("inspect prod Vault certificate: %w", err)
	}

	caPEM, err := os.ReadFile(prodCertPath)
	if err != nil {
		return tfeVaultRuntimeConfig{}, fmt.Errorf("read prod Vault certificate: %w", err)
	}
	initData, err := global.LoadCachedVaultInit()
	if err != nil {
		return tfeVaultRuntimeConfig{}, fmt.Errorf("load prod Vault root token: %w", err)
	}
	if strings.TrimSpace(initData.RootToken) == "" {
		return tfeVaultRuntimeConfig{}, fmt.Errorf("load prod Vault root token: cached token is empty")
	}

	return tfeVaultRuntimeConfig{
		HostAddress: "https://127.0.0.1:8200",
		RunAddress:  defaultTFEExternalVaultProdAddr,
		CACertPEM:   string(caPEM),
		Token:       initData.RootToken,
		Prod:        true,
	}, nil
}

func devTFEVaultRuntimeConfig() tfeVaultRuntimeConfig {
	return tfeVaultRuntimeConfig{
		HostAddress: "http://127.0.0.1:8200",
		RunAddress:  defaultTFEExternalVaultAddr,
		Token:       "root",
	}
}

func newTFEVaultClient() (*vault.Client, tfeVaultRuntimeConfig, error) {
	preferredCfg, stateErr := resolveTFEVaultRuntimeConfig()
	candidates := []tfeVaultRuntimeConfig{devTFEVaultRuntimeConfig()}
	if stateErr == nil && preferredCfg.Prod {
		// Production state can outlive its container when a user switches back
		// to dev mode without running `hal vault delete`. Try the state-indicated
		// HTTPS endpoint first, then fall back to the live dev HTTP endpoint.
		candidates = append([]tfeVaultRuntimeConfig{preferredCfg}, candidates...)
	}

	client, runtimeCfg, err := connectTFEVaultCandidates(candidates, connectTFEVaultClient)
	if err != nil && stateErr != nil {
		return nil, tfeVaultRuntimeConfig{}, fmt.Errorf("%w; production state is unusable: %v", err, stateErr)
	}
	return client, runtimeCfg, err
}

type tfeVaultClientConnector func(tfeVaultRuntimeConfig) (*vault.Client, error)

type tfeVaultSavedEnv struct {
	value string
	set   bool
}

var tfeVaultClientEnvMu sync.Mutex

var tfeVaultClientEnvKeys = []string{
	vault.EnvVaultAddress,
	vault.EnvVaultAgentAddr,
	vault.EnvVaultCACert,
	vault.EnvVaultCACertBytes,
	vault.EnvVaultCAPath,
	vault.EnvVaultClientCert,
	vault.EnvVaultClientKey,
	vault.EnvVaultClientTimeout,
	vault.EnvVaultHeaders,
	vault.EnvVaultSRVLookup,
	vault.EnvVaultSkipVerify,
	vault.EnvVaultNamespace,
	vault.EnvVaultTLSServerName,
	vault.EnvVaultWrapTTL,
	vault.EnvVaultMaxRetries,
	vault.EnvVaultToken,
	vault.EnvVaultMFA,
	vault.EnvRateLimit,
	vault.EnvHTTPProxy,
	vault.EnvVaultProxyAddr,
	vault.EnvVaultDisableRedirects,
}

func connectTFEVaultCandidates(candidates []tfeVaultRuntimeConfig, connect tfeVaultClientConnector) (*vault.Client, tfeVaultRuntimeConfig, error) {
	attempts := make([]string, 0, len(candidates))
	for _, runtimeCfg := range candidates {
		client, err := connect(runtimeCfg)
		if err == nil {
			return client, runtimeCfg, nil
		}
		attempts = append(attempts, fmt.Sprintf("%s: %v", runtimeCfg.HostAddress, err))
	}
	return nil, tfeVaultRuntimeConfig{}, fmt.Errorf("vault unreachable at configured endpoints (%s)", strings.Join(attempts, "; "))
}

func connectTFEVaultClient(runtimeCfg tfeVaultRuntimeConfig) (*vault.Client, error) {
	client, err := newTFEVaultAPIClient(runtimeCfg)
	if err != nil {
		return nil, fmt.Errorf("vault client: %w", err)
	}
	client.SetToken(runtimeCfg.Token)

	health, err := client.Sys().Health()
	if err != nil {
		return nil, fmt.Errorf("health check: %w", err)
	}
	if !health.Initialized || health.Sealed {
		return nil, fmt.Errorf("vault is sealed or uninitialized")
	}
	return client, nil
}

// newTFEVaultAPIClient creates an explicitly managed client without inheriting
// ambient Vault CLI settings. vault.NewClient always evaluates VAULT_* values
// through DefaultConfig—even when a complete Config is supplied—so a stale
// VAULT_CACERT can otherwise prevent a dev-mode HTTP client from being created.
func newTFEVaultAPIClient(runtimeCfg tfeVaultRuntimeConfig) (*vault.Client, error) {
	tfeVaultClientEnvMu.Lock()
	defer tfeVaultClientEnvMu.Unlock()

	saved := make(map[string]tfeVaultSavedEnv, len(tfeVaultClientEnvKeys))
	for _, key := range tfeVaultClientEnvKeys {
		value, set := os.LookupEnv(key)
		saved[key] = tfeVaultSavedEnv{value: value, set: set}
		if err := os.Unsetenv(key); err != nil {
			restoreTFEVaultClientEnvironment(saved)
			return nil, fmt.Errorf("isolate %s: %w", key, err)
		}
	}
	defer restoreTFEVaultClientEnvironment(saved)

	cfg := vault.DefaultConfig()
	if cfg.Error != nil {
		return nil, cfg.Error
	}
	cfg.Address = runtimeCfg.HostAddress
	if runtimeCfg.Prod {
		if err := cfg.ConfigureTLS(&vault.TLSConfig{CACert: global.VaultProdCertPath()}); err != nil {
			return nil, fmt.Errorf("configure TLS for prod Vault: %w", err)
		}
	}
	return vault.NewClient(cfg)
}

func restoreTFEVaultClientEnvironment(saved map[string]tfeVaultSavedEnv) {
	for key, previous := range saved {
		if previous.set {
			_ = os.Setenv(key, previous.value)
		} else {
			_ = os.Unsetenv(key)
		}
	}
}

// loadTFECACert reads the self-signed primary TFE certificate used by Vault to
// validate the TFE OIDC discovery endpoint.
func loadTFECACert() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, halStateDirName, tfeCertsDirName, "cert.pem"))
	if err != nil {
		return ""
	}
	return string(data)
}

func buildTFEVaultJWTConfig(issuer, caPEM string) map[string]interface{} {
	issuer = strings.TrimRight(issuer, "/")
	config := map[string]interface{}{
		"oidc_discovery_url": issuer,
		"bound_issuer":       issuer,
	}
	if caPEM != "" {
		config["oidc_discovery_ca_pem"] = caPEM
	}
	return config
}

func buildTFEVaultJWTRole(orgName string) (map[string]interface{}, error) {
	org := strings.ToLower(strings.TrimSpace(orgName))
	if org == "" {
		return nil, fmt.Errorf("TFE organization name cannot be empty")
	}
	return map[string]interface{}{
		"role_type":         "jwt",
		"bound_audiences":   []string{defaultTFEVaultAudience},
		"bound_claims_type": "glob",
		"bound_claims": map[string]interface{}{
			"sub": fmt.Sprintf("organization:%s:project:*:workspace:*:run_phase:*", org),
		},
		"user_claim":     "terraform_full_workspace",
		"token_policies": []string{defaultTFEVaultPolicy},
		"token_ttl":      "20m",
		"token_max_ttl":  "2h",
	}, nil
}

// ensureTFEVaultJWT idempotently configures the external hal-vault container
// for TFE Dynamic Provider Credentials.
func ensureTFEVaultJWT(client *vault.Client, tfeBaseURL, orgName string) error {
	mountPath := defaultTFEVaultJWTMount

	mounts, err := client.Sys().ListAuth()
	if err != nil {
		return fmt.Errorf("list Vault auth mounts: %w", err)
	}
	if _, exists := mounts[mountPath+"/"]; !exists {
		if err := client.Sys().EnableAuthWithOptions(mountPath, &vault.MountInput{
			Type:        "jwt",
			Description: "TFE Dynamic Provider Credentials",
		}); err != nil {
			return fmt.Errorf("enable JWT auth at %s: %w", mountPath, err)
		}
	}

	issuer, err := integrations.GetTFEOIDCIssuer(tfeBaseURL)
	if err != nil {
		return fmt.Errorf("validate TFE OIDC discovery: %w", err)
	}
	if _, err = client.Logical().Write(
		"auth/"+mountPath+"/config",
		// Vault must discover from TFE's canonical issuer URL. The host-facing
		// base URL may include a published port (for example :8443), while TFE's
		// workload identity issuer and JWKS URLs intentionally use port 443.
		buildTFEVaultJWTConfig(issuer, loadTFECACert()),
	); err != nil {
		return fmt.Errorf("configure JWT auth: %w", err)
	}

	if err := client.Sys().PutPolicy(defaultTFEVaultPolicy, tfeVaultWorkspacePolicy); err != nil {
		return fmt.Errorf("write Vault policy %s: %w", defaultTFEVaultPolicy, err)
	}

	role, err := buildTFEVaultJWTRole(orgName)
	if err != nil {
		return err
	}
	if _, err = client.Logical().Write("auth/"+mountPath+"/role/"+defaultTFEVaultRole, role); err != nil {
		return fmt.Errorf("write Vault role %s: %w", defaultTFEVaultRole, err)
	}

	return nil
}
