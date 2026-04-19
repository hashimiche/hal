package vault

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hal/internal/global"
	"hal/internal/integrations"

	vault "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
)

var (
	jwtEnable     bool
	jwtDisable    bool
	jwtUpdate     bool
	gitlabVersion string
)

var vaultJwtCmd = &cobra.Command{
	Use:   "jwt [status|enable|disable|update]",
	Short: "Simulate an enterprise Secret Zero CI/CD pipeline with GitLab CE",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := parseLifecycleAction(args, &jwtEnable, &jwtDisable, &jwtUpdate); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		client, vaultErr := GetHealthyClient()

		// ==========================================
		// 1. SMART STATUS MODE (Default behavior)
		// ==========================================
		if !jwtEnable && !jwtDisable && !jwtUpdate {
			fmt.Println("🔍 Checking Vault JWT / GitLab Status...")

			// Check Docker
			gitlabExists := (exec.Command(engine, "inspect", "hal-gitlab").Run() == nil)
			projectExists := false
			runnerActive := false
			if gitlabExists {
				apiToken := getGitLabToken("http://127.0.0.1:8080/oauth/token")
				if apiToken != "" {
					projectExists = gitLabProjectExists(apiToken, "root/secret-zero")
					runnerActive, _ = gitLabHasActiveRunner(apiToken)
				}
			}

			// Check Vault API (if Vault is alive)
			jwtMounted := false
			if vaultErr == nil {
				auths, _ := client.Sys().ListAuth()
				_, jwtMounted = auths["jwt/"]
			}

			// Output Status
			if gitlabExists {
				fmt.Printf("  ✅ GitLab CE     : Active (http://gitlab.localhost:8080)\n")
			} else {
				fmt.Printf("  ❌ GitLab CE     : Not running\n")
			}

			if projectExists {
				fmt.Printf("  ✅ Demo Repo     : Ready (root/secret-zero)\n")
			} else {
				fmt.Printf("  ❌ Demo Repo     : Not configured\n")
			}

			if runnerActive {
				fmt.Printf("  ✅ GitLab Runner : Active\n")
			} else {
				fmt.Printf("  ❌ GitLab Runner : Missing or offline\n")
			}

			if jwtMounted {
				fmt.Printf("  ✅ Vault JWT API : Configured and Mounted\n")
			} else {
				fmt.Printf("  ❌ Vault JWT API : Not configured\n")
			}

			// Smart Assistant Logic
			fmt.Println("\n💡 Next Step:")
			if !gitlabExists && !jwtMounted {
				fmt.Println("   To deploy the GitLab CI/CD environment, run:")
				fmt.Println("   hal vault jwt enable")
			} else if gitlabExists && projectExists && runnerActive && jwtMounted {
				fmt.Println("   Demo is ready! View the pipeline at:")
				fmt.Println("   http://gitlab.localhost:8080/root/secret-zero/-/pipelines")
				fmt.Println("\n   To completely remove this demo environment, run:")
				fmt.Println("   hal vault jwt disable")
			} else {
				fmt.Println("   Environment is partially degraded. To safely reset, run:")
				fmt.Println("   hal vault jwt update")
			}
			return
		}

		// ==========================================
		// 2. TEARDOWN / RESET PATH (--disable / --update)
		// ==========================================
		if jwtDisable || jwtUpdate {
			if global.DryRun {
				fmt.Println("[DRY RUN] Would execute: docker rm -f hal-gitlab hal-gitlab-runner")
				fmt.Println("[DRY RUN] Would call API to disable: auth/jwt and kv-jwt")
			} else {
				if jwtDisable {
					fmt.Println("🛑 Tearing down GitLab CI/CD environment...")
				} else {
					fmt.Println("♻️  Update requested. Destroying environment for reset...")
				}

				if vaultErr == nil && client != nil {
					_ = client.Sys().DisableAuth("jwt")
					_ = client.Sys().Unmount("kv-jwt")
					_ = client.Sys().DeletePolicy("cicd-read")
					// We don't delete the root identity as it might be used by other things.
				}

				_ = exec.Command(engine, "rm", "-f", "hal-gitlab", "hal-gitlab-runner").Run()
				_ = global.ClearSharedService("gitlab")
				fmt.Println("✅ GitLab containers removed and Vault API cleaned up.")
			}

			if jwtDisable && !global.DryRun {
				return
			}
		}

		// ==========================================
		// 3. DEPLOY / ENABLE PATH (--enable / --update)
		// ==========================================
		if jwtEnable || jwtUpdate {
			if vaultErr != nil {
				fmt.Printf("❌ Cannot deploy: Vault must be running and healthy. %v\n", vaultErr)
				return
			}

			global.WarnIfEngineResourcesTight(engine, "vault-jwt")
			if !global.DryRun {
				proceed, err := global.ConfirmScenarioProceed(engine, "vault-jwt")
				if err != nil && global.Debug {
					fmt.Printf("[DEBUG] Capacity confirmation unavailable: %v\n", err)
				}
				if err == nil && !proceed {
					fmt.Printf("🛑 Vault JWT deployment aborted to protect your %s engine.\n", engine)
					return
				}
			}

			if global.DryRun {
				fmt.Println("[DRY RUN] Would boot or reuse shared GitLab.")
				fmt.Println("[DRY RUN] Would call GitLab APIs to configure project and pipeline.")
				fmt.Println("[DRY RUN] Would configure Vault JWT auth method.")
				return
			}

			global.EnsureNetwork(engine)

			reused, err := integrations.EnsureGitLabCE(engine, gitlabVersion, "hal9000FTW")
			if err != nil {
				fmt.Printf("❌ %v\n", err)
				return
			}
			if reused {
				fmt.Println("ℹ️  Reusing existing GitLab CE shared service.")
			} else {
				fmt.Printf("🚀 Booted GitLab CE shared service (gitlab/gitlab-ce:%s).\n", gitlabVersion)
			}

			fmt.Println("⏳ Waiting for GitLab API...")
			if err := waitForGitLab("http://127.0.0.1:8080", 90); err != nil {
				fmt.Println("\n❌ GitLab failed to initialize within the time limit.")
				return
			}
			fmt.Println("\n✅ GitLab API is online!")

			// 1. Authenticate via OAuth
			fmt.Println("⚙️  Authenticating root account via API...")
			apiToken := getGitLabToken("http://127.0.0.1:8080/oauth/token")
			if apiToken == "" {
				fmt.Println("❌ Failed to retrieve GitLab API token.")
				return
			}

			// 2. Create the Project
			fmt.Println("⚙️  Ensuring 'secret-zero' repository exists...")
			projectID, err := ensureJWTProject(apiToken)
			if err != nil {
				fmt.Printf("❌ Failed to prepare GitLab repository: %v\n", err)
				return
			}

			fmt.Println("⚙️  Validating GitLab Runner availability...")
			if err := ensureJWTGitLabRunner(engine, apiToken, projectID); err != nil {
				fmt.Printf("❌ %v\n", err)
				fmt.Println("   💡 GitLab project and Vault config are ready, but pipeline execution requires at least one active runner.")
				fmt.Println("   💡 Re-run 'hal vault jwt enable' after fixing runner connectivity.")
				return
			}

			// Protect tags
			fmt.Println("🔒 Applying security guardrails: Protecting 'v*' tags...")
			_ = gitlabPost(fmt.Sprintf("http://127.0.0.1:8080/api/v4/projects/%s/protected_tags", projectID), apiToken, map[string]interface{}{
				"name":                "v*",
				"create_access_level": 40, // 40 = Maintainer/Admin level
			})

			// 3. Configure Vault
			fmt.Println("🛡️  Configuring Vault JWT Auth and strict Tag Policies...")

			_ = client.Sys().Mount("kv-jwt", &vault.MountInput{
				Type:    "kv",
				Options: map[string]string{"version": "2"},
			})

			_, _ = client.Logical().Write("kv-jwt/data/cicd", map[string]interface{}{
				"data": map[string]interface{}{"secret": "zero"},
			})

			_ = client.Sys().PutPolicy("cicd-read", `path "kv-jwt/data/cicd" { capabilities = ["read"] }`)
			_ = client.Sys().EnableAuthWithOptions("jwt", &vault.EnableAuthOptions{Type: "jwt"})
			gitlabOIDC := integrations.GitLabCE("http://gitlab.localhost:8080")

			_, _ = client.Logical().Write("auth/jwt/config", map[string]interface{}{
				"jwks_url":     gitlabOIDC.JWKSURL,
				"bound_issuer": gitlabOIDC.Issuer,
			})

			_, _ = client.Logical().Write("auth/jwt/role/cicd-role", map[string]interface{}{
				"role_type":         "jwt",
				"user_claim":        "user_login",
				"bound_audiences":   []string{"vault"},
				"bound_claims_type": "glob",
				"bound_claims": map[string]interface{}{
					"project_path": "root/secret-zero",
					"ref":          "v*",
				},
				"token_policies": []string{"cicd-read"},
			})

			// 4. Commit the Pipeline
			fmt.Println("🤖 Automating GitOps: Pushing pipeline YAML to trigger run...")

			pipelineYAML := `vault-auth:
	tags:
		- hal-jwt-shell
  id_tokens:
    VAULT_ID_TOKEN:
      aud: vault
  script:
    - |
      echo "⚙️ Presenting JWT to Vault..."
      AUTH_PAYLOAD=$(jq -n --arg jwt "$VAULT_ID_TOKEN" '{"role": "cicd-role", "jwt": $jwt}')
      VAULT_RESPONSE=$(curl -s -X POST -d "$AUTH_PAYLOAD" http://hal-vault:8200/v1/auth/jwt/login)
      VAULT_TOKEN=$(echo $VAULT_RESPONSE | jq -r .auth.client_token)
      
      if [ "$VAULT_TOKEN" == "null" ] || [ -z "$VAULT_TOKEN" ]; then
        echo "❌ Vault authentication failed! (Bound claims mismatch?)"
        echo "Vault API Response: $VAULT_RESPONSE"
        exit 1
      fi
      
      echo "✅ Authentication successful. Fetching secret..."
      SECRET_RESPONSE=$(curl -s -H "X-Vault-Token: $VAULT_TOKEN" http://hal-vault:8200/v1/kv-jwt/data/cicd)
      SECRET_VALUE=$(echo $SECRET_RESPONSE | jq -r .data.data.secret)
      
      echo "✅ SUCCESS! The secret retrieved is: $SECRET_VALUE"
`
			pipelineYAML = strings.ReplaceAll(pipelineYAML, "\t", "  ")

			if err := upsertJWTProjectFiles(projectID, apiToken, pipelineYAML); err != nil {
				fmt.Printf("⚠️  Repository exists but pipeline files were not fully updated: %v\n", err)
			}

			fmt.Println("\n✅ Enterprise Secret Zero Environment Ready!")
			fmt.Println("---------------------------------------------------------")
			fmt.Println("🔗 GitLab UI:    http://gitlab.localhost:8080/root/secret-zero/-/pipelines")
			fmt.Println("   Login:        root / hal9000FTW")
			fmt.Println("\n💡 THE DEMO WORKFLOW:")
			fmt.Println("   1. The repository and pipeline were created or updated quickly against the shared GitLab instance.")
			fmt.Println("   2. Vault JWT auth is bound to protected tags matching 'v*'.")
			fmt.Println("   3. Create a tag such as 'v1.0.0' in GitLab to validate the Vault-bound claims path.")
			fmt.Println("   4. If you want branch-triggered success too, adjust the Vault JWT role bound claims.")
			fmt.Println("---------------------------------------------------------")
			if err := global.AddSharedServiceConsumer("gitlab", "vault-jwt"); err != nil {
				fmt.Printf("⚠️  Could not persist shared ownership metadata: %v\n", err)
			}
		}
	},
}

// -----------------------------------------------------------------------------
// Helper Functions (Kept exactly as you wrote them)
// -----------------------------------------------------------------------------

func waitForGitLab(baseURL string, maxRetries int) error {
	for i := 0; i < maxRetries; i++ {
		if err := integrations.WaitForGitLab(baseURL, 1); err == nil {
			return nil
		}
		fmt.Print(".")
	}
	return fmt.Errorf("timeout")
}

func getGitLabToken(urlStr string) string {
	token, err := integrations.GitLabPasswordToken(urlStr, "root", "hal9000FTW")
	if err == nil {
		return token
	}
	return ""
}

func gitlabPost(urlStr string, token string, payload map[string]interface{}) []byte {
	body, err := integrations.GitLabPost(urlStr, token, payload)
	if err != nil {
		return nil
	}
	return body
}

func gitLabProjectExists(token, repoPath string) bool {
	projectID, err := findJWTProjectID(token, repoPath)
	return err == nil && projectID != ""
}

func ensureJWTProject(token string) (string, error) {
	if existingID, err := findJWTProjectID(token, "root/secret-zero"); err == nil && existingID != "" {
		return existingID, nil
	}

	projectResp, err := integrations.GitLabPost("http://127.0.0.1:8080/api/v4/projects", token, map[string]interface{}{
		"name":                   "secret-zero",
		"initialize_with_readme": true,
		"default_branch":         "main",
		"visibility":             "public",
	})
	if err != nil {
		if existingID, lookupErr := findJWTProjectID(token, "root/secret-zero"); lookupErr == nil && existingID != "" {
			return existingID, nil
		}
		return "", err
	}

	var proj map[string]interface{}
	if err := json.Unmarshal(projectResp, &proj); err != nil {
		return "", err
	}

	projectID := fmt.Sprintf("%v", proj["id"])
	if projectID == "<nil>" || projectID == "" {
		return "", fmt.Errorf("gitlab project response did not include id")
	}

	return projectID, nil
}

func findJWTProjectID(token, repoPath string) (string, error) {
	searchURL := fmt.Sprintf("http://127.0.0.1:8080/api/v4/projects?search=%s", url.QueryEscape("secret-zero"))
	body, err := integrations.GitLabGet(searchURL, token)
	if err != nil {
		return "", err
	}

	var projects []map[string]interface{}
	if err := json.Unmarshal(body, &projects); err != nil {
		return "", err
	}

	for _, project := range projects {
		pathWithNamespace := fmt.Sprintf("%v", project["path_with_namespace"])
		if pathWithNamespace == repoPath {
			return fmt.Sprintf("%v", project["id"]), nil
		}
	}

	return "", nil
}

func upsertJWTProjectFiles(projectID, token, pipelineYAML string) error {
	actions := []map[string]string{{
		"action":    "create",
		"file_path": ".gitlab-ci.yml",
		"content":   pipelineYAML,
	}}

	if err := commitJWTProjectFiles(projectID, token, "Add Vault pipeline", actions); err == nil {
		return nil
	}

	actions[0]["action"] = "update"
	return commitJWTProjectFiles(projectID, token, "Update Vault pipeline", actions)
}

func commitJWTProjectFiles(projectID, token, message string, actions []map[string]string) error {
	_, err := integrations.GitLabPost(
		fmt.Sprintf("http://127.0.0.1:8080/api/v4/projects/%s/repository/commits", projectID),
		token,
		map[string]interface{}{
			"branch":         "main",
			"commit_message": message,
			"actions":        actions,
		},
	)
	return err
}

func gitLabHasActiveRunner(token string) (bool, error) {
	body, err := integrations.GitLabGet("http://127.0.0.1:8080/api/v4/runners/all?status=online", token)
	if err != nil {
		return false, err
	}

	var runners []map[string]interface{}
	if err := json.Unmarshal(body, &runners); err != nil {
		return false, err
	}

	for _, runner := range runners {
		active, _ := runner["active"].(bool)
		online, _ := runner["online"].(bool)
		if active && online {
			return true, nil
		}
	}

	return false, nil
}

func ensureJWTGitLabRunner(engine, token, projectID string) error {
	const managedRunnerDesc = "hal-jwt-runner-shell"
	homeDir, _ := os.UserHomeDir()
	runnerConfigDir := filepath.Join(homeDir, ".hal", "gitlab-runner")

	if active, err := gitLabHasActiveRunnerByDescription(token, managedRunnerDesc); err == nil && active && runnerConfigUsesInternalGitLab(engine) && runnerHasJWTTooling(engine) {
		fmt.Println("   ✅ HAL shell runner is already active for this demo.")
		return nil
	}

	if global.IsContainerRunning(engine, "hal-gitlab-runner") {
		fmt.Println("   ♻️  Reconfiguring HAL runner to use internal GitLab network URL...")
		_ = exec.Command(engine, "rm", "-f", "hal-gitlab-runner").Run()
		_ = os.RemoveAll(runnerConfigDir)
	}

	runnerToken, err := createJWTRunnerAuthToken(token)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(runnerConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to prepare runner config directory: %w", err)
	}

	if !global.IsContainerRunning(engine, "hal-gitlab-runner") {
		gitlabIP, ipErr := gitlabContainerIP(engine)
		if ipErr != nil {
			return ipErr
		}

		runnerArgs := []string{
			"run", "-d",
			"--name", "hal-gitlab-runner",
			"--network", "hal-net",
			"--add-host", fmt.Sprintf("gitlab.localhost:%s", gitlabIP),
			"-v", fmt.Sprintf("%s:/etc/gitlab-runner", runnerConfigDir),
			"gitlab/gitlab-runner:alpine",
		}

		if out, err := exec.Command(engine, runnerArgs...).CombinedOutput(); err != nil {
			return fmt.Errorf("failed to start gitlab runner container: %s", strings.TrimSpace(string(out)))
		}
	}

	if err := ensureJWTToolingInRunner(engine); err != nil {
		return err
	}

	registerArgs := []string{
		"exec", "hal-gitlab-runner",
		"gitlab-runner", "register", "--non-interactive",
		"--url", "http://hal-gitlab:8080",
		"--clone-url", "http://hal-gitlab:8080",
		"--token", runnerToken,
		"--executor", "shell",
		"--description", managedRunnerDesc,
	}

	if out, err := exec.Command(engine, registerArgs...).CombinedOutput(); err != nil {
		outStr := strings.TrimSpace(string(out))
		if !strings.Contains(strings.ToLower(outStr), "already") {
			return fmt.Errorf("failed to register gitlab runner: %s", outStr)
		}
	}

	for i := 0; i < 20; i++ {
		active, err := gitLabHasActiveRunnerByDescription(token, managedRunnerDesc)
		if err == nil && active {
			fmt.Println("   ✅ GitLab runner is active and ready for pipeline jobs.")
			return nil
		}
		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("runner registration attempted but no active runner appeared in GitLab")
}

func ensureJWTToolingInRunner(engine string) error {
	installCmd := exec.Command(
		engine,
		"exec",
		"-u",
		"0",
		"hal-gitlab-runner",
		"sh",
		"-lc",
		"apk add --no-cache jq curl bash >/dev/null",
	)
	if out, err := installCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to install runner tooling (jq/curl/bash): %s", strings.TrimSpace(string(out)))
	}

	if !runnerHasJWTTooling(engine) {
		return fmt.Errorf("gitlab runner container still lacks required tooling after installation")
	}

	return nil
}

func runnerHasJWTTooling(engine string) bool {
	if !global.IsContainerRunning(engine, "hal-gitlab-runner") {
		return false
	}
	out, err := exec.Command(engine, "exec", "hal-gitlab-runner", "sh", "-lc", "command -v jq >/dev/null 2>&1 && command -v curl >/dev/null 2>&1 && command -v bash >/dev/null 2>&1").CombinedOutput()
	if err != nil {
		_ = out
		return false
	}
	return true
}

func gitlabContainerIP(engine string) (string, error) {
	out, err := exec.Command(engine, "inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", "hal-gitlab").Output()
	if err != nil {
		return "", fmt.Errorf("failed to inspect hal-gitlab container IP: %w", err)
	}
	ip := strings.TrimSpace(string(out))
	if ip == "" {
		return "", fmt.Errorf("hal-gitlab does not have a network IP yet")
	}
	return ip, nil
}

func createJWTRunnerAuthToken(token string) (string, error) {
	body, err := integrations.GitLabPost("http://127.0.0.1:8080/api/v4/user/runners", token, map[string]interface{}{
		"runner_type":  "instance_type",
		"description":  "hal-jwt-runner-shell",
		"run_untagged": false,
		"tag_list":     []string{"hal-jwt-shell", "secret-zero"},
	})
	if err != nil {
		return "", fmt.Errorf("failed to create runner token via GitLab API: %w", err)
	}

	var runnerInfo map[string]interface{}
	if err := json.Unmarshal(body, &runnerInfo); err != nil {
		return "", fmt.Errorf("failed to parse runner token response: %w", err)
	}

	runnerToken := strings.TrimSpace(fmt.Sprintf("%v", runnerInfo["token"]))
	if runnerToken == "" || runnerToken == "<nil>" {
		return "", fmt.Errorf("gitlab runner API response did not include token")
	}

	return runnerToken, nil
}

func runnerConfigUsesInternalGitLab(engine string) bool {
	if !global.IsContainerRunning(engine, "hal-gitlab-runner") {
		return false
	}
	out, err := exec.Command(engine, "exec", "hal-gitlab-runner", "sh", "-lc", "cat /etc/gitlab-runner/config.toml").CombinedOutput()
	if err != nil {
		return false
	}
	cfg := string(out)
	return strings.Contains(cfg, `url = "http://hal-gitlab:8080"`) && strings.Contains(cfg, `clone_url = "http://hal-gitlab:8080"`)
}

func gitLabHasActiveRunnerByDescription(token, desc string) (bool, error) {
	body, err := integrations.GitLabGet("http://127.0.0.1:8080/api/v4/runners/all?status=online", token)
	if err != nil {
		return false, err
	}

	var runners []map[string]interface{}
	if err := json.Unmarshal(body, &runners); err != nil {
		return false, err
	}

	for _, runner := range runners {
		active, _ := runner["active"].(bool)
		online, _ := runner["online"].(bool)
		description := fmt.Sprintf("%v", runner["description"])
		if active && online && strings.Contains(description, desc) {
			return true, nil
		}
	}

	return false, nil
}

func init() {
	// 1. Standard Lifecycle Flags
	vaultJwtCmd.Flags().BoolVarP(&jwtEnable, "enable", "e", false, "Deploy GitLab CE and configure Vault JWT")
	vaultJwtCmd.Flags().BoolVarP(&jwtDisable, "disable", "d", false, "Remove GitLab CE and strip JWT from Vault")
	vaultJwtCmd.Flags().BoolVarP(&jwtUpdate, "update", "u", false, "Reconcile GitLab/Vault JWT integration settings")
	_ = vaultJwtCmd.Flags().MarkHidden("enable")
	_ = vaultJwtCmd.Flags().MarkHidden("disable")
	_ = vaultJwtCmd.Flags().MarkHidden("update")

	// 2. Feature-Specific Flags
	vaultJwtCmd.Flags().StringVar(&gitlabVersion, "gitlab-version", "18.10.1-ce.0", "Version of the GitLab CE container image to deploy")

	Cmd.AddCommand(vaultJwtCmd)
}
