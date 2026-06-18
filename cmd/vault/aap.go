package vault

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"hal/internal/global"

	vault "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
)

const (
	vaultAAPJWTMountPath          = "jwt-aap"
	vaultAAPDevelopmentPolicyName = "aap-development-policy"
	vaultAAPProductionPolicyName  = "aap-production-policy"
	vaultAAPDevelopmentRoleName   = "aap-development-role"
	vaultAAPProductionRoleName    = "aap-production-role"
	vaultAAPAAPExternalCredPrefix = "Vault OIDC Base Credential - "
	vaultAAPAAPCustomTypePrefix   = "Vault KV Lookup "
	vaultAAPAAPCustomTypeSuffix   = " Credential"
	vaultAAPAAPCustomCredSuffix   = " Values"
	vaultAAPAAPProjectSuffix      = " Project"
	vaultAAPAAPJobTemplateSuffix  = " KV Demo"
	vaultAAPAAPDemoInventoryName  = "Demo Inventory"
	vaultAAPAAPProjectRepoURL     = "https://github.com/chrisdola/hashicorp-ansible-playbooks"
	vaultAAPAAPProjectBranch      = "main"
	vaultAAPAAPProjectPlaybook    = "print_kv.yml"
)

var (
	vaultAAPEnable  bool
	vaultAAPDisable bool
	vaultAAPUpdate  bool

	vaultAAPOIDCDiscoveryURL string
	vaultAAPBoundAudience    string
	vaultAAPDevOrgName       string
	vaultAAPProdOrgName      string
	vaultAAPCACertFile       string
	vaultAAPAAPUsername      string
	vaultAAPAAPPassword      string
	vaultAAPAAPVaultURL      string
	vaultAAPAAPLookupTypeID  int
)

type aapEnvironmentConfig struct {
	Environment      string
	Organization     string
	RoleName         string
	SecretPath       string
	ExternalCredName string
	CustomTypeName   string
	CustomCredName   string
	ProjectName      string
	JobTemplateName  string
}

type aapAPIClient struct {
	engine   string
	baseURL  string
	username string
	password string
	client   *http.Client
}

var vaultAAPCmd = &cobra.Command{
	Use:   "aap",
	Short: "Manage AAP-related Vault integrations",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		vaultAAPOIDCCmd.Run(vaultAAPOIDCCmd, []string{"status"})
	},
}

var vaultAAPOIDCCmd = &cobra.Command{
	Use:   "oidc [status|enable|disable|update]",
	Short: "Configure Vault JWT auth for local AAP organization-scoped access",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := parseLifecycleAction(args, &vaultAAPEnable, &vaultAAPDisable, &vaultAAPUpdate); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		client, vaultErr := GetHealthyClient()

		if !vaultAAPEnable && !vaultAAPDisable && !vaultAAPUpdate {
			vaultAAPStatus(engine, client, vaultErr)
			return
		}

		if vaultAAPDisable {
			if vaultErr != nil {
				fmt.Printf("❌ Cannot disable AAP JWT integration: %v\n", vaultErr)
				return
			}

			if global.DryRun {
				fmt.Printf("[DRY RUN] Would delete AAP JWT roles/policies and disable auth/%s mount.\n", vaultAAPJWTMountPath)
				fmt.Println("[DRY RUN] Would remove AAP OIDC organizations/credentials/input sources managed by this command.")
				return
			}

			envCfgs := vaultAAPAAPEnvironmentConfigs()
			if global.IsContainerRunning(engine, "hal-aap") {
				aapClient, err := newAAPAPIClient(engine, vaultAAPAAPUsername, vaultAAPAAPPassword)
				if err != nil {
					fmt.Printf("❌ Failed to connect to AAP API for disable: %v\n", err)
					return
				}
				if err := disableAAPAAPOIDCResources(aapClient, envCfgs); err != nil {
					fmt.Printf("❌ Failed to disable AAP-side OIDC resources: %v\n", err)
					return
				}
				fmt.Println("✅ AAP-side OIDC resources disabled.")
			} else {
				fmt.Println("⚠️  AAP runtime is not running; skipping AAP-side cleanup.")
			}

			if err := disableVaultAAPJWT(client); err != nil {
				fmt.Printf("❌ Failed to disable AAP JWT integration: %v\n", err)
				return
			}

			fmt.Println("✅ Vault AAP JWT integration disabled.")
			global.RefreshHalHealth(engine)
			return
		}

		if vaultErr != nil {
			fmt.Printf("❌ Cannot configure AAP JWT integration: %v\n", vaultErr)
			return
		}

		if !global.IsContainerRunning(engine, "hal-aap") {
			fmt.Println("❌ AAP runtime is not running.")
			fmt.Println("   💡 Run 'hal aap create' first.")
			return
		}

		if global.DryRun {
			fmt.Println("[DRY RUN] Would read AAP CA cert from hal-aap container.")
			fmt.Printf("[DRY RUN] Would enable/configure Vault auth/%s mount for AAP OIDC discovery.\n", vaultAAPJWTMountPath)
			fmt.Println("[DRY RUN] Would reconcile AAP Vault policies, roles, and secret seed data.")
			fmt.Println("[DRY RUN] Would reconcile AAP organizations, external credentials, custom credential types, and input sources.")
			return
		}

		if vaultAAPUpdate {
			fmt.Println("♻️  Update requested. Reconciling Vault AAP JWT integration...")
		} else {
			fmt.Println("🚀 Enabling Vault AAP JWT integration (this can take a few minutes)...")
		}

		caPem, err := readAAPCACertFromContainer(engine, vaultAAPCACertFile)
		if err != nil {
			fmt.Printf("❌ Failed to read AAP CA cert from container: %v\n", err)
			return
		}

		if err := ensureVaultJWTMount(client); err != nil {
			fmt.Printf("❌ Failed to enable auth/%s mount: %v\n", vaultAAPJWTMountPath, err)
			return
		}

		if _, err := client.Logical().Write("auth/"+vaultAAPJWTMountPath+"/config", map[string]interface{}{
			"oidc_discovery_url":    vaultAAPOIDCDiscoveryURL,
			"oidc_discovery_ca_pem": caPem,
		}); err != nil {
			fmt.Printf("❌ Failed to configure auth/%s/config: %v\n", vaultAAPJWTMountPath, err)
			return
		}

		devPolicy := `path "secret/data/development" {
	capabilities = ["read"]
}`
		prodPolicy := `path "secret/data/production" {
	capabilities = ["read"]
}`

		if err := client.Sys().PutPolicy(vaultAAPDevelopmentPolicyName, devPolicy); err != nil {
			fmt.Printf("❌ Failed to write policy %s: %v\n", vaultAAPDevelopmentPolicyName, err)
			return
		}
		if err := client.Sys().PutPolicy(vaultAAPProductionPolicyName, prodPolicy); err != nil {
			fmt.Printf("❌ Failed to write policy %s: %v\n", vaultAAPProductionPolicyName, err)
			return
		}

		if _, err := client.Logical().Write("auth/"+vaultAAPJWTMountPath+"/role/"+vaultAAPDevelopmentRoleName, map[string]interface{}{
			"role_type":       "jwt",
			"bound_audiences": []string{vaultAAPBoundAudience},
			"user_claim":      "sub",
			"bound_claims": map[string]interface{}{
				"aap_controller_organization_name": []string{vaultAAPDevOrgName},
			},
			"token_policies": []string{vaultAAPDevelopmentPolicyName},
		}); err != nil {
			fmt.Printf("❌ Failed to write role %s: %v\n", vaultAAPDevelopmentRoleName, err)
			return
		}

		if _, err := client.Logical().Write("auth/"+vaultAAPJWTMountPath+"/role/"+vaultAAPProductionRoleName, map[string]interface{}{
			"role_type":       "jwt",
			"bound_audiences": []string{vaultAAPBoundAudience},
			"user_claim":      "sub",
			"bound_claims": map[string]interface{}{
				"aap_controller_organization_name": []string{vaultAAPProdOrgName},
			},
			"token_policies": []string{vaultAAPProductionPolicyName},
		}); err != nil {
			fmt.Printf("❌ Failed to write role %s: %v\n", vaultAAPProductionRoleName, err)
			return
		}

		if _, err := client.Logical().Write("secret/data/development", map[string]interface{}{
			"data": map[string]interface{}{"env_name": "development"},
		}); err != nil {
			fmt.Printf("❌ Failed to seed secret/data/development: %v\n", err)
			return
		}

		if _, err := client.Logical().Write("secret/data/production", map[string]interface{}{
			"data": map[string]interface{}{"env_name": "production"},
		}); err != nil {
			fmt.Printf("❌ Failed to seed secret/data/production: %v\n", err)
			return
		}

		aapClient, err := newAAPAPIClient(engine, vaultAAPAAPUsername, vaultAAPAAPPassword)
		if err != nil {
			fmt.Printf("❌ Failed to connect to AAP API: %v\n", err)
			return
		}

		envCfgs := vaultAAPAAPEnvironmentConfigs()
		if err := reconcileAAPAAPOIDCResources(aapClient, envCfgs, caPem); err != nil {
			fmt.Printf("❌ Failed to reconcile AAP-side OIDC resources: %v\n", err)
			return
		}

		fmt.Println("✅ Vault AAP JWT integration configured.")
		fmt.Println("   Roles: aap-development-role, aap-production-role")
		fmt.Println("   Policies: aap-development-policy, aap-production-policy")
		fmt.Println("   Secret paths: secret/data/development, secret/data/production")
		fmt.Println("   AAP resources: orgs, external lookup creds, custom credential types, input sources, projects, and job templates reconciled")
		global.RefreshHalHealth(engine)
	},
}

func vaultAAPStatus(engine string, client *vault.Client, vaultErr error) {
	fmt.Println("🔍 Checking Vault AAP JWT integration status...")

	aapRunning := global.IsContainerRunning(engine, "hal-aap")
	if aapRunning {
		fmt.Println("  ✅ AAP runtime     : running (hal-aap)")
	} else {
		fmt.Println("  ❌ AAP runtime     : not running")
	}

	if vaultErr != nil {
		fmt.Printf("  ❌ Vault API       : unavailable (%v)\n", vaultErr)
		fmt.Println("\n💡 Next Step:")
		fmt.Println("   hal vault create")
		return
	}

	auths, _ := client.Sys().ListAuth()
	_, jwtMounted := auths[vaultAAPJWTMountPath+"/"]
	if jwtMounted {
		fmt.Printf("  ✅ JWT auth mount  : enabled (auth/%s)\n", vaultAAPJWTMountPath)
	} else {
		fmt.Println("  ❌ JWT auth mount  : not enabled")
	}

	devRole := vaultPathExists(client, "auth/"+vaultAAPJWTMountPath+"/role/"+vaultAAPDevelopmentRoleName)
	prodRole := vaultPathExists(client, "auth/"+vaultAAPJWTMountPath+"/role/"+vaultAAPProductionRoleName)
	devSecret := vaultPathExists(client, "secret/data/development")
	prodSecret := vaultPathExists(client, "secret/data/production")
	devPolicy := vaultPolicyExists(client, vaultAAPDevelopmentPolicyName)
	prodPolicy := vaultPolicyExists(client, vaultAAPProductionPolicyName)

	statusIcon := func(ok bool) string {
		if ok {
			return "✅"
		}
		return "❌"
	}

	fmt.Printf("  %s Dev role        : %s\n", statusIcon(devRole), vaultAAPDevelopmentRoleName)
	fmt.Printf("  %s Prod role       : %s\n", statusIcon(prodRole), vaultAAPProductionRoleName)
	fmt.Printf("  %s Dev policy      : %s\n", statusIcon(devPolicy), vaultAAPDevelopmentPolicyName)
	fmt.Printf("  %s Prod policy     : %s\n", statusIcon(prodPolicy), vaultAAPProductionPolicyName)
	fmt.Printf("  %s Dev secret      : secret/data/development\n", statusIcon(devSecret))
	fmt.Printf("  %s Prod secret     : secret/data/production\n", statusIcon(prodSecret))

	fullyReady := aapRunning && jwtMounted && devRole && prodRole && devPolicy && prodPolicy && devSecret && prodSecret

	if aapRunning {
		envCfgs := vaultAAPAAPEnvironmentConfigs()
		aapClient, err := newAAPAPIClient(engine, vaultAAPAAPUsername, vaultAAPAAPPassword)
		if err != nil {
			fmt.Printf("  ⚠️  AAP API         : unavailable (%v)\n", err)
		} else {
			aapReady, statusErr := aapOIDCResourcesReady(aapClient, envCfgs)
			if statusErr != nil {
				fmt.Printf("  ⚠️  AAP resources   : unknown (%v)\n", statusErr)
			} else {
				if aapReady {
					fmt.Println("  ✅ AAP resources   : reconciled")
				} else {
					fmt.Println("  ❌ AAP resources   : not fully reconciled")
					fullyReady = false
				}
			}
		}
	}

	fmt.Println("\n💡 Next Step:")
	if fullyReady {
		fmt.Println("   Integration is ready. Reconcile at any time with: hal vault aap oidc update")
		return
	}
	fmt.Println("   hal vault aap oidc enable")
}

func ensureVaultJWTMount(client *vault.Client) error {
	auths, err := client.Sys().ListAuth()
	if err == nil {
		if _, exists := auths[vaultAAPJWTMountPath+"/"]; exists {
			return nil
		}
	}

	return client.Sys().EnableAuthWithOptions(vaultAAPJWTMountPath, &vault.EnableAuthOptions{Type: "jwt"})
}

func disableVaultAAPJWT(client *vault.Client) error {
	_, _ = client.Logical().Delete("auth/" + vaultAAPJWTMountPath + "/role/" + vaultAAPDevelopmentRoleName)
	_, _ = client.Logical().Delete("auth/" + vaultAAPJWTMountPath + "/role/" + vaultAAPProductionRoleName)
	_ = client.Sys().DeletePolicy(vaultAAPDevelopmentPolicyName)
	_ = client.Sys().DeletePolicy(vaultAAPProductionPolicyName)
	_, _ = client.Logical().Delete("secret/metadata/development")
	_, _ = client.Logical().Delete("secret/metadata/production")

	if err := client.Sys().DisableAuth(vaultAAPJWTMountPath); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no handler for route") {
			return nil
		}
		return err
	}

	return nil
}

func readAAPCACertFromContainer(engine, containerPath string) (string, error) {
	out, err := exec.Command(engine, "exec", "hal-aap", "cat", containerPath).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("exec failed: %s", strings.TrimSpace(string(out)))
	}
	cert := strings.TrimSpace(string(out))
	if cert == "" {
		return "", fmt.Errorf("empty certificate content at %s", containerPath)
	}
	return cert, nil
}

func vaultPathExists(client *vault.Client, path string) bool {
	secret, err := client.Logical().Read(path)
	if err != nil {
		return false
	}
	return secret != nil
}

func vaultPolicyExists(client *vault.Client, policyName string) bool {
	policy, err := client.Sys().GetPolicy(policyName)
	if err != nil {
		return false
	}
	return strings.TrimSpace(policy) != ""
}

func init() {
	vaultAAPOIDCCmd.Flags().StringVar(&vaultAAPOIDCDiscoveryURL, "oidc-discovery-url", "https://hal-aap/o", "AAP OIDC discovery URL used by Vault JWT auth config")
	vaultAAPOIDCCmd.Flags().StringVar(&vaultAAPBoundAudience, "bound-audience", "http://hal-vault:8200", "JWT audience expected by Vault AAP roles")
	vaultAAPOIDCCmd.Flags().StringVar(&vaultAAPDevOrgName, "development-org", "Development", "AAP organization mapped to development Vault role")
	vaultAAPOIDCCmd.Flags().StringVar(&vaultAAPProdOrgName, "production-org", "Production", "AAP organization mapped to production Vault role")
	vaultAAPOIDCCmd.Flags().StringVar(&vaultAAPCACertFile, "aap-ca-cert-file", "/home/aap/aap/tls/ca.cert", "Path to the AAP CA certificate inside hal-aap container")
	vaultAAPOIDCCmd.Flags().StringVar(&vaultAAPAAPUsername, "aap-username", "admin", "AAP controller username")
	vaultAAPOIDCCmd.Flags().StringVar(&vaultAAPAAPPassword, "aap-password", "admin", "AAP controller password")
	vaultAAPOIDCCmd.Flags().StringVar(&vaultAAPAAPVaultURL, "aap-vault-url", "http://hal-vault:8200", "Vault URL used by AAP external secret lookup credentials")
	vaultAAPOIDCCmd.Flags().IntVar(&vaultAAPAAPLookupTypeID, "aap-vault-lookup-type-id", 29, "AAP credential type ID for HashiCorp Vault Secret Lookup (OIDC)")

	vaultAAPCmd.AddCommand(vaultAAPOIDCCmd)
	Cmd.AddCommand(vaultAAPCmd)
}

func vaultAAPAAPEnvironmentConfigs() []aapEnvironmentConfig {
	devLabel := strings.Title(strings.ToLower(vaultAAPDevOrgName))
	prodLabel := strings.Title(strings.ToLower(vaultAAPProdOrgName))

	return []aapEnvironmentConfig{
		{
			Environment:      "development",
			Organization:     vaultAAPDevOrgName,
			RoleName:         vaultAAPDevelopmentRoleName,
			SecretPath:       "/development",
			ExternalCredName: vaultAAPAAPExternalCredPrefix + devLabel,
			CustomTypeName:   vaultAAPAAPCustomTypePrefix + devLabel + vaultAAPAAPCustomTypeSuffix,
			CustomCredName:   vaultAAPAAPCustomTypePrefix + devLabel + vaultAAPAAPCustomTypeSuffix + vaultAAPAAPCustomCredSuffix,
			ProjectName:      devLabel + vaultAAPAAPProjectSuffix,
			JobTemplateName:  devLabel + vaultAAPAAPJobTemplateSuffix,
		},
		{
			Environment:      "production",
			Organization:     vaultAAPProdOrgName,
			RoleName:         vaultAAPProductionRoleName,
			SecretPath:       "/production",
			ExternalCredName: vaultAAPAAPExternalCredPrefix + prodLabel,
			CustomTypeName:   vaultAAPAAPCustomTypePrefix + prodLabel + vaultAAPAAPCustomTypeSuffix,
			CustomCredName:   vaultAAPAAPCustomTypePrefix + prodLabel + vaultAAPAAPCustomTypeSuffix + vaultAAPAAPCustomCredSuffix,
			ProjectName:      prodLabel + vaultAAPAAPProjectSuffix,
			JobTemplateName:  prodLabel + vaultAAPAAPJobTemplateSuffix,
		},
	}
}

func newAAPAPIClient(engine, username, password string) (*aapAPIClient, error) {
	base, err := detectAAPAPIBaseURL(engine)
	if err != nil {
		return nil, err
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	transport.Proxy = nil

	return &aapAPIClient{
		engine:   engine,
		baseURL:  base,
		username: username,
		password: password,
		client: &http.Client{
			Timeout:   20 * time.Second,
			Transport: transport,
		},
	}, nil
}

func detectAAPAPIBaseURL(engine string) (string, error) {
	portOut, err := exec.Command(
		engine,
		"inspect",
		"-f",
		"{{(index (index .NetworkSettings.Ports \"443/tcp\") 0).HostPort}}",
		"hal-aap",
	).Output()
	if err != nil {
		return "", fmt.Errorf("failed to inspect hal-aap port mapping: %w", err)
	}

	hostPort := strings.TrimSpace(string(portOut))
	hostBase := "https://127.0.0.1"
	if hostPort != "" && hostPort != "<no value>" && hostPort != "443" {
		hostBase = fmt.Sprintf("https://127.0.0.1:%s", hostPort)
	}

	candidates := []string{
		hostBase + "/api/controller/v2",
		hostBase + "/api/v2",
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	transport.Proxy = nil
	client := &http.Client{Timeout: 10 * time.Second, Transport: transport}

	for _, base := range candidates {
		resp, reqErr := client.Get(base + "/ping/")
		if reqErr != nil {
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized {
			return base, nil
		}
	}

	return "", fmt.Errorf("unable to find a reachable AAP API base URL")
}

func (c *aapAPIClient) doJSON(method, path string, query url.Values, payload interface{}) (map[string]interface{}, int, error) {
	fullURL := strings.TrimRight(c.baseURL, "/") + "/" + strings.TrimLeft(path, "/")
	if len(query) > 0 {
		fullURL = fullURL + "?" + query.Encode()
	}

	var body io.Reader
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, err
		}
		body = bytes.NewBuffer(buf)
	}

	req, err := http.NewRequest(method, fullURL, body)
	if err != nil {
		return nil, 0, err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	var data map[string]interface{}
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &data); err != nil {
			if resp.StatusCode >= 400 {
				return nil, resp.StatusCode, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
			}
			return nil, resp.StatusCode, nil
		}
	}

	if resp.StatusCode >= 400 {
		detail := strings.TrimSpace(string(bodyBytes))
		if data != nil {
			if v, ok := data["detail"]; ok {
				detail = fmt.Sprintf("%v", v)
			}
			if v, ok := data["error"]; ok {
				detail = fmt.Sprintf("%v", v)
			}
		}
		return data, resp.StatusCode, fmt.Errorf("http %d: %s", resp.StatusCode, detail)
	}

	return data, resp.StatusCode, nil
}

func aapResults(body map[string]interface{}) []map[string]interface{} {
	if body == nil {
		return nil
	}
	raw, ok := body["results"].([]interface{})
	if !ok {
		return nil
	}
	results := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]interface{}); ok {
			results = append(results, m)
		}
	}
	return results
}

func aapMapString(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

func aapMapInt(m map[string]interface{}, key string) int {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	s := fmt.Sprintf("%v", v)
	n, _ := strconv.Atoi(strings.Split(s, ".")[0])
	return n
}

func (c *aapAPIClient) findOrganization(name string) (map[string]interface{}, error) {
	body, _, err := c.doJSON(http.MethodGet, "/organizations/", url.Values{"name": []string{name}}, nil)
	if err != nil {
		return nil, err
	}
	for _, item := range aapResults(body) {
		if aapMapString(item, "name") == name {
			return item, nil
		}
	}
	return nil, nil
}

func (c *aapAPIClient) ensureOrganization(name string) (map[string]interface{}, error) {
	existing, err := c.findOrganization(name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	body, _, err := c.doJSON(http.MethodPost, "/organizations/", nil, map[string]interface{}{"name": name})
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (c *aapAPIClient) findCredentialTypeByName(name string) (map[string]interface{}, error) {
	script := fmt.Sprintf(
		"import json; name=%q; ct=CredentialType.objects.filter(name=name).values('id','name','kind','namespace','inputs','injectors').first(); print('__HAL_JSON__'+json.dumps(ct))",
		name,
	)

	var ct map[string]interface{}
	if err := c.runAWXManageJSON(script, &ct); err != nil {
		return nil, err
	}
	if len(ct) == 0 {
		return nil, nil
	}
	return ct, nil
}

func (c *aapAPIClient) getCredentialTypeByID(id int) (map[string]interface{}, error) {
	if id <= 0 {
		return nil, nil
	}
	script := fmt.Sprintf(
		"import json; ct=CredentialType.objects.filter(id=%d).values('id','name','kind','namespace','inputs','injectors').first(); print('__HAL_JSON__'+json.dumps(ct))",
		id,
	)

	var ct map[string]interface{}
	if err := c.runAWXManageJSON(script, &ct); err != nil {
		return nil, err
	}
	if len(ct) == 0 {
		return nil, nil
	}
	return ct, nil
}

func (c *aapAPIClient) findCredentialTypeForVaultOIDC() (map[string]interface{}, error) {
	if vaultAAPAAPLookupTypeID > 0 {
		ct, err := c.getCredentialTypeByID(vaultAAPAAPLookupTypeID)
		if err != nil {
			return nil, err
		}
		if ct != nil {
			return ct, nil
		}
	}

	preferredNames := []string{
		"HashiCorp Vault Secret Lookup (OIDC)",
		"HashiCorp Vault KV (OIDC)",
		"HashiCorp Vault Secret Lookup",
	}
	for _, name := range preferredNames {
		ct, err := c.findCredentialTypeByName(name)
		if err != nil {
			return nil, err
		}
		if ct != nil {
			return ct, nil
		}
	}

	script := "import json; rows=list(CredentialType.objects.values('id','name','kind','namespace','inputs','injectors')); print('__HAL_JSON__'+json.dumps(rows))"
	rows := []map[string]interface{}{}
	if err := c.runAWXManageJSON(script, &rows); err != nil {
		return nil, err
	}

	for _, item := range rows {
		ns := strings.ToLower(aapMapString(item, "namespace"))
		name := strings.ToLower(aapMapString(item, "name"))
		if ns == "hashivault-kv-oidc" || strings.Contains(name, "vault") && strings.Contains(name, "oidc") {
			return item, nil
		}
	}
	for _, item := range rows {
		ns := strings.ToLower(aapMapString(item, "namespace"))
		if ns == "hashivault-kv" {
			return item, nil
		}
	}

	return nil, fmt.Errorf("could not locate HashiCorp Vault external credential type in AAP")
}

func (c *aapAPIClient) findCredential(name string, orgID, credTypeID int) (map[string]interface{}, error) {
	q := url.Values{"name": []string{name}}
	if orgID > 0 {
		q.Set("organization", strconv.Itoa(orgID))
	}
	if credTypeID > 0 {
		q.Set("credential_type", strconv.Itoa(credTypeID))
	}
	body, _, err := c.doJSON(http.MethodGet, "/credentials/", q, nil)
	if err != nil {
		return nil, err
	}
	for _, item := range aapResults(body) {
		if aapMapString(item, "name") == name {
			return item, nil
		}
	}
	return nil, nil
}

func (c *aapAPIClient) findInventory(name string) (map[string]interface{}, error) {
	body, _, err := c.doJSON(http.MethodGet, "/inventories/", url.Values{"name": []string{name}}, nil)
	if err != nil {
		return nil, err
	}
	for _, item := range aapResults(body) {
		if aapMapString(item, "name") == name {
			return item, nil
		}
	}
	return nil, nil
}

func (c *aapAPIClient) findProject(name string, orgID int) (map[string]interface{}, error) {
	q := url.Values{"name": []string{name}}
	if orgID > 0 {
		q.Set("organization", strconv.Itoa(orgID))
	}
	body, _, err := c.doJSON(http.MethodGet, "/projects/", q, nil)
	if err != nil {
		return nil, err
	}
	for _, item := range aapResults(body) {
		if aapMapString(item, "name") == name {
			return item, nil
		}
	}
	return nil, nil
}

func (c *aapAPIClient) ensureProject(name string, orgID int) (map[string]interface{}, error) {
	payload := map[string]interface{}{
		"name":                 name,
		"organization":         orgID,
		"scm_type":             "git",
		"scm_url":              vaultAAPAAPProjectRepoURL,
		"scm_branch":           vaultAAPAAPProjectBranch,
		"scm_update_on_launch": true,
	}

	existing, err := c.findProject(name, orgID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		body, _, err := c.doJSON(http.MethodPost, "/projects/", nil, payload)
		if err != nil {
			return nil, err
		}
		return body, nil
	}

	id := aapMapInt(existing, "id")
	body, _, err := c.doJSON(http.MethodPatch, fmt.Sprintf("/projects/%d/", id), nil, payload)
	if err != nil {
		return nil, err
	}
	if body == nil {
		return existing, nil
	}
	return body, nil
}

func (c *aapAPIClient) updateProject(projectID int) error {
	if projectID <= 0 {
		return fmt.Errorf("project id is required")
	}
	body, status, err := c.doJSON(http.MethodPost, fmt.Sprintf("/projects/%d/update/", projectID), nil, map[string]interface{}{})
	if err != nil {
		// If an update is already running, allow the existing sync to settle.
		if status == http.StatusBadRequest && strings.Contains(strings.ToLower(err.Error()), "already") {
			return c.waitForProjectToSettle(projectID, 90*time.Second)
		}
		return err
	}
	if body == nil {
		return c.waitForProjectToSettle(projectID, 90*time.Second)
	}
	updateID := aapMapInt(body, "project_update")
	if updateID == 0 {
		updateID = aapMapInt(body, "id")
	}
	if updateID == 0 {
		return c.waitForProjectToSettle(projectID, 90*time.Second)
	}
	return c.waitForProjectUpdate(updateID, 90*time.Second)
}

func (c *aapAPIClient) waitForProjectToSettle(projectID int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		body, _, err := c.doJSON(http.MethodGet, fmt.Sprintf("/projects/%d/", projectID), nil, nil)
		if err != nil {
			return err
		}
		status := strings.ToLower(aapMapString(body, "status"))
		switch status {
		case "successful", "never updated":
			return nil
		case "failed", "error", "canceled":
			return fmt.Errorf("project sync ended with status %s", status)
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout waiting for project %d to settle", projectID)
}

func (c *aapAPIClient) waitForProjectUpdate(updateID int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		body, _, err := c.doJSON(http.MethodGet, fmt.Sprintf("/project_updates/%d/", updateID), nil, nil)
		if err != nil {
			return err
		}
		status := strings.ToLower(aapMapString(body, "status"))
		switch status {
		case "successful":
			return nil
		case "failed", "error", "canceled":
			return fmt.Errorf("project update ended with status %s", status)
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout waiting for project update %d", updateID)
}

func (c *aapAPIClient) waitForProjectPlaybook(projectID int, playbook string, timeout time.Duration) error {
	if projectID <= 0 {
		return fmt.Errorf("project id is required")
	}
	if strings.TrimSpace(playbook) == "" {
		return fmt.Errorf("playbook is required")
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		playbooks, err := c.projectPlaybooks(projectID)
		if err != nil {
			return err
		}

		for _, pb := range playbooks {
			if pb == playbook {
				return nil
			}
		}

		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("timeout waiting for playbook %q in project %d", playbook, projectID)
}

func (c *aapAPIClient) projectPlaybooks(projectID int) ([]string, error) {
	fullURL := strings.TrimRight(c.baseURL, "/") + fmt.Sprintf("/projects/%d/playbooks/", projectID)
	req, err := http.NewRequest(http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	var list []string
	if err := json.Unmarshal(bodyBytes, &list); err == nil {
		return list, nil
	}

	var wrapper map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &wrapper); err == nil {
		results := aapResults(wrapper)
		out := make([]string, 0, len(results))
		for _, r := range results {
			name := aapMapString(r, "name")
			if name != "" {
				out = append(out, name)
			}
		}
		return out, nil
	}

	return nil, fmt.Errorf("unexpected project playbooks response")
}

func (c *aapAPIClient) deleteProject(name string, orgID int) error {
	project, err := c.findProject(name, orgID)
	if err != nil {
		return err
	}
	if project == nil {
		return nil
	}
	id := aapMapInt(project, "id")
	_, _, err = c.doJSON(http.MethodDelete, fmt.Sprintf("/projects/%d/", id), nil, nil)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "404") {
		return err
	}
	return nil
}

func (c *aapAPIClient) findJobTemplate(name string, orgID int) (map[string]interface{}, error) {
	q := url.Values{"name": []string{name}}
	if orgID > 0 {
		q.Set("organization", strconv.Itoa(orgID))
	}
	body, _, err := c.doJSON(http.MethodGet, "/job_templates/", q, nil)
	if err != nil {
		return nil, err
	}
	for _, item := range aapResults(body) {
		if aapMapString(item, "name") == name {
			return item, nil
		}
	}
	return nil, nil
}

func (c *aapAPIClient) ensureJobTemplate(name string, orgID, inventoryID, projectID int, extraVars map[string]interface{}) (map[string]interface{}, error) {
	extraVarsJSON, err := json.Marshal(extraVars)
	if err != nil {
		return nil, err
	}

	payload := map[string]interface{}{
		"name":         name,
		"job_type":     "run",
		"inventory":    inventoryID,
		"project":      projectID,
		"playbook":     vaultAAPAAPProjectPlaybook,
		"organization": orgID,
		"extra_vars":   string(extraVarsJSON),
	}

	existing, err := c.findJobTemplate(name, orgID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		body, _, err := c.doJSON(http.MethodPost, "/job_templates/", nil, payload)
		if err != nil {
			return nil, err
		}
		return body, nil
	}

	id := aapMapInt(existing, "id")
	body, _, err := c.doJSON(http.MethodPatch, fmt.Sprintf("/job_templates/%d/", id), nil, payload)
	if err != nil {
		return nil, err
	}
	if body == nil {
		return existing, nil
	}
	return body, nil
}

func (c *aapAPIClient) ensureJobTemplateCredential(jobTemplateID, credentialID int) error {
	if jobTemplateID <= 0 || credentialID <= 0 {
		return fmt.Errorf("job template id and credential id are required")
	}
	hasCredential, err := c.jobTemplateHasCredential(jobTemplateID, credentialID)
	if err != nil {
		return err
	}
	if hasCredential {
		return nil
	}
	_, _, err = c.doJSON(http.MethodPost, fmt.Sprintf("/job_templates/%d/credentials/", jobTemplateID), nil, map[string]interface{}{"id": credentialID})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "already") {
			return nil
		}
		return err
	}
	return nil
}

func (c *aapAPIClient) deleteJobTemplate(name string, orgID int) error {
	jt, err := c.findJobTemplate(name, orgID)
	if err != nil {
		return err
	}
	if jt == nil {
		return nil
	}
	id := aapMapInt(jt, "id")
	_, _, err = c.doJSON(http.MethodDelete, fmt.Sprintf("/job_templates/%d/", id), nil, nil)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "404") {
		return err
	}
	return nil
}

func (c *aapAPIClient) ensureCredential(name string, orgID, credTypeID int, inputs map[string]interface{}) (map[string]interface{}, error) {
	existing, err := c.findCredential(name, orgID, credTypeID)
	if err != nil {
		return nil, err
	}

	if existing == nil {
		payload := map[string]interface{}{
			"name":            name,
			"organization":    orgID,
			"credential_type": credTypeID,
			"inputs":          inputs,
		}
		body, _, err := c.doJSON(http.MethodPost, "/credentials/", nil, payload)
		if err != nil {
			return nil, err
		}
		return body, nil
	}

	id := aapMapInt(existing, "id")
	body, _, err := c.doJSON(http.MethodPatch, fmt.Sprintf("/credentials/%d/", id), nil, map[string]interface{}{
		"name":            name,
		"organization":    orgID,
		"credential_type": credTypeID,
		"inputs":          inputs,
	})
	if err != nil {
		return nil, err
	}
	if body == nil {
		return existing, nil
	}
	return body, nil
}

func (c *aapAPIClient) ensureCustomCredentialType(name string) (map[string]interface{}, error) {
	inputs := map[string]interface{}{
		"fields": []map[string]interface{}{
			{"id": "env_name", "label": "Environment Name", "type": "string"},
		},
		"required": []string{"env_name"},
	}
	injectors := map[string]interface{}{
		"extra_vars": map[string]interface{}{"env_name": "{{ env_name }}"},
	}

	inputsJSON, err := json.Marshal(inputs)
	if err != nil {
		return nil, err
	}
	injectorsJSON, err := json.Marshal(injectors)
	if err != nil {
		return nil, err
	}

	script := fmt.Sprintf(
		"import json; name=%q; inputs=json.loads(%q); injectors=json.loads(%q); ct,_=CredentialType.objects.get_or_create(name=name, defaults={'kind':'cloud','inputs':inputs,'injectors':injectors}); ct.kind='cloud'; ct.inputs=inputs; ct.injectors=injectors; ct.save(); out={'id':ct.id,'name':ct.name,'kind':ct.kind,'namespace':ct.namespace,'inputs':ct.inputs,'injectors':ct.injectors}; print('__HAL_JSON__'+json.dumps(out))",
		name,
		string(inputsJSON),
		string(injectorsJSON),
	)

	var ct map[string]interface{}
	if err := c.runAWXManageJSON(script, &ct); err != nil {
		return nil, err
	}
	if len(ct) == 0 {
		return nil, fmt.Errorf("failed to create or update credential type %q", name)
	}
	return ct, nil
}

func (c *aapAPIClient) findCredentialInputSource(targetCredID int, inputFieldName string) (map[string]interface{}, error) {
	body, _, err := c.doJSON(http.MethodGet, "/credential_input_sources/", url.Values{
		"target_credential": []string{strconv.Itoa(targetCredID)},
		"input_field_name":  []string{inputFieldName},
	}, nil)
	if err != nil {
		return nil, err
	}
	results := aapResults(body)
	if len(results) == 0 {
		return nil, nil
	}
	return results[0], nil
}

func (c *aapAPIClient) ensureCredentialInputSource(targetCredID, sourceCredID int, inputFieldName string, metadata map[string]interface{}) error {
	existing, err := c.findCredentialInputSource(targetCredID, inputFieldName)
	if err != nil {
		return err
	}

	payload := map[string]interface{}{
		"target_credential": targetCredID,
		"source_credential": sourceCredID,
		"input_field_name":  inputFieldName,
		"metadata":          metadata,
	}

	if existing == nil {
		_, _, err := c.doJSON(http.MethodPost, "/credential_input_sources/", nil, payload)
		return err
	}

	id := aapMapInt(existing, "id")
	_, _, err = c.doJSON(http.MethodPatch, fmt.Sprintf("/credential_input_sources/%d/", id), nil, payload)
	return err
}

func (c *aapAPIClient) deleteCredentialInputSource(targetCredID int, inputFieldName string) error {
	existing, err := c.findCredentialInputSource(targetCredID, inputFieldName)
	if err != nil {
		return err
	}
	if existing == nil {
		return nil
	}
	id := aapMapInt(existing, "id")
	_, _, err = c.doJSON(http.MethodDelete, fmt.Sprintf("/credential_input_sources/%d/", id), nil, nil)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "404") {
		return err
	}
	return nil
}

func (c *aapAPIClient) deleteCredential(name string, orgID, credTypeID int) error {
	cred, err := c.findCredential(name, orgID, credTypeID)
	if err != nil {
		return err
	}
	if cred == nil {
		return nil
	}
	id := aapMapInt(cred, "id")
	_, _, err = c.doJSON(http.MethodDelete, fmt.Sprintf("/credentials/%d/", id), nil, nil)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "404") {
		return err
	}
	return nil
}

func (c *aapAPIClient) deleteCredentialTypeByName(name string) error {
	script := fmt.Sprintf(
		"import json; name=%q; deleted,_=CredentialType.objects.filter(name=name).delete(); print('__HAL_JSON__'+json.dumps({'deleted':deleted}))",
		name,
	)

	var result map[string]interface{}
	if err := c.runAWXManageJSON(script, &result); err != nil {
		return err
	}
	return nil
}

func (c *aapAPIClient) runAWXManageJSON(script string, out interface{}) error {
	const jsonPrefix = "__HAL_JSON__"

	manageCommand := fmt.Sprintf(
		"XDG_RUNTIME_DIR=/run/user/$(id -u) podman exec automation-controller-web /usr/bin/awx-manage shell_plus --quiet-load -c %q",
		script,
	)

	cmd := exec.Command(c.engine, "exec", "-u", "aap", "hal-aap", "bash", "-lc", manageCommand)
	rawOut, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("awx-manage shell_plus failed: %s", strings.TrimSpace(string(rawOut)))
	}

	lines := strings.Split(strings.ReplaceAll(string(rawOut), "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, jsonPrefix) {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, jsonPrefix))
		if payload == "" || payload == "null" {
			return nil
		}
		if err := json.Unmarshal([]byte(payload), out); err != nil {
			return fmt.Errorf("failed to parse awx-manage JSON output: %w", err)
		}
		return nil
	}

	return fmt.Errorf("awx-manage output did not include JSON payload: %s", strings.TrimSpace(string(rawOut)))
}

func reconcileAAPAAPOIDCResources(c *aapAPIClient, envCfgs []aapEnvironmentConfig, caPem string) error {
	lookupType, err := c.findCredentialTypeForVaultOIDC()
	if err != nil {
		return err
	}
	lookupTypeID := aapMapInt(lookupType, "id")

	demoInventory, err := c.findInventory(vaultAAPAAPDemoInventoryName)
	if err != nil {
		return err
	}
	if demoInventory == nil {
		return fmt.Errorf("could not locate %s inventory in AAP", vaultAAPAAPDemoInventoryName)
	}
	demoInventoryID := aapMapInt(demoInventory, "id")

	for _, cfg := range envCfgs {
		org, err := c.ensureOrganization(cfg.Organization)
		if err != nil {
			return fmt.Errorf("org %s: %w", cfg.Organization, err)
		}
		orgID := aapMapInt(org, "id")

		externalInputs, err := buildAAPVaultLookupInputs(lookupType, cfg, caPem)
		if err != nil {
			return fmt.Errorf("external credential inputs for %s: %w", cfg.Environment, err)
		}

		externalCred, err := c.ensureCredential(cfg.ExternalCredName, orgID, lookupTypeID, externalInputs)
		if err != nil {
			return fmt.Errorf("external credential %s: %w", cfg.ExternalCredName, err)
		}
		externalCredID := aapMapInt(externalCred, "id")

		customType, err := c.ensureCustomCredentialType(cfg.CustomTypeName)
		if err != nil {
			return fmt.Errorf("custom credential type %s: %w", cfg.CustomTypeName, err)
		}
		customTypeID := aapMapInt(customType, "id")

		targetCred, err := c.ensureCredential(cfg.CustomCredName, orgID, customTypeID, map[string]interface{}{})
		if err != nil {
			return fmt.Errorf("target credential %s: %w", cfg.CustomCredName, err)
		}
		targetCredID := aapMapInt(targetCred, "id")

		project, err := c.ensureProject(cfg.ProjectName, orgID)
		if err != nil {
			return fmt.Errorf("project %s: %w", cfg.ProjectName, err)
		}
		projectID := aapMapInt(project, "id")
		if err := c.updateProject(projectID); err != nil {
			return fmt.Errorf("project sync %s: %w", cfg.ProjectName, err)
		}
		if err := c.waitForProjectPlaybook(projectID, vaultAAPAAPProjectPlaybook, 90*time.Second); err != nil {
			return fmt.Errorf("project playbooks %s: %w", cfg.ProjectName, err)
		}

		jobTemplate, err := c.ensureJobTemplate(cfg.JobTemplateName, orgID, demoInventoryID, projectID, map[string]interface{}{
			"var_names": []string{"env_name"},
		})
		if err != nil {
			return fmt.Errorf("job template %s: %w", cfg.JobTemplateName, err)
		}
		jobTemplateID := aapMapInt(jobTemplate, "id")
		if err := c.ensureJobTemplateCredential(jobTemplateID, targetCredID); err != nil {
			return fmt.Errorf("job template credential %s: %w", cfg.JobTemplateName, err)
		}

		metadata, err := buildAAPVaultLookupMetadata(lookupType, cfg)
		if err != nil {
			return fmt.Errorf("input source metadata for %s: %w", cfg.Environment, err)
		}

		if err := c.ensureCredentialInputSource(targetCredID, externalCredID, "env_name", metadata); err != nil {
			return fmt.Errorf("input source for %s: %w", cfg.Environment, err)
		}
	}

	return nil
}

func disableAAPAAPOIDCResources(c *aapAPIClient, envCfgs []aapEnvironmentConfig) error {
	lookupType, err := c.findCredentialTypeForVaultOIDC()
	if err != nil {
		lookupType = nil
	}
	lookupTypeID := 0
	if lookupType != nil {
		lookupTypeID = aapMapInt(lookupType, "id")
	}

	for _, cfg := range envCfgs {
		org, err := c.findOrganization(cfg.Organization)
		if err != nil {
			return err
		}
		if org == nil {
			continue
		}
		orgID := aapMapInt(org, "id")

		if err := c.deleteJobTemplate(cfg.JobTemplateName, orgID); err != nil {
			return err
		}
		if err := c.deleteProject(cfg.ProjectName, orgID); err != nil {
			return err
		}

		customType, err := c.findCredentialTypeByName(cfg.CustomTypeName)
		if err != nil {
			return err
		}
		customTypeID := 0
		if customType != nil {
			customTypeID = aapMapInt(customType, "id")
		}

		targetCred, err := c.findCredential(cfg.CustomCredName, orgID, customTypeID)
		if err != nil {
			return err
		}
		if targetCred != nil {
			targetCredID := aapMapInt(targetCred, "id")
			if err := c.deleteCredentialInputSource(targetCredID, "env_name"); err != nil {
				return err
			}
		}

		if err := c.deleteCredential(cfg.CustomCredName, orgID, customTypeID); err != nil {
			return err
		}
		if err := c.deleteCredential(cfg.ExternalCredName, orgID, lookupTypeID); err != nil {
			return err
		}
		if err := c.deleteCredentialTypeByName(cfg.CustomTypeName); err != nil {
			return err
		}
	}

	return nil
}

func aapOIDCResourcesReady(c *aapAPIClient, envCfgs []aapEnvironmentConfig) (bool, error) {
	lookupType, err := c.findCredentialTypeForVaultOIDC()
	if err != nil {
		return false, nil
	}
	lookupTypeID := aapMapInt(lookupType, "id")

	for _, cfg := range envCfgs {
		org, err := c.findOrganization(cfg.Organization)
		if err != nil {
			return false, err
		}
		if org == nil {
			return false, nil
		}
		orgID := aapMapInt(org, "id")

		externalCred, err := c.findCredential(cfg.ExternalCredName, orgID, lookupTypeID)
		if err != nil {
			return false, err
		}
		if externalCred == nil {
			return false, nil
		}

		customType, err := c.findCredentialTypeByName(cfg.CustomTypeName)
		if err != nil {
			return false, err
		}
		if customType == nil {
			return false, nil
		}

		customTypeID := aapMapInt(customType, "id")
		targetCred, err := c.findCredential(cfg.CustomCredName, orgID, customTypeID)
		if err != nil {
			return false, err
		}
		if targetCred == nil {
			return false, nil
		}

		targetCredID := aapMapInt(targetCred, "id")
		inputSource, err := c.findCredentialInputSource(targetCredID, "env_name")
		if err != nil {
			return false, err
		}
		if inputSource == nil {
			return false, nil
		}

		project, err := c.findProject(cfg.ProjectName, orgID)
		if err != nil {
			return false, err
		}
		if project == nil {
			return false, nil
		}
		projectID := aapMapInt(project, "id")

		jobTemplate, err := c.findJobTemplate(cfg.JobTemplateName, orgID)
		if err != nil {
			return false, err
		}
		if jobTemplate == nil {
			return false, nil
		}
		if aapMapInt(jobTemplate, "project") != projectID {
			return false, nil
		}
		jobTemplateID := aapMapInt(jobTemplate, "id")
		hasCredential, err := c.jobTemplateHasCredential(jobTemplateID, targetCredID)
		if err != nil {
			return false, err
		}
		if !hasCredential {
			return false, nil
		}
	}

	return true, nil
}

func (c *aapAPIClient) jobTemplateHasCredential(jobTemplateID, credentialID int) (bool, error) {
	body, _, err := c.doJSON(http.MethodGet, fmt.Sprintf("/job_templates/%d/credentials/", jobTemplateID), nil, nil)
	if err != nil {
		return false, err
	}
	for _, item := range aapResults(body) {
		if aapMapInt(item, "id") == credentialID {
			return true, nil
		}
	}
	return false, nil
}

func buildAAPVaultLookupInputs(lookupType map[string]interface{}, cfg aapEnvironmentConfig, caPem string) (map[string]interface{}, error) {
	inputs := map[string]interface{}{}
	required := map[string]bool{}

	inputsDef, _ := lookupType["inputs"].(map[string]interface{})
	reqFields, _ := inputsDef["required"].([]interface{})
	for _, r := range reqFields {
		required[fmt.Sprintf("%v", r)] = true
	}

	fields, _ := inputsDef["fields"].([]interface{})
	for _, rawField := range fields {
		field, ok := rawField.(map[string]interface{})
		if !ok {
			continue
		}
		id := aapMapString(field, "id")
		if id == "" {
			continue
		}

		lc := strings.ToLower(id)
		switch {
		case lc == "url" || strings.Contains(lc, "server_url"):
			inputs[id] = vaultAAPAAPVaultURL
		case lc == "auth_path" || (strings.Contains(lc, "auth") && strings.Contains(lc, "path")):
			inputs[id] = vaultAAPJWTMountPath
		case lc == "role_id" || lc == "jwt_role" || lc == "role":
			inputs[id] = cfg.RoleName
		case lc == "api_version":
			inputs[id] = "v2"
		case lc == "ca_cert" || strings.Contains(lc, "ca") && strings.Contains(lc, "cert"):
			inputs[id] = caPem
		}
	}

	if len(fields) == 0 && len(reqFields) == 0 {
		inputs["url"] = vaultAAPAAPVaultURL
		inputs["jwt_role"] = cfg.RoleName
		inputs["api_version"] = "v2"
		return inputs, nil
	}

	missing := []string{}
	for req := range required {
		if _, ok := inputs[req]; ok {
			continue
		}
		fieldFound := false
		for _, rawField := range fields {
			field, ok := rawField.(map[string]interface{})
			if !ok {
				continue
			}
			if aapMapString(field, "id") == req {
				fieldFound = true
				if d, ok := field["default"]; ok {
					inputs[req] = d
				}
				if field["internal"] == true {
					inputs[req] = ""
				}
				break
			}
		}
		if _, ok := inputs[req]; !ok && fieldFound {
			missing = append(missing, req)
		}
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("unmapped required lookup inputs: %s", strings.Join(missing, ", "))
	}

	return inputs, nil
}

func buildAAPVaultLookupMetadata(lookupType map[string]interface{}, cfg aapEnvironmentConfig) (map[string]interface{}, error) {
	metadata := map[string]interface{}{}
	required := map[string]bool{}

	inputsDef, _ := lookupType["inputs"].(map[string]interface{})
	metadataFields, _ := inputsDef["metadata"].([]interface{})
	for _, rawField := range metadataFields {
		field, ok := rawField.(map[string]interface{})
		if !ok {
			continue
		}
		if req, ok := field["required"].(bool); ok && req {
			id := aapMapString(field, "id")
			if id != "" {
				required[id] = true
			}
		}
	}

	if len(metadataFields) == 0 {
		metadata["default_auth_path"] = vaultAAPJWTMountPath
		metadata["secret_path"] = cfg.SecretPath
		metadata["secret_key"] = "env_name"
		metadata["secret_backend"] = "secret"
		metadata["secret_version"] = ""
		return metadata, nil
	}

	for _, rawField := range metadataFields {
		field, ok := rawField.(map[string]interface{})
		if !ok {
			continue
		}
		id := aapMapString(field, "id")
		if id == "" {
			continue
		}
		lc := strings.ToLower(id)
		switch {
		case lc == "url" || strings.Contains(lc, "server_url"):
			metadata[id] = vaultAAPAAPVaultURL
		case lc == "jwt_role" || lc == "role" || lc == "role_id":
			metadata[id] = cfg.RoleName
		case lc == "api_version":
			metadata[id] = "v2"
		case lc == "secret_path" || strings.Contains(lc, "path") && strings.Contains(lc, "secret"):
			metadata[id] = cfg.SecretPath
		case lc == "secret_key" || lc == "key_name":
			metadata[id] = "env_name"
		case lc == "auth_path" || strings.Contains(lc, "auth") && strings.Contains(lc, "path"):
			metadata[id] = vaultAAPJWTMountPath
		case lc == "secret_backend" || strings.Contains(lc, "backend"):
			metadata[id] = "secret"
		case lc == "secret_version":
			metadata[id] = ""
		}
	}

	missing := []string{}
	for req := range required {
		if _, ok := metadata[req]; ok {
			continue
		}
		for _, rawField := range metadataFields {
			field, ok := rawField.(map[string]interface{})
			if !ok {
				continue
			}
			if aapMapString(field, "id") == req {
				if d, ok := field["default"]; ok {
					metadata[req] = d
				}
				break
			}
		}
		if _, ok := metadata[req]; !ok {
			if req == "job_template_id" {
				metadata[req] = ""
			} else {
				missing = append(missing, req)
			}
		}
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("unmapped required lookup metadata fields: %s", strings.Join(missing, ", "))
	}

	return metadata, nil
}
