package terraform

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"hal/internal/global"
	"hal/internal/integrations"

	"github.com/spf13/cobra"
)

const (
	tfeAgentPrimaryContainer   = "hal-tfe-agent"
	tfeAgentTwinContainer      = "hal-tfe-bis-agent"
	defaultTFEAgentImage       = "hashicorp/tfc-agent:1.28"
	defaultTFEAgentPoolName    = "hal-agent-pool"
	defaultTFETwinAgentPool    = "hal-agent-pool-bis"
	defaultTFEAgentDisplayName = "hal-tfc-agent"
	defaultTFETwinAgentName    = "hal-tfc-agent-bis"
	tfeProxyInternalIP         = "10.89.3.54"
)

type tfeAgentState struct {
	PoolID        string `json:"pool_id"`
	PoolName      string `json:"pool_name"`
	TokenID       string `json:"token_id"`
	ContainerName string `json:"container_name"`
	AgentName     string `json:"agent_name"`
	Image         string `json:"image"`
	Org           string `json:"org"`
	BaseURL       string `json:"base_url"`
}

var (
	tfeAgentEnable        bool
	tfeAgentDisable       bool
	tfeAgentUpdate        bool
	tfeAgentImage         string
	tfeAgentPoolName      string
	tfeAgentName          string
	tfeAgentOrg           string
	tfeAgentBaseURL       string
	tfeAgentAPIToken      string
	tfeAgentAdminUsername string
	tfeAgentAdminEmail    string
	tfeAgentAdminPassword string
	tfeAgentAutoApprove   bool
)

var agentCmd = &cobra.Command{
	Use:   "agent [status|enable|disable|update]",
	Short: "Deploy and manage a custom TFE agent for agent-pool-backed workspace runs",
	Run: func(cmd *cobra.Command, args []string) {
		if err := parseLifecycleAction(args, &tfeAgentEnable, &tfeAgentDisable, &tfeAgentUpdate); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		target, err := normalizeTFETarget(tfeLifecycleTarget)
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		if tfeAgentDisable {
			if tfeAgentEnable || tfeAgentUpdate {
				fmt.Println("❌ '--disable' cannot be combined with '--enable' or '--update'.")
				return
			}
			for _, lifecycleTarget := range tfeAgentTargets(target) {
				if err := configureTFEAgentTargetDefaults(cmd, lifecycleTarget); err != nil {
					fmt.Printf("❌ Failed to configure agent target defaults (%s): %v\n", lifecycleTarget, err)
					return
				}
				if err := disableTFEAgent(engine, lifecycleTarget, tfeAgentAutoApprove); err != nil {
					fmt.Printf("❌ Failed to disable Terraform agent flow (%s): %v\n", lifecycleTarget, err)
					return
				}
			}
			return
		}

		if tfeAgentUpdate {
			tfeAgentEnable = true
		}

		if !tfeAgentEnable {
			for i, lifecycleTarget := range tfeAgentTargets(target) {
				if err := configureTFEAgentTargetDefaults(cmd, lifecycleTarget); err != nil {
					fmt.Printf("❌ Failed to configure agent target defaults (%s): %v\n", lifecycleTarget, err)
					return
				}
				showTFEAgentStatus(engine, lifecycleTarget)
				if i+1 < len(tfeAgentTargets(target)) {
					fmt.Println("---------------------------------------------------------")
				}
			}
			return
		}

		for _, lifecycleTarget := range tfeAgentTargets(target) {
			if err := configureTFEAgentTargetDefaults(cmd, lifecycleTarget); err != nil {
				fmt.Printf("❌ Failed to configure agent target defaults (%s): %v\n", lifecycleTarget, err)
				return
			}
			if err := enableTFEAgent(engine, lifecycleTarget); err != nil {
				fmt.Printf("❌ Failed to enable Terraform agent flow (%s): %v\n", lifecycleTarget, err)
				return
			}
		}

		fmt.Println("✅ Terraform agent flow is ready.")
		fmt.Println("💡 Next Step:")
		fmt.Println("   In TFE UI, open your workspace settings and switch Execution Mode to 'Agent'.")
		fmt.Println("   Then pick the pool:", tfeAgentPoolName)
	},
}

func tfeAgentTargets(target string) []string {
	if target == tfeTargetBoth {
		return []string{tfeTargetPrimary, tfeTargetTwin}
	}
	return []string{target}
}

func showTFEAgentStatus(engine, target string) {
	fmt.Printf("🔍 Checking Terraform Agent Status (target=%s)...\n", target)

	coreContainer, coreErr := tfeCoreContainerForTarget(target)
	if coreErr != nil {
		fmt.Printf("  ❌ tfe-runtime         : unresolved (%v)\n", coreErr)
	} else if global.IsContainerRunning(engine, coreContainer) {
		fmt.Printf("  🟢 tfe-runtime         : running (%s)\n", coreContainer)
	} else {
		fmt.Printf("  ⚪ tfe-runtime         : not running (%s)\n", coreContainer)
	}

	containerName := tfeAgentContainerNameForTarget(target)

	if global.IsContainerRunning(engine, containerName) {
		fmt.Printf("  🟢 agent-container     : running (%s)\n", containerName)
	} else if containerExists(engine, containerName) {
		fmt.Printf("  🟡 agent-container     : stopped (%s)\n", containerName)
	} else {
		fmt.Printf("  ⚪ agent-container     : not created (%s)\n", containerName)
	}

	state, err := loadTFEAgentState(target)
	if err != nil {
		fmt.Printf("  ⚠️  state              : unreadable (%v)\n", err)
	} else if state == nil {
		fmt.Println("  ⚪ state              : not initialized")
	} else {
		fmt.Printf("  🟢 pool               : %s (%s)\n", state.PoolName, state.PoolID)
		if strings.TrimSpace(state.TokenID) != "" {
			fmt.Printf("  🟢 token-id           : %s\n", state.TokenID)
		} else {
			fmt.Println("  ⚪ token-id           : missing")
		}
		fmt.Printf("  🔗 tfe-url            : %s\n", state.BaseURL)
	}

	fmt.Println("\n💡 Next Step:")
	fmt.Printf("   hal terraform agent enable -t %s\n", target)
	fmt.Printf("   hal terraform agent disable -t %s\n", target)
}

func enableTFEAgent(engine, target string) error {
	coreContainer, err := tfeCoreContainerForTarget(target)
	if err != nil {
		return err
	}
	if !global.IsContainerRunning(engine, coreContainer) {
		if target == tfeTargetTwin {
			return fmt.Errorf("terraform enterprise twin is not running; run 'hal terraform create -t twin' first")
		}
		return fmt.Errorf("terraform enterprise is not running; run 'hal terraform create' first")
	}

	global.EnsureNetwork(engine)

	baseURL := strings.TrimSuffix(strings.TrimSpace(tfeAgentBaseURL), "/")
	if baseURL == "" {
		return fmt.Errorf("tfe url cannot be empty")
	}

	org := strings.ToLower(strings.TrimSpace(tfeAgentOrg))
	if org == "" {
		return fmt.Errorf("tfe org cannot be empty")
	}

	poolName := strings.TrimSpace(tfeAgentPoolName)
	if poolName == "" {
		return fmt.Errorf("agent pool name cannot be empty")
	}

	agentName := strings.TrimSpace(tfeAgentName)
	if agentName == "" {
		return fmt.Errorf("agent name cannot be empty")
	}

	image := strings.TrimSpace(tfeAgentImage)
	if image == "" {
		return fmt.Errorf("agent image cannot be empty")
	}

	containerName := tfeAgentContainerNameForTarget(target)

	if tfeAgentUpdate {
		_ = exec.Command(engine, "rm", "-f", containerName).Run()
	}

	state, _ := loadTFEAgentState(target)
	if global.IsContainerRunning(engine, containerName) && !tfeAgentUpdate {
		fmt.Println("ℹ️  Agent container is already running. Reusing existing runtime.")
		if state != nil {
			fmt.Printf("ℹ️  Existing pool: %s (%s)\n", state.PoolName, state.PoolID)
		}
		return nil
	}

	token, _, err := ensureTFEFoundation(engine, tfeFoundationConfig{
		BaseURL:       baseURL,
		OrgName:       org,
		ProjectName:   "",
		APIToken:      tfeAgentAPIToken,
		AdminUsername: tfeAgentAdminUsername,
		AdminEmail:    tfeAgentAdminEmail,
		AdminPassword: tfeAgentAdminPassword,
	})
	if err != nil {
		return fmt.Errorf("failed to bootstrap TFE API token: %w", err)
	}

	poolID, createdPool, err := ensureTFEAgentPool(baseURL, org, token, poolName)
	if err != nil {
		return err
	}
	if createdPool {
		fmt.Printf("✅ Created agent pool: %s (%s)\n", poolName, poolID)
	} else {
		fmt.Printf("ℹ️  Reusing agent pool: %s (%s)\n", poolName, poolID)
	}

	tokenID, tokenValue, err := createTFEAgentToken(baseURL, poolID, token, "hal-managed-agent")
	if err != nil {
		return err
	}

	certPath, err := tfeCLICertPath(target)
	if err != nil {
		return fmt.Errorf("missing local TFE certificate: %w", err)
	}

	addHostArg := ""
	if parsed, parseErr := url.Parse(baseURL); parseErr == nil {
		if strings.EqualFold(parsed.Hostname(), "tfe.localhost") {
			addHostArg = "tfe.localhost:" + tfeProxyInternalIP
		} else if strings.EqualFold(parsed.Hostname(), "tfe-bis.localhost") {
			addHostArg = "tfe-bis.localhost:" + tfeTwinProxyInternalIP
		}
	}

	runArgs := []string{
		"run", "-d",
		"--name", containerName,
		"--network", "hal-net",
	}
	if addHostArg != "" {
		runArgs = append(runArgs, "--add-host", addHostArg)
	}
	runArgs = append(runArgs,
		"-e", "TFC_ADDRESS="+baseURL,
		"-e", "TFC_AGENT_TOKEN="+tokenValue,
		"-e", "TFC_AGENT_NAME="+agentName,
		"-e", "TFC_AGENT_SINGLE=false",
		"-e", "SSL_CERT_FILE=/hal/certs/tfe-localhost.crt",
		"-v", fmt.Sprintf("%s:/hal/certs/tfe-localhost.crt:ro", certPath),
		image,
	)

	out, runErr := exec.Command(engine, runArgs...).CombinedOutput()
	if runErr != nil {
		_ = deleteTFEAgentToken(baseURL, tokenID, token)
		return fmt.Errorf("failed to start agent container: %s", strings.TrimSpace(string(out)))
	}

	state = &tfeAgentState{
		PoolID:        poolID,
		PoolName:      poolName,
		TokenID:       tokenID,
		ContainerName: containerName,
		AgentName:     agentName,
		Image:         image,
		Org:           org,
		BaseURL:       baseURL,
	}
	if err := saveTFEAgentState(target, state); err != nil {
		fmt.Printf("⚠️  Agent started but state persistence failed: %v\n", err)
	}

	agents, err := listTFEPoolAgents(baseURL, poolID, token)
	if err == nil && len(agents) > 0 {
		fmt.Printf("✅ Agent pool has %d known agent entry(s).\n", len(agents))
	}

	fmt.Printf("🔗 TFE Agent Pool: %s\n", poolName)
	fmt.Printf("📦 Agent Image: %s\n", image)
	return nil
}

func disableTFEAgent(engine, target string, autoApprove bool) error {
	if !autoApprove && isInteractiveTTY() {
		fmt.Printf("⚠️  Disable Terraform agent runtime for target=%s? [y/N]: ", target)
		var confirm string
		if _, err := fmt.Scanln(&confirm); err == nil {
			confirm = strings.ToLower(strings.TrimSpace(confirm))
			if confirm != "y" && confirm != "yes" {
				fmt.Println("ℹ️  Disable cancelled.")
				return nil
			}
		}
	}

	state, _ := loadTFEAgentState(target)
	containerName := tfeAgentContainerNameForTarget(target)

	if out, err := exec.Command(engine, "rm", "-f", containerName).CombinedOutput(); err != nil {
		msg := strings.ToLower(strings.TrimSpace(string(out)))
		if !strings.Contains(msg, "no such container") && !strings.Contains(msg, "no container") {
			return fmt.Errorf("failed to remove agent container: %s", strings.TrimSpace(string(out)))
		}
	} else if strings.TrimSpace(string(out)) != "" {
		fmt.Printf("✅ Removed container: %s\n", containerName)
	}

	if state != nil && strings.TrimSpace(state.TokenID) != "" {
		token := strings.TrimSpace(tfeAgentAPIToken)
		if token == "" {
			token = strings.TrimSpace(os.Getenv("TFE_API_TOKEN"))
		}
		if token == "" {
			token = global.LoadCachedTFEAPIToken()
		}

		baseURL := strings.TrimSuffix(strings.TrimSpace(state.BaseURL), "/")
		if baseURL == "" {
			baseURL = strings.TrimSuffix(strings.TrimSpace(tfeAgentBaseURL), "/")
		}

		if token != "" && baseURL != "" {
			if err := deleteTFEAgentToken(baseURL, state.TokenID, token); err != nil {
				fmt.Printf("⚠️  Could not revoke agent token %s: %v\n", state.TokenID, err)
			} else {
				fmt.Printf("🧹 Revoked agent token: %s\n", state.TokenID)
			}
		} else {
			fmt.Println("⚠️  Skipping token revoke (missing TFE API token or base URL).")
		}
	}

	if err := removeTFEAgentState(target); err != nil {
		fmt.Printf("⚠️  Could not remove local agent state file: %v\n", err)
	}

	fmt.Printf("✅ Terraform agent container removed for target=%s.\n", target)
	fmt.Println("ℹ️  Agent pool remains in TFE so you can re-attach quickly later.")
	return nil
}

func ensureTFEAgentPool(baseURL, org, apiToken, poolName string) (string, bool, error) {
	listURL := fmt.Sprintf("%s/api/v2/organizations/%s/agent-pools", baseURL, org)
	body, _, err := integrations.TFERequest("GET", listURL, apiToken, nil)
	if err != nil {
		return "", false, fmt.Errorf("agent pool list failed: %s", strings.TrimSpace(string(body)))
	}

	var listResp map[string]interface{}
	_ = json.Unmarshal(body, &listResp)
	if data, ok := listResp["data"].([]interface{}); ok {
		for _, item := range data {
			pool, _ := item.(map[string]interface{})
			attrs, _ := pool["attributes"].(map[string]interface{})
			name := strings.TrimSpace(fmt.Sprintf("%v", attrs["name"]))
			if name == poolName {
				poolID := strings.TrimSpace(fmt.Sprintf("%v", pool["id"]))
				if poolID != "" && poolID != "<nil>" {
					return poolID, false, nil
				}
			}
		}
	}

	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "agent-pools",
			"attributes": map[string]interface{}{
				"name":                poolName,
				"organization-scoped": true,
			},
		},
	}
	createBody, _, createErr := integrations.TFERequest("POST", listURL, apiToken, payload)
	if createErr != nil {
		return "", false, fmt.Errorf("agent pool creation failed: %s", strings.TrimSpace(string(createBody)))
	}

	poolID := extractTFEDataID(createBody)
	if poolID == "" {
		return "", false, fmt.Errorf("agent pool creation response did not include id")
	}

	return poolID, true, nil
}

func createTFEAgentToken(baseURL, poolID, apiToken, description string) (string, string, error) {
	if strings.TrimSpace(description) == "" {
		description = "hal-agent-token"
	}

	url := fmt.Sprintf("%s/api/v2/agent-pools/%s/authentication-tokens", baseURL, poolID)
	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "authentication-tokens",
			"attributes": map[string]interface{}{
				"description": description,
			},
		},
	}
	body, _, err := integrations.TFERequest("POST", url, apiToken, payload)
	if err != nil {
		return "", "", fmt.Errorf("agent token creation failed: %s", strings.TrimSpace(string(body)))
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(body, &resp)
	data, _ := resp["data"].(map[string]interface{})
	tokenID := strings.TrimSpace(fmt.Sprintf("%v", data["id"]))
	attrs, _ := data["attributes"].(map[string]interface{})
	tokenValue := strings.TrimSpace(fmt.Sprintf("%v", attrs["token"]))

	if tokenID == "" || tokenID == "<nil>" {
		return "", "", fmt.Errorf("agent token creation response missing token id")
	}
	if tokenValue == "" || tokenValue == "<nil>" {
		return "", "", fmt.Errorf("agent token creation response missing token value")
	}

	return tokenID, tokenValue, nil
}

func deleteTFEAgentToken(baseURL, tokenID, apiToken string) error {
	url := fmt.Sprintf("%s/api/v2/authentication-tokens/%s", baseURL, tokenID)
	body, _, err := integrations.TFERequest("DELETE", url, apiToken, nil)
	if err != nil {
		return fmt.Errorf("token delete failed: %s", strings.TrimSpace(string(body)))
	}
	return nil
}

func listTFEPoolAgents(baseURL, poolID, apiToken string) ([]string, error) {
	url := fmt.Sprintf("%s/api/v2/agent-pools/%s/agents", baseURL, poolID)
	body, _, err := integrations.TFERequest("GET", url, apiToken, nil)
	if err != nil {
		return nil, err
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(body, &resp)
	data, _ := resp["data"].([]interface{})
	agents := make([]string, 0, len(data))
	for _, item := range data {
		agent, _ := item.(map[string]interface{})
		id := strings.TrimSpace(fmt.Sprintf("%v", agent["id"]))
		if id != "" && id != "<nil>" {
			agents = append(agents, id)
		}
	}
	return agents, nil
}

func tfeAgentContainerNameForTarget(target string) string {
	if target == tfeTargetTwin {
		return tfeAgentTwinContainer
	}
	return tfeAgentPrimaryContainer
}

func configureTFEAgentTargetDefaults(cmd *cobra.Command, target string) error {
	if target == tfeTargetTwin {
		layout, err := buildTFETwinLayout()
		if err != nil {
			return err
		}
		if !cmd.Flags().Changed("tfe-url") {
			tfeAgentBaseURL = layout.UIURL
		}
		if !cmd.Flags().Changed("tfe-org") {
			tfeAgentOrg = tfeTwinOrg
		}
		if !cmd.Flags().Changed("tfe-admin-username") {
			tfeAgentAdminUsername = tfeTwinAdminUser
		}
		if !cmd.Flags().Changed("tfe-admin-email") {
			tfeAgentAdminEmail = tfeTwinAdminEmail
		}
		if !cmd.Flags().Changed("tfe-admin-password") {
			tfeAgentAdminPassword = tfeTwinAdminPass
		}
		if !cmd.Flags().Changed("pool-name") {
			tfeAgentPoolName = defaultTFETwinAgentPool
		}
		if !cmd.Flags().Changed("agent-name") {
			tfeAgentName = defaultTFETwinAgentName
		}
		return nil
	}

	if !cmd.Flags().Changed("tfe-url") {
		tfeAgentBaseURL = "https://tfe.localhost:8443"
	}
	if !cmd.Flags().Changed("tfe-org") {
		tfeAgentOrg = "hal"
	}
	if !cmd.Flags().Changed("tfe-admin-username") {
		tfeAgentAdminUsername = "haladmin"
	}
	if !cmd.Flags().Changed("tfe-admin-email") {
		tfeAgentAdminEmail = "haladmin@localhost"
	}
	if !cmd.Flags().Changed("tfe-admin-password") {
		tfeAgentAdminPassword = "hal9000FTW"
	}
	if !cmd.Flags().Changed("pool-name") {
		tfeAgentPoolName = defaultTFEAgentPoolName
	}
	if !cmd.Flags().Changed("agent-name") {
		tfeAgentName = defaultTFEAgentDisplayName
	}

	return nil
}

func tfeAgentStatePath(target string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	name := "tfe-agent-state.json"
	if target == tfeTargetTwin {
		name = "tfe-agent-bis-state.json"
	}
	return filepath.Join(homeDir, ".hal", name), nil
}

func saveTFEAgentState(target string, state *tfeAgentState) error {
	if state == nil {
		return fmt.Errorf("state cannot be nil")
	}
	path, err := tfeAgentStatePath(target)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0600)
}

func loadTFEAgentState(target string) (*tfeAgentState, error) {
	path, err := tfeAgentStatePath(target)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var state tfeAgentState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func removeTFEAgentState(target string) error {
	path, err := tfeAgentStatePath(target)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func init() {
	agentCmd.Flags().BoolVarP(&tfeAgentEnable, "enable", "e", false, "Create or reuse a TFE agent pool, issue token, and run an agent container")
	agentCmd.Flags().BoolVarP(&tfeAgentDisable, "disable", "d", false, "Stop and remove the HAL-managed TFE agent container and revoke its token")
	agentCmd.Flags().BoolVarP(&tfeAgentUpdate, "update", "u", false, "Reconcile existing agent pool/runtime settings")
	_ = agentCmd.Flags().MarkHidden("enable")
	_ = agentCmd.Flags().MarkHidden("disable")
	_ = agentCmd.Flags().MarkHidden("update")
	agentCmd.Flags().BoolVar(&tfeAgentAutoApprove, "auto-approve", false, "Skip interactive confirmation for destructive disable operations")
	agentCmd.Flags().StringVar(&tfeAgentImage, "image", defaultTFEAgentImage, "Docker image used for the custom TFE agent")
	agentCmd.Flags().StringVar(&tfeAgentPoolName, "pool-name", defaultTFEAgentPoolName, "TFE agent pool name to create or reuse")
	agentCmd.Flags().StringVar(&tfeAgentName, "agent-name", defaultTFEAgentDisplayName, "Display name advertised by the running agent")
	agentCmd.Flags().StringVar(&tfeAgentOrg, "tfe-org", "hal", "Terraform Enterprise organization name")
	agentCmd.Flags().StringVar(&tfeAgentBaseURL, "tfe-url", "https://tfe.localhost:8443", "Terraform Enterprise base URL")
	agentCmd.Flags().StringVar(&tfeAgentAPIToken, "tfe-api-token", "", "Terraform Enterprise app API token (or set TFE_API_TOKEN)")
	agentCmd.Flags().StringVar(&tfeAgentAdminUsername, "tfe-admin-username", "haladmin", "Initial TFE admin username used when bootstrapping via IACT")
	agentCmd.Flags().StringVar(&tfeAgentAdminEmail, "tfe-admin-email", "haladmin@localhost", "Initial TFE admin email used when bootstrapping via IACT")
	agentCmd.Flags().StringVar(&tfeAgentAdminPassword, "tfe-admin-password", "hal9000FTW", "Initial TFE admin password used when bootstrapping via IACT")
	bindTFETargetFlag(agentCmd)
	bindTwinFlags(agentCmd)

	// Keep advanced tuning available but hidden for a concise default help surface.
	for _, name := range []string{
		"image",
		"pool-name",
		"agent-name",
		"tfe-org",
		"tfe-url",
		"tfe-api-token",
		"tfe-admin-username",
		"tfe-admin-email",
		"tfe-admin-password",
		"twin-version",
		"twin-password",
		"twin-tfe-org",
		"twin-tfe-project",
		"twin-tfe-admin-username",
		"twin-tfe-admin-email",
		"twin-tfe-admin-password",
		"twin-proxy-nginx-version",
		"twin-https-port",
		"twin-hostname",
		"twin-container-name",
		"twin-proxy-ip",
		"twin-db-password",
		"twin-db-name",
		"twin-minio-root-user",
		"twin-minio-root-password",
		"twin-s3-bucket",
	} {
		_ = agentCmd.Flags().MarkHidden(name)
	}

	Cmd.AddCommand(agentCmd)
}
