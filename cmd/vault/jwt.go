package vault

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"time"

	"hal/internal/global"

	vault "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
)

var (
	jwtEnable     bool
	jwtDisable    bool
	jwtForce      bool
	gitlabVersion string
)

var vaultJwtCmd = &cobra.Command{
	Use:   "jwt",
	Short: "Simulate an enterprise Secret Zero CI/CD pipeline with GitLab CE",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		client, vaultErr := GetHealthyClient()

		// ==========================================
		// 1. SMART STATUS MODE (Default behavior)
		// ==========================================
		if !jwtEnable && !jwtDisable && !jwtForce {
			fmt.Println("🔍 Checking Vault JWT / GitLab Status...")

			// Check Docker
			gitlabExists := (exec.Command(engine, "inspect", "hal-gitlab").Run() == nil)
			runnerExists := (exec.Command(engine, "inspect", "hal-gitlab-runner").Run() == nil)

			// Check Vault API (if Vault is alive)
			jwtMounted := false
			if vaultErr == nil {
				auths, _ := client.Sys().ListAuth()
				_, jwtMounted = auths["jwt/"]
			}

			// Output Status
			if gitlabExists {
				fmt.Printf("  ✅ GitLab CE     : Active (http://127.0.0.1:8080)\n")
			} else {
				fmt.Printf("  ❌ GitLab CE     : Not running\n")
			}

			if runnerExists {
				fmt.Printf("  ✅ GitLab Runner : Active\n")
			} else {
				fmt.Printf("  ❌ GitLab Runner : Not running\n")
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
				fmt.Println("   hal vault jwt --enable")
			} else if gitlabExists && runnerExists && jwtMounted {
				fmt.Println("   Demo is ready! View the pipeline at:")
				fmt.Println("   http://127.0.0.1:8080/root/secret-zero/-/pipelines")
				fmt.Println("\n   To completely remove this demo environment, run:")
				fmt.Println("   hal vault jwt --disable")
			} else {
				fmt.Println("   Environment is partially degraded. To safely reset, run:")
				fmt.Println("   hal vault jwt --force")
			}
			return
		}

		// ==========================================
		// 2. TEARDOWN / RESET PATH (--disable / --force)
		// ==========================================
		if jwtDisable || jwtForce {
			if global.DryRun {
				fmt.Println("[DRY RUN] Would execute: docker rm -f hal-gitlab hal-gitlab-runner")
				fmt.Println("[DRY RUN] Would call API to disable: auth/jwt and kv-jwt")
			} else {
				if jwtDisable {
					fmt.Println("🛑 Tearing down GitLab CI/CD environment...")
				} else {
					fmt.Println("♻️  Force flag detected. Destroying environment for reset...")
				}

				_ = exec.Command(engine, "rm", "-f", "hal-gitlab", "hal-gitlab-runner").Run()

				if vaultErr == nil && client != nil {
					_ = client.Sys().DisableAuth("jwt")
					_ = client.Sys().Unmount("kv-jwt")
					_ = client.Sys().DeletePolicy("cicd-read")
					// We don't delete the root identity as it might be used by other things.
				}
				fmt.Println("✅ GitLab containers removed and Vault API cleaned up.")
			}

			if jwtDisable && !global.DryRun {
				return
			}
		}

		// ==========================================
		// 3. DEPLOY / ENABLE PATH (--enable / --force)
		// ==========================================
		if jwtEnable || jwtForce {
			if vaultErr != nil {
				fmt.Printf("❌ Cannot deploy: Vault must be running and healthy. %v\n", vaultErr)
				return
			}

			if global.DryRun {
				fmt.Println("[DRY RUN] Would execute Docker run commands for GitLab and Runner.")
				fmt.Println("[DRY RUN] Would call GitLab APIs to configure project and pipeline.")
				fmt.Println("[DRY RUN] Would configure Vault JWT auth method.")
				return
			}

			fmt.Printf("🚀 Booting GitLab CE (gitlab/gitlab-ce:%s)...\n", gitlabVersion)

			// Docker cleanup just in case (though force should have handled it)
			_ = exec.Command(engine, "rm", "-f", "hal-gitlab", "hal-gitlab-runner").Run()

			gitlabArgs := []string{
				"run", "-d", "--name", "hal-gitlab",
				"--network", "hal-net",
				"--network-alias", "gitlab.localhost",
				"-p", "8080:8080",
				"--shm-size", "256m",
				"--privileged",
				"-e", "GITLAB_OMNIBUS_CONFIG=external_url 'http://gitlab.localhost:8080'; puma['port'] = 8081; gitlab_rails['initial_root_password'] = 'halpassword';",
				fmt.Sprintf("gitlab/gitlab-ce:%s", gitlabVersion),
			}

			// Note: Removed --platform linux/arm64. If you need it specifically for M1/M2 macs,
			// you should pass it as a flag or detect architecture, but hardcoding it breaks linux/amd64 users.
			if err := exec.Command(engine, gitlabArgs...).Run(); err != nil {
				fmt.Printf("❌ Failed to start GitLab: %v\n", err)
				return
			}

			fmt.Println("⏳ Waiting for GitLab to initialize (This usually takes 3-5 minutes)...")
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
			fmt.Println("⚙️  Creating 'secret-zero' repository...")
			projectResp := gitlabPost("http://127.0.0.1:8080/api/v4/projects", apiToken, map[string]interface{}{
				"name":                   "secret-zero",
				"initialize_with_readme": true,
				"default_branch":         "main",
				"visibility":             "public",
			})
			var proj map[string]interface{}
			json.Unmarshal(projectResp, &proj)
			projectID := fmt.Sprintf("%v", proj["id"])

			// Protect tags
			fmt.Println("🔒 Applying security guardrails: Protecting 'v*' tags...")
			gitlabPost(fmt.Sprintf("http://127.0.0.1:8080/api/v4/projects/%s/protected_tags", projectID), apiToken, map[string]interface{}{
				"name":                "v*",
				"create_access_level": 40, // 40 = Maintainer/Admin level
			})

			// 3. Create and Register the Instance Runner
			fmt.Println("⚙️  Provisioning GitLab Runner...")
			runnerResp := gitlabPost("http://127.0.0.1:8080/api/v4/user/runners", apiToken, map[string]interface{}{
				"runner_type":  "instance_type",
				"description":  "hal-runner",
				"run_untagged": true,
			})
			var runInfo map[string]interface{}
			json.Unmarshal(runnerResp, &runInfo)
			runnerToken := runInfo["token"].(string)

			runnerArgs := []string{
				"run", "-d", "--name", "hal-gitlab-runner",
				"--network", "hal-net",
				"gitlab/gitlab-runner:alpine",
			}
			_ = exec.Command(engine, runnerArgs...).Run()

			// Install dependencies inside runner
			fmt.Println("⚙️  Installing CI dependencies (curl, jq) inside runner...")
			_ = exec.Command(engine, "exec", "-u", "root", "hal-gitlab-runner", "apk", "add", "--no-cache", "curl", "jq").Run()

			// Register the runner
			_ = exec.Command(engine, "exec", "hal-gitlab-runner", "gitlab-runner", "register",
				"--non-interactive",
				"--url", "http://gitlab.localhost:8080",
				"--token", runnerToken,
				"--executor", "shell",
				"--clone-url", "http://hal-gitlab:8080",
			).Run()

			// 4. Configure Vault
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

			_, _ = client.Logical().Write("auth/jwt/config", map[string]interface{}{
				"jwks_url":     "http://gitlab.localhost:8080/oauth/discovery/keys",
				"bound_issuer": "http://gitlab.localhost:8080",
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

			// 5. Commit the Pipeline
			fmt.Println("🤖 Automating GitOps: Pushing pipeline YAML to trigger run...")

			pipelineYAML := `vault-auth:
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

			gitlabPost(fmt.Sprintf("http://127.0.0.1:8080/api/v4/projects/%s/repository/commits", projectID), apiToken, map[string]interface{}{
				"branch":         "main",
				"commit_message": "Add Vault pipeline",
				"actions": []map[string]string{
					{
						"action":    "create",
						"file_path": ".gitlab-ci.yml",
						"content":   pipelineYAML,
					},
				},
			})

			fmt.Println("\n✅ Enterprise Secret Zero Environment Ready!")
			fmt.Println("---------------------------------------------------------")
			fmt.Println("🔗 GitLab UI:    http://127.0.0.1:8080/root/secret-zero/-/pipelines")
			fmt.Println("   Login:        root / halpassword")
			fmt.Println("\n💡 THE DEMO WORKFLOW:")
			fmt.Println("   1. A pipeline just automatically triggered on the 'main' branch.")
			fmt.Println("   2. Check the logs: It FAILED because Vault rejected the JWT claims.")
			fmt.Println("   3. Go to Code -> Tags, and create a new tag (e.g., 'v1.0.0' or 'v2').")
			fmt.Println("      🔒 Note: Tags starting with 'v*' are strictly protected. Only Admins can create them!")
			fmt.Println("   4. Watch the new pipeline run. It will SUCCEED and print the secret!")
			fmt.Println("---------------------------------------------------------")
		}
	},
}

// -----------------------------------------------------------------------------
// Helper Functions (Kept exactly as you wrote them)
// -----------------------------------------------------------------------------

func waitForGitLab(baseURL string, maxRetries int) error {
	client := http.Client{Timeout: 3 * time.Second}
	for i := 0; i < maxRetries; i++ {
		resp, err := client.Get(baseURL + "/users/sign_in")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return nil
		}
		fmt.Print(".")
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("timeout")
}

func getGitLabToken(urlStr string) string {
	client := http.Client{Timeout: 5 * time.Second}
	for i := 0; i < 5; i++ {
		resp, err := client.PostForm(urlStr, url.Values{
			"grant_type": {"password"},
			"username":   {"root"},
			"password":   {"halpassword"},
		})
		if err == nil && resp.StatusCode == 200 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			var result map[string]interface{}
			json.Unmarshal(body, &result)
			return result["access_token"].(string)
		}
		time.Sleep(5 * time.Second)
	}
	return ""
}

func gitlabPost(urlStr string, token string, payload map[string]interface{}) []byte {
	jsonData, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", urlStr, bytes.NewBuffer(jsonData))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body
}

func init() {
	// 1. Standard Lifecycle Flags
	vaultJwtCmd.Flags().BoolVarP(&jwtEnable, "enable", "e", false, "Deploy GitLab CE and configure Vault JWT")
	vaultJwtCmd.Flags().BoolVarP(&jwtDisable, "disable", "d", false, "Remove GitLab CE and strip JWT from Vault")
	vaultJwtCmd.Flags().BoolVarP(&jwtForce, "force", "f", false, "Force a clean redeployment of the entire environment")

	// 2. Feature-Specific Flags
	vaultJwtCmd.Flags().StringVar(&gitlabVersion, "gitlab-version", "16.11.2-ce.0", "Version of the GitLab CE container image to deploy")

	Cmd.AddCommand(vaultJwtCmd)
}
