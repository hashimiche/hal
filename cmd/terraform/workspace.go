package terraform

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"hal/internal/global"
	"hal/internal/integrations"

	"github.com/spf13/cobra"
)

var (
	workspaceEnable          bool
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
	workspaceSharedConsumer  = "terraform-workspace"
	workspaceGitLabServiceID = "gitlab"
)

var workspaceCmd = &cobra.Command{
	Use:     "workspace",
	Aliases: []string{"ws"},
	Short:   "Configure a Terraform workspace lab with shared GitLab reuse",
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		if !workspaceEnable {
			fmt.Println("🔍 Checking Terraform workspace scenario prerequisites...")
			if global.IsContainerRunning(engine, "hal-tfe") {
				fmt.Println("  ✅ Terraform Enterprise: running")
			} else {
				fmt.Println("  ❌ Terraform Enterprise: not running")
			}

			if global.IsContainerRunning(engine, "hal-gitlab") {
				fmt.Println("  ✅ Shared GitLab: running")
			} else {
				fmt.Println("  ⚠️  Shared GitLab: not running (will be bootstrapped automatically)")
			}

			fmt.Println("\n💡 Next Step:")
			fmt.Println("   hal terraform workspace --enable")
			return
		}

		if !global.IsContainerRunning(engine, "hal-tfe") {
			fmt.Println("❌ Terraform Enterprise is not running.")
			fmt.Println("   💡 Run 'hal terraform deploy' first.")
			return
		}

		global.EnsureNetwork(engine)

		reused, err := integrations.EnsureGitLabCE(engine, workspaceGitLabVersion, workspaceGitLabPassword)
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}
		if reused {
			fmt.Println("ℹ️  Reusing existing shared GitLab service.")
		} else {
			fmt.Println("🚀 Booted shared GitLab service for Terraform workspace setup.")
		}

		fmt.Println("⏳ Waiting for GitLab API...")
		if err := integrations.WaitForGitLab("http://127.0.0.1:8080", 90); err != nil {
			fmt.Println("❌ GitLab failed to become ready in time.")
			return
		}

		token, err := integrations.GitLabPasswordToken("http://127.0.0.1:8080/oauth/token", "root", workspaceGitLabPassword)
		if err != nil {
			fmt.Printf("❌ Failed to retrieve GitLab API token: %v\n", err)
			return
		}

		if err := ensureGitLabAllowsLocalWebhooks(token); err != nil {
			fmt.Printf("⚠️  Could not relax GitLab local webhook policy automatically: %v\n", err)
			fmt.Println("   💡 TFE may fail to attach VCS webhooks until this is enabled in GitLab application settings.")
		}

		gitlabProjectID, webURL, err := ensureTFDemoProject(token)
		if err != nil {
			fmt.Printf("❌ Failed to prepare Terraform demo repository: %v\n", err)
			return
		}

		if err := seedTFDemoFiles(token, gitlabProjectID); err != nil {
			fmt.Printf("⚠️  Repository exists but demo files were not fully updated: %v\n", err)
		}

		if err := global.AddSharedServiceConsumer(workspaceGitLabServiceID, workspaceSharedConsumer); err != nil {
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
			fmt.Printf("   💡 Login to TFE UI (%s) with %s / %s, create a user token, export TFE_API_TOKEN, then rerun 'hal tf ws -e'.\n", tfeBaseURL, tfeAdminUsername, tfeAdminPassword)
		} else {
			tokenSourceHint = "✅ TFE app API token ready (cached for reuse)."
		}

		if tfeAPIToken != "" && tfeVCSOAuthTokenID == "" {
			fmt.Println("⚙️  Creating GitLab VCS token and wiring Terraform Enterprise OAuth automatically...")
			gitlabPAT, patErr := createGitLabPAT(token)
			if patErr != nil {
				fmt.Printf("⚠️  Could not create GitLab PAT for TFE VCS wiring: %v\n", patErr)
			} else {
				oauthID, oauthErr := ensureTFEGitLabOAuthTokenID(tfeOrgName, gitlabPAT)
				if oauthErr != nil {
					fmt.Printf("⚠️  Could not auto-create TFE GitLab OAuth token id: %v\n", oauthErr)
				} else {
					tfeVCSOAuthTokenID = oauthID
					fmt.Printf("✅ TFE GitLab OAuth token id ready: %s\n", tfeVCSOAuthTokenID)
				}
			}
		}

		if tfeAPIToken == "" {
			fmt.Println("⚠️  Skipping TFE workspace wiring: missing usable TFE API token.")
		} else {
			repoIdentifier := fmt.Sprintf("root/%s", workspaceProjectPath)
			workspaceURL, err := ensureTFEWorkspace(strings.ToLower(tfeOrgName), tfeProjectID, repoIdentifier)
			if err != nil {
				fmt.Printf("⚠️  TFE workspace bootstrap incomplete: %v\n", err)
			} else {
				fmt.Printf("🔗 TFE Workspace: %s\n", workspaceURL)
			}
		}
		if tokenSourceHint != "" {
			fmt.Println(tokenSourceHint)
		}

		fmt.Println("\n✅ Terraform workspace GitLab scenario prepared.")
		fmt.Println("---------------------------------------------------------")
		fmt.Printf("🔗 GitLab Repo: %s\n", webURL)
		fmt.Println("   Login:       root / hal3000FTW")
		fmt.Println("🧭 Next:        Create a Git tag in GitLab to validate the end-to-end VCS-driven auto-apply workflow")
		fmt.Println("---------------------------------------------------------")
	},
}

func ensureTFEWorkspace(orgName, projectID, repoIdentifier string) (string, error) {
	getURL := fmt.Sprintf("%s/api/v2/organizations/%s/workspaces/%s", tfeBaseURL, orgName, tfeWorkspaceName)
	body, status, getErr := integrations.TFERequest("GET", getURL, tfeAPIToken, nil)
	if getErr == nil {
		var existing map[string]interface{}
		_ = json.Unmarshal(body, &existing)
		data, _ := existing["data"].(map[string]interface{})
		workspaceID := fmt.Sprintf("%v", data["id"])

		if tfeVCSOAuthTokenID != "" {
			patchPayload := map[string]interface{}{
				"data": map[string]interface{}{
					"type": "workspaces",
					"id":   workspaceID,
					"attributes": map[string]interface{}{
						"auto-apply": true,
						"vcs-repo": map[string]interface{}{
							"identifier":         repoIdentifier,
							"branch":             "main",
							"oauth-token-id":     tfeVCSOAuthTokenID,
							"ingress-submodules": false,
						},
					},
				},
			}
			patchURL := fmt.Sprintf("%s/api/v2/workspaces/%s", tfeBaseURL, workspaceID)
			patchBody, _, patchErr := integrations.TFERequest("PATCH", patchURL, tfeAPIToken, patchPayload)
			if patchErr != nil {
				return "", fmt.Errorf("workspace exists but VCS update failed: %s", strings.TrimSpace(string(patchBody)))
			}
		}
		return fmt.Sprintf("%s/app/organizations/%s/workspaces/%s", tfeBaseURL, orgName, tfeWorkspaceName), nil
	}
	if status != 404 {
		return "", fmt.Errorf("workspace lookup failed: %s", strings.TrimSpace(string(body)))
	}

	attributes := map[string]interface{}{
		"name":       tfeWorkspaceName,
		"auto-apply": true,
	}

	if tfeVCSOAuthTokenID != "" {
		attributes["vcs-repo"] = map[string]interface{}{
			"identifier":         repoIdentifier,
			"branch":             "main",
			"oauth-token-id":     tfeVCSOAuthTokenID,
			"ingress-submodules": false,
		}
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
		fmt.Println("   💡 Re-run 'hal tf ws -e' after TFE token is available to finish VCS wiring automatically.")
	}

	return fmt.Sprintf("%s/app/organizations/%s/workspaces/%s", tfeBaseURL, orgName, tfeWorkspaceName), nil
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

func ensureTFEGitLabOAuthTokenID(orgName, gitlabToken string) (string, error) {
	org := strings.ToLower(strings.TrimSpace(orgName))
	if org == "" {
		return "", fmt.Errorf("organization name cannot be empty")
	}

	clientID, err := ensureTFEGitLabOAuthClient(org)
	if err != nil {
		return "", err
	}

	if existing := findOrgOAuthTokenForClient(org, clientID); existing != "" {
		return existing, nil
	}

	if err := setOAuthTokenStringOnClient(clientID, gitlabToken); err != nil {
		return "", err
	}

	tokenID := findOrgOAuthTokenForClient(org, clientID)
	if tokenID == "" {
		return "", fmt.Errorf("oauth token was not created after oauth-token-string update")
	}

	return tokenID, nil
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
	for _, item := range data {
		token, _ := item.(map[string]interface{})
		rel, _ := token["relationships"].(map[string]interface{})
		oauthClient, _ := rel["oauth-client"].(map[string]interface{})
		oauthClientData, _ := oauthClient["data"].(map[string]interface{})
		if fmt.Sprintf("%v", oauthClientData["id"]) == clientID {
			return fmt.Sprintf("%v", token["id"])
		}
	}

	return ""
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

func init() {
	workspaceCmd.Flags().BoolVarP(&workspaceEnable, "enable", "e", false, "Bootstrap or reuse shared GitLab and configure a Terraform demo repository")
	workspaceCmd.Flags().StringVar(&workspaceGitLabVersion, "gitlab-version", "18.10.1-ce.0", "Version of the GitLab CE image used for shared Terraform workspace setup")
	workspaceCmd.Flags().StringVar(&workspaceGitLabPassword, "gitlab-root-password", "hal3000FTW", "Root password used to bootstrap GitLab when HAL starts it")
	workspaceCmd.Flags().StringVar(&workspaceProjectName, "project-name", "tfe-agent-demo", "GitLab project name for the Terraform workspace demo")
	workspaceCmd.Flags().StringVar(&workspaceProjectPath, "project-path", "tfe-agent-demo", "GitLab project path for the Terraform workspace demo")
	workspaceCmd.Flags().StringVar(&tfeOrgName, "tfe-org", "hal", "Terraform Enterprise organization name to bootstrap")
	workspaceCmd.Flags().StringVar(&tfeProjectName, "tfe-project", "Dave", "Terraform Enterprise project name to bootstrap")
	workspaceCmd.Flags().StringVar(&tfeWorkspaceName, "tfe-workspace", "tfe-agent-demo", "Terraform Enterprise workspace name to bootstrap")
	workspaceCmd.Flags().StringVar(&tfeAPIToken, "tfe-api-token", "", "Terraform Enterprise app API token (or set TFE_API_TOKEN)")
	workspaceCmd.Flags().StringVar(&tfeVCSOAuthTokenID, "tfe-vcs-oauth-token-id", "", "Terraform Enterprise VCS OAuth token id for linking the workspace to GitLab (or set TFE_GITLAB_OAUTH_TOKEN_ID)")
	workspaceCmd.Flags().StringVar(&tfeVCSOAuthTokenID, "gitlab-token-id", "", "Alias of --tfe-vcs-oauth-token-id")
	workspaceCmd.Flags().StringVar(&tfeBaseURL, "tfe-url", "https://tfe.localhost:8443", "Terraform Enterprise base URL")
	workspaceCmd.Flags().StringVar(&tfeAdminUsername, "tfe-admin-username", "haladmin", "Initial TFE admin username used when bootstrapping via IACT")
	workspaceCmd.Flags().StringVar(&tfeAdminEmail, "tfe-admin-email", "haladmin@localhost", "Initial TFE admin email used when bootstrapping via IACT")
	workspaceCmd.Flags().StringVar(&tfeAdminPassword, "tfe-admin-password", "hal3000FTW", "Initial TFE admin password used when bootstrapping via IACT")

	Cmd.AddCommand(workspaceCmd)
}
