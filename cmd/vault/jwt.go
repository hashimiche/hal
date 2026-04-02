package vault

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"hal/internal/global"
	"hal/internal/integrations"

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
				fmt.Printf("  ✅ GitLab CE     : Active (http://gitlab.localhost:8080)\n")
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
				fmt.Println("   http://gitlab.localhost:8080/root/secret-zero/-/pipelines")
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

				if vaultErr == nil && client != nil {
					_ = client.Sys().DisableAuth("jwt")
					_ = client.Sys().Unmount("kv-jwt")
					_ = client.Sys().DeletePolicy("cicd-read")
					// We don't delete the root identity as it might be used by other things.
				}

				if jwtForce {
					_ = exec.Command(engine, "rm", "-f", "hal-gitlab", "hal-gitlab-runner").Run()
					_ = global.ClearSharedService("gitlab")
					fmt.Println("✅ GitLab containers removed and Vault API cleaned up.")
				} else {
					remaining, err := global.RemoveSharedServiceConsumer("gitlab", "vault-jwt")
					if err != nil {
						fmt.Printf("⚠️  Could not update shared service ownership metadata: %v\n", err)
					}
					if len(remaining) == 0 {
						_ = exec.Command(engine, "rm", "-f", "hal-gitlab", "hal-gitlab-runner").Run()
						fmt.Println("✅ GitLab containers removed and Vault API cleaned up.")
					} else {
						fmt.Printf("✅ Vault JWT configuration removed. Reused GitLab remains active (in use by: %s).\n", strings.Join(remaining, ", "))
					}
				}
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

			gitlabRunning := global.IsContainerRunning(engine, "hal-gitlab")
			runnerRunning := global.IsContainerRunning(engine, "hal-gitlab-runner")

			if !gitlabRunning {
				gitlabArgs := []string{
					"run", "-d", "--name", "hal-gitlab",
					"--network", "hal-net",
					"--network-alias", "gitlab.localhost",
					"-p", "8080:8080",
					"--shm-size", "256m",
					"--privileged",
					"-e", "GITLAB_OMNIBUS_CONFIG=external_url 'http://gitlab.localhost:8080'; nginx['listen_port'] = 8080; nginx['listen_addresses'] = ['0.0.0.0', '[::]']; puma['port'] = 8081; gitlab_rails['initial_root_password'] = 'hal3000FTW';",
					fmt.Sprintf("gitlab/gitlab-ce:%s", gitlabVersion),
				}

				if err := exec.Command(engine, gitlabArgs...).Run(); err != nil {
					fmt.Printf("❌ Failed to start GitLab: %v\n", err)
					return
				}
			} else {
				fmt.Println("ℹ️  Reusing existing GitLab CE shared service.")
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
			if !runnerRunning {
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

				fmt.Println("⚙️  Installing CI dependencies (curl, jq) inside runner...")
				_ = exec.Command(engine, "exec", "-u", "root", "hal-gitlab-runner", "apk", "add", "--no-cache", "curl", "jq").Run()

				_ = exec.Command(engine, "exec", "hal-gitlab-runner", "gitlab-runner", "register",
					"--non-interactive",
					"--url", "http://gitlab.localhost:8080",
					"--token", runnerToken,
					"--executor", "shell",
					"--clone-url", "http://hal-gitlab:8080",
				).Run()
			} else {
				fmt.Println("ℹ️  Reusing existing GitLab Runner shared service.")
			}

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
			fmt.Println("🔗 GitLab UI:    http://gitlab.localhost:8080/root/secret-zero/-/pipelines")
			fmt.Println("   Login:        root / hal3000FTW")
			fmt.Println("\n💡 THE DEMO WORKFLOW:")
			fmt.Println("   1. A pipeline just automatically triggered on the 'main' branch.")
			fmt.Println("   2. Check the logs: It FAILED because Vault rejected the JWT claims.")
			fmt.Println("   3. Go to Code -> Tags, and create a new tag (e.g., 'v1.0.0' or 'v2').")
			fmt.Println("      🔒 Note: Tags starting with 'v*' are strictly protected. Only Admins can create them!")
			fmt.Println("   4. Watch the new pipeline run. It will SUCCEED and print the secret!")
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
	token, err := integrations.GitLabPasswordToken(urlStr, "root", "hal3000FTW")
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

func init() {
	// 1. Standard Lifecycle Flags
	vaultJwtCmd.Flags().BoolVarP(&jwtEnable, "enable", "e", false, "Deploy GitLab CE and configure Vault JWT")
	vaultJwtCmd.Flags().BoolVarP(&jwtDisable, "disable", "d", false, "Remove GitLab CE and strip JWT from Vault")
	vaultJwtCmd.Flags().BoolVarP(&jwtForce, "force", "f", false, "Force a clean redeployment of the entire environment")

	// 2. Feature-Specific Flags
	vaultJwtCmd.Flags().StringVar(&gitlabVersion, "gitlab-version", "18.10.1-ce.0", "Version of the GitLab CE container image to deploy")

	Cmd.AddCommand(vaultJwtCmd)
}
