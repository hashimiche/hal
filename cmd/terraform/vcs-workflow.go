package terraform

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"hal/internal/global"
	"hal/internal/integrations"

	"github.com/spf13/cobra"
)

var (
	workspaceEnable          bool
	workspaceDisable         bool
	workspaceUpdate          bool
	workspaceAutoApprove     bool
	workspaceGitLabVersion   string
	workspaceGitLabPassword  string
	workspaceProjectPath     string
	workspaceProjectName     string
	tfeOrgName               string
	tfeProjectName           string
	tfeWorkspaceName         string
	tfeAPIToken              string
	tfeVCSOAuthTokenID       string
	tfeBaseURL               string
	tfeAdminUsername         string
	tfeAdminEmail            string
	tfeAdminPassword         string
	tfeTagsRegex             string
	tfeVCSBranch             string
	workspaceGitLabServiceID = "gitlab"
)

var workspaceCmd = &cobra.Command{
	Use:     "vcs-workflow [status|enable|disable|update]",
	Aliases: []string{"vcs", "workspace", "ws"},
	Short:   "Configure a Terraform VCS-driven lab with shared GitLab reuse",
	Run: func(cmd *cobra.Command, args []string) {
		if err := parseLifecycleAction(args, &workspaceEnable, &workspaceDisable, &workspaceUpdate); err != nil {
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

		if workspaceUpdate {
			workspaceEnable = true
		}

		if target == tfeTargetBoth {
			if workspaceDisable {
				disableWorkspaceScenario(engine, tfeTargetPrimary, workspaceAutoApprove)
				disableWorkspaceScenario(engine, tfeTargetTwin, workspaceAutoApprove)
				return
			}

			if !workspaceEnable {
				showWorkspaceScenarioStatus(engine, tfeTargetPrimary)
				fmt.Println("---------------------------------------------------------")
				showWorkspaceScenarioStatus(engine, tfeTargetTwin)
				return
			}

			if err := runWorkspaceScenarioEnable(cmd, engine, tfeTargetPrimary); err != nil {
				fmt.Printf("❌ %v\n", err)
				return
			}
			fmt.Println("---------------------------------------------------------")
			if err := runWorkspaceScenarioEnable(cmd, engine, tfeTargetTwin); err != nil {
				fmt.Printf("❌ %v\n", err)
				return
			}
			return
		}

		if workspaceDisable {
			if workspaceEnable {
				fmt.Println("❌ '--disable' cannot be combined with '--enable'.")
				return
			}
			disableWorkspaceScenario(engine, target, workspaceAutoApprove)
			return
		}

		if !workspaceEnable {
			showWorkspaceScenarioStatus(engine, target)
			return
		}

		if err := runWorkspaceScenarioEnable(cmd, engine, target); err != nil {
			fmt.Printf("❌ %v\n", err)
		}
	},
}

func showWorkspaceScenarioStatus(engine, target string) {
	fmt.Printf("🔍 Checking Terraform VCS workflow prerequisites (target=%s)...\n", target)
	coreContainer, err := tfeCoreContainerForTarget(target)
	if err != nil {
		fmt.Printf("  ❌ Terraform Enterprise target: unresolved (%v)\n", err)
	} else if global.IsContainerRunning(engine, coreContainer) {
		fmt.Printf("  ✅ Terraform Enterprise target: running (%s)\n", coreContainer)
	} else {
		fmt.Printf("  ❌ Terraform Enterprise target: not running (%s)\n", coreContainer)
	}

	if global.IsContainerRunning(engine, "hal-gitlab") {
		fmt.Println("  ✅ Shared GitLab: running")
	} else {
		fmt.Println("  ⚠️  Shared GitLab: not running (will be bootstrapped automatically)")
	}

	fmt.Println("\n💡 Next Step:")
	fmt.Printf("   hal terraform vcs-workflow enable -t %s\n", target)
}

func runWorkspaceScenarioEnable(cmd *cobra.Command, engine, target string) error {
	if err := configureWorkspaceTargetDefaults(cmd, target); err != nil {
		return err
	}

	coreContainer, err := tfeCoreContainerForTarget(target)
	if err != nil {
		return err
	}
	if !global.IsContainerRunning(engine, coreContainer) {
		if target == tfeTargetTwin {
			return fmt.Errorf("Terraform Enterprise twin target is not running (%s). Run 'hal terraform create -t twin' first", coreContainer)
		}
		return fmt.Errorf("Terraform Enterprise is not running (%s). Run 'hal terraform create' first", coreContainer)
	}

	global.EnsureNetwork(engine)

	reused, err := integrations.EnsureGitLabCE(engine, workspaceGitLabVersion, workspaceGitLabPassword)
	if err != nil {
		return err
	}
	if reused {
		fmt.Println("ℹ️  Reusing existing shared GitLab service.")
	} else {
		fmt.Println("🚀 Booted shared GitLab service for Terraform VCS workflow setup.")
	}

	fmt.Println("⏳ Waiting for GitLab API...")
	if err := integrations.WaitForGitLab("http://127.0.0.1:8080", 90); err != nil {
		return fmt.Errorf("GitLab failed to become ready in time")
	}

	if err := ensureGitLabCanReachTFEWebhook(engine); err != nil {
		fmt.Printf("⚠️  Could not enforce GitLab -> TFE webhook routing automatically: %v\n", err)
		fmt.Println("   💡 VCS push events may not trigger runs if tfe.localhost is unreachable from hal-gitlab.")
	}

	vcsToken, err := integrations.GitLabPasswordToken("http://127.0.0.1:8080/oauth/token", "root", workspaceGitLabPassword)
	if err != nil {
		return fmt.Errorf("failed to retrieve GitLab API token: %w", err)
	}

	if err := ensureGitLabAllowsLocalWebhooks(vcsToken); err != nil {
		fmt.Printf("⚠️  Could not relax GitLab local webhook policy automatically: %v\n", err)
		fmt.Println("   💡 TFE may fail to attach VCS webhooks until this is enabled in GitLab application settings.")
	}

	gitlabProjectID, webURL, err := ensureTFDemoProject(vcsToken)
	if err != nil {
		return fmt.Errorf("failed to prepare Terraform demo repository: %w", err)
	}

	if err := seedTFDemoFiles(vcsToken, gitlabProjectID); err != nil {
		fmt.Printf("⚠️  Repository exists but demo files were not fully updated: %v\n", err)
	}

	if err := global.AddSharedServiceConsumer(workspaceGitLabServiceID, workspaceSharedConsumerForTarget(target)); err != nil {
		fmt.Printf("⚠️  Could not persist shared ownership metadata: %v\n", err)
	}

	tokenSourceHint := ""
	if tfeVCSOAuthTokenID == "" {
		tfeVCSOAuthTokenID = os.Getenv("TFE_GITLAB_OAUTH_TOKEN_ID")
	}
	if tfeVCSOAuthTokenID == "" {
		tfeVCSOAuthTokenID = os.Getenv("TFE_GITLAB_TOKEN_ID")
	}

	tfeProjectID := ""
	tfeAPIToken, tfeProjectID, err = ensureTFEFoundation(engine, tfeFoundationConfig{
		BaseURL:       tfeBaseURL,
		OrgName:       tfeOrgName,
		ProjectName:   tfeProjectName,
		APIToken:      tfeAPIToken,
		AdminUsername: tfeAdminUsername,
		AdminEmail:    tfeAdminEmail,
		AdminPassword: tfeAdminPassword,
	})
	if err != nil {
		fmt.Printf("⚠️  TFE foundation bootstrap incomplete: %v\n", err)
		fmt.Println("   💡 HAL could not mint a usable API token automatically from this TFE instance.")
	} else {
		tokenSourceHint = "✅ TFE app API token ready (cached for reuse)."
	}

	if tfeAPIToken != "" && strings.TrimSpace(tfeVCSOAuthTokenID) != "" {
		valid, reason := isGitLabOAuthTokenIDUsable(strings.TrimSpace(tfeVCSOAuthTokenID))
		if !valid {
			fmt.Printf("⚠️  Ignoring provided GitLab OAuth token id '%s': %s\n", tfeVCSOAuthTokenID, reason)
			tfeVCSOAuthTokenID = ""
		}
	}

	if tfeAPIToken != "" && tfeVCSOAuthTokenID == "" {
		fmt.Println("⚙️  Creating GitLab VCS token and wiring Terraform Enterprise OAuth automatically...")
		oauthID, oauthErr := ensureTFEGitLabOAuthTokenID(tfeOrgName, vcsToken)
		if oauthErr != nil {
			fmt.Printf("⚠️  OAuth-access-token wiring failed, retrying with PAT fallback: %v\n", oauthErr)
			gitlabPAT, patErr := createGitLabPAT(vcsToken)
			if patErr != nil {
				fmt.Printf("⚠️  Could not create GitLab PAT for TFE VCS wiring: %v\n", patErr)
			} else {
				oauthID, oauthErr = ensureTFEGitLabOAuthTokenID(tfeOrgName, gitlabPAT)
				if oauthErr != nil {
					fmt.Printf("⚠️  Could not auto-create TFE GitLab OAuth token id: %v\n", oauthErr)
				} else {
					tfeVCSOAuthTokenID = oauthID
					fmt.Printf("✅ TFE GitLab OAuth token id ready: %s\n", tfeVCSOAuthTokenID)
				}
			}
		} else {
			tfeVCSOAuthTokenID = oauthID
			fmt.Printf("✅ TFE GitLab OAuth token id ready: %s\n", tfeVCSOAuthTokenID)
		}
	}

	if tfeAPIToken == "" {
		fmt.Println("⚠️  Skipping TFE workspace wiring: missing usable TFE API token.")
	} else {
		repoIdentifier := fmt.Sprintf("root/%s", workspaceProjectPath)
		workspaceURL, err := ensureTFEWorkspace(strings.ToLower(tfeOrgName), tfeProjectID, repoIdentifier)
		if err != nil && strings.Contains(strings.ToLower(err.Error()), "tags regex") {
			fmt.Println("⚠️  TFE rejected tags-regex with current trigger settings; retrying without tags-regex...")
			tfeTagsRegex = ""
			workspaceURL, err = ensureTFEWorkspace(strings.ToLower(tfeOrgName), tfeProjectID, repoIdentifier)
		}
		if err != nil && strings.Contains(strings.ToLower(err.Error()), "failed to create webhook on repository") {
			fmt.Println("⚠️  Webhook creation failed with current OAuth token; rotating token and retrying once...")
			if refreshedID, rotateErr := ensureTFEGitLabOAuthTokenID(tfeOrgName, vcsToken); rotateErr == nil {
				tfeVCSOAuthTokenID = refreshedID
				workspaceURL, err = ensureTFEWorkspace(strings.ToLower(tfeOrgName), tfeProjectID, repoIdentifier)
			} else {
				fmt.Printf("⚠️  OAuth token rotation failed: %v\n", rotateErr)
			}
		}
		if err != nil {
			fmt.Printf("⚠️  TFE workspace bootstrap incomplete: %v\n", err)
		} else {
			fmt.Printf("🔗 TFE Workspace: %s\n", workspaceURL)
		}
	}
	if tokenSourceHint != "" {
		fmt.Println(tokenSourceHint)
	}

	fmt.Printf("\n✅ Terraform VCS workflow prepared (target=%s).\n", target)
	fmt.Println("---------------------------------------------------------")
	fmt.Printf("🔗 GitLab Repo: %s\n", webURL)
	fmt.Println("   Login:       root / hal9000FTW")
	fmt.Println("🧭 Next:        Push a new commit to main in GitLab to validate the end-to-end VCS-driven auto-apply workflow")
	fmt.Println("---------------------------------------------------------")

	return nil
}

func disableWorkspaceScenario(engine, target string, autoApprove bool) {
	if global.DryRun {
		fmt.Printf("[DRY RUN] Would remove terraform-vcs-workflow consumer (%s) from shared gitlab service\n", target)
		fmt.Println("[DRY RUN] Would stop shared GitLab if no remaining consumers")
		return
	}

	if !autoApprove && isInteractiveTTY() {
		fmt.Printf("⚠️  'hal tf vcs-workflow disable -t %s' is destructive for workflow metadata. Continue? [y/N]: ", target)
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("ℹ️  Disable cancelled.")
			return
		}
	}

	remaining, err := global.RemoveSharedServiceConsumer(workspaceGitLabServiceID, workspaceSharedConsumerForTarget(target))
	if err != nil {
		fmt.Printf("⚠️  Could not update shared GitLab ownership metadata: %v\n", err)
	}

	if len(remaining) > 0 {
		fmt.Printf("ℹ️  Shared GitLab remains active (still used by: %s).\n", strings.Join(remaining, ", "))
		fmt.Printf("✅ Terraform VCS workflow disabled for target=%s (metadata only).\n", target)
		return
	}

	if isAnyTFERuntimeRunning(engine) {
		fmt.Println("ℹ️  Shared GitLab remains active because at least one Terraform Enterprise runtime is still running.")
		fmt.Printf("✅ Terraform VCS workflow disabled for target=%s (GitLab preserved).\n", target)
		return
	}

	if global.IsContainerRunning(engine, "hal-gitlab") {
		if out, rmErr := exec.Command(engine, "rm", "-f", "hal-gitlab").CombinedOutput(); rmErr != nil {
			fmt.Printf("⚠️  Failed to stop shared GitLab container: %s\n", strings.TrimSpace(string(out)))
		} else {
			fmt.Println("🧹 Stopped shared GitLab (no remaining shared-service consumers).")
		}
	}

	_ = global.ClearSharedService(workspaceGitLabServiceID)
	fmt.Printf("✅ Terraform VCS workflow disabled for target=%s.\n", target)
}

func ensureTFEWorkspace(orgName, projectID, repoIdentifier string) (string, error) {
	getURL := fmt.Sprintf("%s/api/v2/organizations/%s/workspaces/%s", tfeBaseURL, orgName, tfeWorkspaceName)
	body, status, getErr := integrations.TFERequest("GET", getURL, tfeAPIToken, nil)
	if getErr == nil {
		var existing map[string]interface{}
		_ = json.Unmarshal(body, &existing)
		data, _ := existing["data"].(map[string]interface{})
		workspaceID := fmt.Sprintf("%v", data["id"])

		attributes := map[string]interface{}{
			"auto-apply":     true,
			"execution-mode": "remote",
			"queue-all-runs": true,
		}
		if tfeVCSOAuthTokenID != "" {
			attributes["vcs-repo"] = buildTFEVCSRepoConfig(repoIdentifier)
		}

		patchPayload := map[string]interface{}{
			"data": map[string]interface{}{
				"type":       "workspaces",
				"id":         workspaceID,
				"attributes": attributes,
			},
		}
		patchURL := fmt.Sprintf("%s/api/v2/workspaces/%s", tfeBaseURL, workspaceID)
		patchBody, _, patchErr := integrations.TFERequest("PATCH", patchURL, tfeAPIToken, patchPayload)
		if patchErr != nil {
			return "", fmt.Errorf("workspace exists but update failed: %s", strings.TrimSpace(string(patchBody)))
		}
		return fmt.Sprintf("%s/app/organizations/%s/workspaces/%s", tfeBaseURL, orgName, tfeWorkspaceName), nil
	}
	if status != 404 {
		return "", fmt.Errorf("workspace lookup failed: %s", strings.TrimSpace(string(body)))
	}

	attributes := map[string]interface{}{
		"name":           tfeWorkspaceName,
		"auto-apply":     true,
		"execution-mode": "remote",
		"queue-all-runs": true,
	}

	if tfeVCSOAuthTokenID != "" {
		attributes["vcs-repo"] = buildTFEVCSRepoConfig(repoIdentifier)
	}

	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type":       "workspaces",
			"attributes": attributes,
			"relationships": map[string]interface{}{
				"project": map[string]interface{}{
					"data": map[string]interface{}{
						"type": "projects",
						"id":   projectID,
					},
				},
			},
		},
	}

	createURL := fmt.Sprintf("%s/api/v2/organizations/%s/workspaces", tfeBaseURL, orgName)
	createBody, _, createErr := integrations.TFERequest("POST", createURL, tfeAPIToken, payload)
	if createErr != nil {
		return "", fmt.Errorf("workspace creation failed: %s", strings.TrimSpace(string(createBody)))
	}

	if tfeVCSOAuthTokenID == "" {
		fmt.Println("⚠️  Workspace created without VCS link (missing GitLab OAuth token id).")
		fmt.Println("   💡 Re-run 'hal tf vcs enable' after TFE token is available to finish VCS wiring automatically.")
	}

	return fmt.Sprintf("%s/app/organizations/%s/workspaces/%s", tfeBaseURL, orgName, tfeWorkspaceName), nil
}

func ensureTFEGitLabOAuthTokenID(orgName, gitlabToken string) (string, error) {
	org := strings.ToLower(strings.TrimSpace(orgName))
	if org == "" {
		return "", fmt.Errorf("organization name cannot be empty")
	}

	clientID, err := ensureTFEGitLabOAuthClient(org)
	if err != nil {
		return "", err
	}

	// Remove any stale tokens first so workspace wiring always uses a fresh, known-good token.
	_ = deleteOrgOAuthTokensForClient(org, clientID)

	if err := setOAuthTokenStringOnClient(clientID, gitlabToken); err != nil {
		return "", err
	}

	tokenID := ""
	for i := 0; i < 10; i++ {
		tokenID = findOrgOAuthTokenForClient(org, clientID)
		if tokenID != "" {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if tokenID == "" {
		return "", fmt.Errorf("oauth token was not created after oauth-token-string update")
	}

	return tokenID, nil
}

func deleteOrgOAuthTokensForClient(orgName, clientID string) error {
	listURL := fmt.Sprintf("%s/api/v2/organizations/%s/oauth-tokens", tfeBaseURL, orgName)
	body, _, err := integrations.TFERequest("GET", listURL, tfeAPIToken, nil)
	if err != nil {
		return err
	}

	var listResp map[string]interface{}
	_ = json.Unmarshal(body, &listResp)
	data, _ := listResp["data"].([]interface{})
	for _, item := range data {
		token, _ := item.(map[string]interface{})
		tokenID := strings.TrimSpace(fmt.Sprintf("%v", token["id"]))
		if tokenID == "" || tokenID == "<nil>" {
			continue
		}

		rel, _ := token["relationships"].(map[string]interface{})
		oauthClient, _ := rel["oauth-client"].(map[string]interface{})
		oauthClientData, _ := oauthClient["data"].(map[string]interface{})
		if fmt.Sprintf("%v", oauthClientData["id"]) != clientID {
			continue
		}

		delURL := fmt.Sprintf("%s/api/v2/oauth-tokens/%s", tfeBaseURL, tokenID)
		_, _, _ = integrations.TFERequest("DELETE", delURL, tfeAPIToken, nil)
	}

	return nil
}

func createGitLabPAT(apiToken string) (string, error) {
	expires := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	name := fmt.Sprintf("hal-tfe-vcs-%d", time.Now().Unix())
	body, err := integrations.GitLabPost("http://127.0.0.1:8080/api/v4/users/1/personal_access_tokens", apiToken, map[string]interface{}{
		"name":       name,
		"scopes":     []string{"api", "read_repository", "write_repository", "read_user"},
		"expires_at": expires,
	})
	if err != nil {
		return "", err
	}

	var tokenResp map[string]interface{}
	if unmarshalErr := json.Unmarshal(body, &tokenResp); unmarshalErr != nil {
		return "", unmarshalErr
	}
	token, _ := tokenResp["token"].(string)
	if strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("gitlab PAT create response did not include token")
	}

	return token, nil
}

func ensureTFEGitLabOAuthClient(orgName string) (string, error) {
	listURL := fmt.Sprintf("%s/api/v2/organizations/%s/oauth-clients", tfeBaseURL, orgName)
	body, status, err := integrations.TFERequest("GET", listURL, tfeAPIToken, nil)
	if err == nil {
		if existing := findOAuthClientForGitLab(body); existing != "" {
			return existing, nil
		}
	} else if status != 404 {
		return "", fmt.Errorf("oauth client lookup failed: %s", strings.TrimSpace(string(body)))
	}

	payloads := []map[string]interface{}{
		{
			"data": map[string]interface{}{
				"type": "oauth-clients",
				"attributes": map[string]interface{}{
					"name":             "hal-gitlab",
					"service-provider": "gitlab_community_edition",
					"http-url":         "http://gitlab.localhost:8080",
					"api-url":          "http://gitlab.localhost:8080/api/v4",
				},
			},
		},
		{
			"data": map[string]interface{}{
				"type": "oauth-clients",
				"attributes": map[string]interface{}{
					"name":             "hal-gitlab",
					"service-provider": "gitlab",
					"http-url":         "http://gitlab.localhost:8080",
					"api-url":          "http://gitlab.localhost:8080/api/v4",
				},
			},
		},
	}

	createURL := fmt.Sprintf("%s/api/v2/organizations/%s/oauth-clients", tfeBaseURL, orgName)
	var lastErr error
	for _, payload := range payloads {
		resp, _, createErr := integrations.TFERequest("POST", createURL, tfeAPIToken, payload)
		if createErr != nil {
			lastErr = fmt.Errorf("%s", strings.TrimSpace(string(resp)))
			continue
		}

		id := extractTFEDataID(resp)
		if id != "" {
			return id, nil
		}
	}

	if lastErr != nil {
		return "", fmt.Errorf("oauth client creation failed: %v", lastErr)
	}
	return "", fmt.Errorf("oauth client creation failed")
}

func setOAuthTokenStringOnClient(clientID, gitlabToken string) error {
	patchURL := fmt.Sprintf("%s/api/v2/oauth-clients/%s", tfeBaseURL, clientID)
	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "oauth-clients",
			"id":   clientID,
			"attributes": map[string]interface{}{
				"oauth-token-string": gitlabToken,
			},
		},
	}

	body, _, err := integrations.TFERequest("PATCH", patchURL, tfeAPIToken, payload)
	if err != nil {
		return fmt.Errorf("oauth token string update failed: %s", strings.TrimSpace(string(body)))
	}

	return nil
}

func findOrgOAuthTokenForClient(orgName, clientID string) string {
	listURL := fmt.Sprintf("%s/api/v2/organizations/%s/oauth-tokens", tfeBaseURL, orgName)
	body, _, err := integrations.TFERequest("GET", listURL, tfeAPIToken, nil)
	if err != nil {
		return ""
	}

	var listResp map[string]interface{}
	_ = json.Unmarshal(body, &listResp)
	data, _ := listResp["data"].([]interface{})
	selectedID := ""
	selectedAt := time.Time{}
	for _, item := range data {
		token, _ := item.(map[string]interface{})
		rel, _ := token["relationships"].(map[string]interface{})
		oauthClient, _ := rel["oauth-client"].(map[string]interface{})
		oauthClientData, _ := oauthClient["data"].(map[string]interface{})
		if fmt.Sprintf("%v", oauthClientData["id"]) == clientID {
			tokenID := strings.TrimSpace(fmt.Sprintf("%v", token["id"]))
			if tokenID == "" || tokenID == "<nil>" {
				continue
			}

			attrs, _ := token["attributes"].(map[string]interface{})
			updatedAt := parseTFETime(fmt.Sprintf("%v", attrs["updated-at"]))
			if updatedAt.IsZero() {
				updatedAt = parseTFETime(fmt.Sprintf("%v", attrs["created-at"]))
			}

			if selectedID == "" {
				selectedID = tokenID
				selectedAt = updatedAt
				continue
			}

			if !updatedAt.IsZero() {
				if selectedAt.IsZero() || updatedAt.After(selectedAt) {
					selectedID = tokenID
					selectedAt = updatedAt
				}
			} else if selectedAt.IsZero() {
				// Fall back to last-seen token when timestamps are missing.
				selectedID = tokenID
			}
		}
	}

	return selectedID
}

func parseTFETime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "<nil>" {
		return time.Time{}
	}

	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}

	return t
}

func findOAuthClientForGitLab(body []byte) string {
	var listResp map[string]interface{}
	_ = json.Unmarshal(body, &listResp)
	data, _ := listResp["data"].([]interface{})
	for _, item := range data {
		client, _ := item.(map[string]interface{})
		attrs, _ := client["attributes"].(map[string]interface{})
		serviceProvider := strings.ToLower(fmt.Sprintf("%v", attrs["service-provider"]))
		httpURL := strings.ToLower(fmt.Sprintf("%v", attrs["http-url"]))
		if strings.Contains(serviceProvider, "gitlab") || strings.Contains(httpURL, "gitlab") {
			return fmt.Sprintf("%v", client["id"])
		}
	}
	return ""
}

func extractTFEDataID(body []byte) string {
	var resp map[string]interface{}
	_ = json.Unmarshal(body, &resp)
	data, _ := resp["data"].(map[string]interface{})
	if data == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", data["id"]))
}

func isGitLabOAuthTokenIDUsable(tokenID string) (bool, string) {
	tokenURL := fmt.Sprintf("%s/api/v2/oauth-tokens/%s", tfeBaseURL, tokenID)
	body, _, err := integrations.TFERequest("GET", tokenURL, tfeAPIToken, nil)
	if err != nil {
		return false, "token id not found or not readable"
	}

	var tokenResp map[string]interface{}
	_ = json.Unmarshal(body, &tokenResp)
	data, _ := tokenResp["data"].(map[string]interface{})
	rel, _ := data["relationships"].(map[string]interface{})
	oauthClient, _ := rel["oauth-client"].(map[string]interface{})
	oauthClientData, _ := oauthClient["data"].(map[string]interface{})
	clientID := fmt.Sprintf("%v", oauthClientData["id"])
	if strings.TrimSpace(clientID) == "" || clientID == "<nil>" {
		return false, "token is not linked to an oauth client"
	}

	clientURL := fmt.Sprintf("%s/api/v2/oauth-clients/%s", tfeBaseURL, clientID)
	clientBody, _, clientErr := integrations.TFERequest("GET", clientURL, tfeAPIToken, nil)
	if clientErr != nil {
		return false, "unable to resolve oauth client for token"
	}

	var clientResp map[string]interface{}
	_ = json.Unmarshal(clientBody, &clientResp)
	clientData, _ := clientResp["data"].(map[string]interface{})
	attrs, _ := clientData["attributes"].(map[string]interface{})
	serviceProvider := strings.ToLower(fmt.Sprintf("%v", attrs["service-provider"]))
	httpURL := strings.ToLower(fmt.Sprintf("%v", attrs["http-url"]))

	if !strings.Contains(serviceProvider, "gitlab") && !strings.Contains(httpURL, "gitlab") {
		return false, fmt.Sprintf("token belongs to non-GitLab oauth client (%s)", serviceProvider)
	}

	return true, ""
}

func ensureGitLabAllowsLocalWebhooks(apiToken string) error {
	settingsURL := "http://127.0.0.1:8080/api/v4/application/settings?allow_local_requests_from_web_hooks_and_services=true"
	req, err := http.NewRequest(http.MethodPut, settingsURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiToken)

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gitlab settings update failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}

func ensureTFDemoProject(token string) (string, string, error) {
	body, err := integrations.GitLabPost("http://127.0.0.1:8080/api/v4/projects", token, map[string]interface{}{
		"name":                   workspaceProjectName,
		"path":                   workspaceProjectPath,
		"initialize_with_readme": true,
		"default_branch":         "main",
		"visibility":             "public",
	})
	if err == nil {
		var proj map[string]interface{}
		_ = json.Unmarshal(body, &proj)
		return fmt.Sprintf("%v", proj["id"]), fmt.Sprintf("%v", proj["web_url"]), nil
	}

	query := url.QueryEscape(workspaceProjectPath)
	searchURL := fmt.Sprintf("http://127.0.0.1:8080/api/v4/projects?search=%s", query)
	searchBody, searchErr := integrations.GitLabGet(searchURL, token)
	if searchErr != nil {
		return "", "", fmt.Errorf("create project failed and search failed: %w", err)
	}

	var projects []map[string]interface{}
	if jsonErr := json.Unmarshal(searchBody, &projects); jsonErr != nil {
		return "", "", jsonErr
	}

	targetPath := fmt.Sprintf("root/%s", workspaceProjectPath)
	for _, p := range projects {
		if fmt.Sprintf("%v", p["path_with_namespace"]) == targetPath {
			return fmt.Sprintf("%v", p["id"]), fmt.Sprintf("%v", p["web_url"]), nil
		}
	}

	return "", "", fmt.Errorf("project %s not found after create attempt", targetPath)
}

func seedTFDemoFiles(token, projectID string) error {
	mainTF := `terraform {
  required_version = ">= 1.5.0"
}

resource "null_resource" "hello" {
  triggers = {
    message = "hello-from-hal"
  }
}
`

	gitlabCI := `image: hashicorp/terraform:1.9

stages:
  - validate

terraform-validate:
  stage: validate
  script:
    - terraform init -backend=false
    - terraform fmt -check
    - terraform validate
`

	actions := []map[string]string{
		{"action": "create", "file_path": "main.tf", "content": mainTF},
		{"action": "create", "file_path": ".gitlab-ci.yml", "content": gitlabCI},
	}

	_, err := integrations.GitLabPost(
		fmt.Sprintf("http://127.0.0.1:8080/api/v4/projects/%s/repository/commits", projectID),
		token,
		map[string]interface{}{
			"branch":         "main",
			"commit_message": "Add Terraform demo workspace files",
			"actions":        actions,
		},
	)
	if err == nil {
		return nil
	}

	if !strings.Contains(err.Error(), "400") {
		return err
	}

	updateActions := []map[string]string{
		{"action": "update", "file_path": "main.tf", "content": mainTF},
		{"action": "update", "file_path": ".gitlab-ci.yml", "content": gitlabCI},
	}

	_, updateErr := integrations.GitLabPost(
		fmt.Sprintf("http://127.0.0.1:8080/api/v4/projects/%s/repository/commits", projectID),
		token,
		map[string]interface{}{
			"branch":         "main",
			"commit_message": "Update Terraform demo workspace files",
			"actions":        updateActions,
		},
	)

	return updateErr
}

func workspaceSharedConsumerForTarget(target string) string {
	return fmt.Sprintf("terraform-vcs-workflow-%s", target)
}

func isAnyTFERuntimeRunning(engine string) bool {
	if global.IsContainerRunning(engine, "hal-tfe") {
		return true
	}
	layout, err := buildTFETwinLayout()
	if err != nil {
		return false
	}
	return global.IsContainerRunning(engine, layout.CoreContainer)
}

func configureWorkspaceTargetDefaults(cmd *cobra.Command, target string) error {
	if target == tfeTargetTwin {
		layout, err := buildTFETwinLayout()
		if err != nil {
			return err
		}
		if !cmd.Flags().Changed("tfe-url") {
			tfeBaseURL = layout.UIURL
		}
		if !cmd.Flags().Changed("tfe-org") {
			tfeOrgName = tfeTwinOrg
		}
		if !cmd.Flags().Changed("tfe-project") {
			tfeProjectName = tfeTwinProject
		}
		if !cmd.Flags().Changed("tfe-admin-username") {
			tfeAdminUsername = tfeTwinAdminUser
		}
		if !cmd.Flags().Changed("tfe-admin-email") {
			tfeAdminEmail = tfeTwinAdminEmail
		}
		if !cmd.Flags().Changed("tfe-admin-password") {
			tfeAdminPassword = tfeTwinAdminPass
		}
		if !cmd.Flags().Changed("tfe-workspace") {
			tfeWorkspaceName = "tfe-agent-demo-bis"
		}
		if !cmd.Flags().Changed("project-name") {
			workspaceProjectName = "tfe-agent-demo-bis"
		}
		if !cmd.Flags().Changed("project-path") {
			workspaceProjectPath = "tfe-agent-demo-bis"
		}
		return nil
	}

	if !cmd.Flags().Changed("tfe-url") {
		tfeBaseURL = "https://tfe.localhost:8443"
	}
	if !cmd.Flags().Changed("tfe-org") {
		tfeOrgName = "hal"
	}
	if !cmd.Flags().Changed("tfe-project") {
		tfeProjectName = "Dave"
	}
	if !cmd.Flags().Changed("tfe-admin-username") {
		tfeAdminUsername = "haladmin"
	}
	if !cmd.Flags().Changed("tfe-admin-email") {
		tfeAdminEmail = "haladmin@localhost"
	}
	if !cmd.Flags().Changed("tfe-admin-password") {
		tfeAdminPassword = "hal9000FTW"
	}
	if !cmd.Flags().Changed("tfe-workspace") {
		tfeWorkspaceName = "tfe-agent-demo"
	}
	if !cmd.Flags().Changed("project-name") {
		workspaceProjectName = "tfe-agent-demo"
	}
	if !cmd.Flags().Changed("project-path") {
		workspaceProjectPath = "tfe-agent-demo"
	}

	return nil
}

func init() {
	workspaceCmd.Flags().BoolVarP(&workspaceEnable, "enable", "e", false, "Bootstrap or reuse shared GitLab and configure a Terraform VCS demo repository")
	workspaceCmd.Flags().BoolVarP(&workspaceDisable, "disable", "d", false, "Disable Terraform VCS workflow automation and release shared GitLab ownership")
	workspaceCmd.Flags().BoolVarP(&workspaceUpdate, "update", "u", false, "Reconcile existing Terraform VCS workflow automation without full teardown")
	_ = workspaceCmd.Flags().MarkHidden("enable")
	_ = workspaceCmd.Flags().MarkHidden("disable")
	_ = workspaceCmd.Flags().MarkHidden("update")
	workspaceCmd.Flags().BoolVar(&workspaceAutoApprove, "auto-approve", false, "Skip interactive confirmation for destructive disable operations")
	workspaceCmd.Flags().StringVar(&workspaceGitLabVersion, "gitlab-version", "18.10.1-ce.0", "Version of the GitLab CE image used for shared Terraform workspace setup")
	workspaceCmd.Flags().StringVar(&workspaceGitLabPassword, "gitlab-root-password", "hal9000FTW", "Root password used to bootstrap GitLab when HAL starts it")
	workspaceCmd.Flags().StringVar(&workspaceProjectName, "project-name", "tfe-agent-demo", "GitLab project name for the Terraform workspace demo")
	workspaceCmd.Flags().StringVar(&workspaceProjectPath, "project-path", "tfe-agent-demo", "GitLab project path for the Terraform workspace demo")
	workspaceCmd.Flags().StringVar(&tfeOrgName, "tfe-org", "hal", "Terraform Enterprise organization name to bootstrap")
	workspaceCmd.Flags().StringVar(&tfeProjectName, "tfe-project", "Dave", "Terraform Enterprise project name to bootstrap")
	workspaceCmd.Flags().StringVar(&tfeWorkspaceName, "tfe-workspace", "tfe-agent-demo", "Terraform Enterprise workspace name to bootstrap")
	workspaceCmd.Flags().StringVar(&tfeAPIToken, "tfe-api-token", "", "Terraform Enterprise app API token (or set TFE_API_TOKEN)")
	workspaceCmd.Flags().StringVar(&tfeVCSOAuthTokenID, "tfe-vcs-oauth-token-id", "", "Terraform Enterprise VCS OAuth token id for linking the workspace to GitLab (or set TFE_GITLAB_OAUTH_TOKEN_ID)")
	workspaceCmd.Flags().StringVar(&tfeVCSOAuthTokenID, "gitlab-token-id", "", "Alias of --tfe-vcs-oauth-token-id")
	workspaceCmd.Flags().StringVar(&tfeBaseURL, "tfe-url", "https://tfe.localhost:8443", "Terraform Enterprise base URL")
	workspaceCmd.Flags().StringVar(&tfeVCSBranch, "tfe-vcs-branch", "main", "Git branch to trigger VCS runs from (set non-main for tag-focused workflows)")
	workspaceCmd.Flags().StringVar(&tfeAdminUsername, "tfe-admin-username", "haladmin", "Initial TFE admin username used when bootstrapping via IACT")
	workspaceCmd.Flags().StringVar(&tfeAdminEmail, "tfe-admin-email", "haladmin@localhost", "Initial TFE admin email used when bootstrapping via IACT")
	workspaceCmd.Flags().StringVar(&tfeAdminPassword, "tfe-admin-password", "hal9000FTW", "Initial TFE admin password used when bootstrapping via IACT")
	workspaceCmd.Flags().StringVar(&tfeTagsRegex, "tfe-tags-regex", "", "Optional regex for VCS tag-triggered runs (leave empty to disable)")

	bindTFETargetFlag(workspaceCmd)
	for _, name := range []string{
		"gitlab-version",
		"gitlab-root-password",
		"project-name",
		"project-path",
		"tfe-api-token",
		"tfe-vcs-oauth-token-id",
		"gitlab-token-id",
		"tfe-url",
		"tfe-org",
		"tfe-project",
		"tfe-workspace",
		"tfe-vcs-branch",
		"tfe-admin-username",
		"tfe-admin-email",
		"tfe-admin-password",
		"tfe-tags-regex",
	} {
		_ = workspaceCmd.Flags().MarkHidden(name)
	}

	Cmd.AddCommand(workspaceCmd)
}

func buildTFEVCSRepoConfig(repoIdentifier string) map[string]interface{} {
	vcsRepo := map[string]interface{}{
		"identifier":         repoIdentifier,
		"oauth-token-id":     tfeVCSOAuthTokenID,
		"ingress-submodules": false,
	}

	if strings.TrimSpace(tfeVCSBranch) != "" {
		vcsRepo["branch"] = strings.TrimSpace(tfeVCSBranch)
	}

	if strings.TrimSpace(tfeTagsRegex) != "" {
		vcsRepo["tags-regex"] = strings.TrimSpace(tfeTagsRegex)
	}

	return vcsRepo
}

func ensureGitLabCanReachTFEWebhook(engine string) error {
	if !global.IsContainerRunning(engine, "hal-gitlab") || !global.IsContainerRunning(engine, "hal-tfe-proxy") {
		return nil
	}

	proxyIPOut, err := exec.Command(engine, "inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", "hal-tfe-proxy").Output()
	if err != nil {
		return fmt.Errorf("failed to discover hal-tfe-proxy IP: %w", err)
	}
	proxyIP := strings.TrimSpace(string(proxyIPOut))
	if proxyIP == "" {
		return fmt.Errorf("hal-tfe-proxy has no routable container IP")
	}

	// Keep tfe.localhost resolvable from inside GitLab so webhook callbacks reach TFE reliably.
	patchHosts := fmt.Sprintf("grep -v '[[:space:]]tfe.localhost$' /etc/hosts > /tmp/hosts.hal && echo '%s tfe.localhost' >> /tmp/hosts.hal && cat /tmp/hosts.hal > /etc/hosts", proxyIP)
	if out, err := exec.Command(engine, "exec", "-u", "0", "hal-gitlab", "sh", "-lc", patchHosts).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to patch hal-gitlab /etc/hosts: %s", strings.TrimSpace(string(out)))
	}

	return nil
}
