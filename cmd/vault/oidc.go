package vault

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hal/internal/global"

	vault "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
)

var (
	oidcEnable      bool
	oidcDisable     bool
	oidcForce       bool
	keycloakVersion string
)

var vaultOidcCmd = &cobra.Command{
	Use:   "oidc",
	Short: "Deploy Keycloak and fully configure Vault OIDC auth",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		client, vaultErr := GetHealthyClient()
		isPodman := strings.Contains(engine, "podman")

		// ==========================================
		// 1. SMART STATUS MODE (Default behavior)
		// ==========================================
		if !oidcEnable && !oidcDisable && !oidcForce {
			fmt.Println("🔍 Checking Vault OIDC / Keycloak Status...")

			// Check Docker
			kcExists := (exec.Command(engine, "inspect", "hal-keycloak").Run() == nil)

			// Check Vault API (if Vault is alive)
			authMounted := false
			kvMounted := false
			if vaultErr == nil {
				auths, _ := client.Sys().ListAuth()
				_, authMounted = auths["oidc/"]

				mounts, _ := client.Sys().ListMounts()
				_, kvMounted = mounts["kv-oidc/"]
			}

			// Output Status
			if kcExists {
				fmt.Printf("  ✅ Keycloak IdP  : Active (http://127.0.0.1:8081)\n")
			} else {
				fmt.Printf("  ❌ Keycloak IdP  : Not running\n")
			}

			if authMounted {
				fmt.Printf("  ✅ Vault OIDC    : Configured (oidc/)\n")
			} else {
				fmt.Printf("  ❌ Vault OIDC    : Not configured\n")
			}

			if kvMounted {
				fmt.Printf("  ✅ Sandbox KV    : Configured (kv-oidc/)\n")
			} else {
				fmt.Printf("  ⚠️  Sandbox KV    : Not configured (Optional)\n")
			}

			// Smart Assistant Logic
			fmt.Println("\n💡 Next Step:")
			if !kcExists && !authMounted {
				fmt.Println("   To deploy Keycloak and wire up Vault OIDC, run:")
				fmt.Println("   hal vault oidc --enable")
			} else if kcExists && authMounted {
				fmt.Println("   Demo is ready! Test the integration:")
				fmt.Println("   vault login -method=oidc")
				fmt.Println("\n   To completely remove this demo environment, run:")
				fmt.Println("   hal vault oidc --disable")
			} else {
				fmt.Println("   Environment is partially degraded. To safely reset, run:")
				fmt.Println("   hal vault oidc --force")
			}
			return
		}

		// ==========================================
		// 2. TEARDOWN / RESET PATH (--disable / --force)
		// ==========================================
		if oidcDisable || oidcForce {
			if global.DryRun {
				fmt.Println("[DRY RUN] Would execute: docker rm -f hal-keycloak")
				fmt.Println("[DRY RUN] Would call API to clean up Vault OIDC mounts, identity groups, and policies")
			} else {
				if oidcDisable {
					fmt.Println("🛑 Tearing down OIDC environment...")
				} else {
					fmt.Println("♻️  Force flag detected. Destroying OIDC environment for reset...")
				}

				if vaultErr == nil && client != nil {
					fmt.Println("⚙️  Connecting to Vault API for cleanup...")
					_ = client.Sys().DisableAuth("oidc")
					_ = client.Sys().Unmount("kv-oidc")
					_ = client.Sys().DeletePolicy("admin")
					_ = client.Sys().DeletePolicy("user-ro")

					fmt.Println("⚙️  Removing Sandbox Identity Groups and Entities...")
					_, _ = client.Logical().Delete("identity/group/name/admin")
					_, _ = client.Logical().Delete("identity/group/name/user-ro")
					_, _ = client.Logical().Delete("identity/entity/name/alice")
					_, _ = client.Logical().Delete("identity/entity/name/bob")
				} else {
					fmt.Println("⚠️  Vault is offline. Skipped Vault-internal cleanup.")
				}

				fmt.Println("⚙️  Removing Keycloak container...")
				_ = exec.Command(engine, "rm", "-f", "hal-keycloak").Run()

				if oidcDisable {
					fmt.Println("✅ OIDC integration, KV engine, and identity data successfully removed!")
				}
			}

			if oidcDisable && !global.DryRun {
				return
			}
		}

		// ==========================================
		// 3. DEPLOY / ENABLE PATH (--enable / --force)
		// ==========================================
		if oidcEnable || oidcForce {
			if vaultErr != nil {
				fmt.Printf("❌ Cannot deploy: Vault must be running and healthy. %v\n", vaultErr)
				return
			}

			if global.DryRun {
				fmt.Println("[DRY RUN] Would execute Docker run command for Keycloak.")
				fmt.Println("[DRY RUN] Would mount 'kv-oidc' and 'oidc' auth method.")
				fmt.Println("[DRY RUN] Would create Vault policies and external Identity Groups.")
				return
			}

			fmt.Println("⚙️  Preparing Keycloak IdP configuration...")
			realmJSON := `{
				"realm": "hal",
				"enabled": true,
				"users": [
					{ 
						"username": "alice", 
						"enabled": true, 
						"email": "alice@hal.local",
						"firstName": "Alice",
						"lastName": "Admin",
						"emailVerified": true,
						"credentials": [{"type": "password", "value": "password"}], 
						"groups": ["admin"] 
					},
					{ 
						"username": "bob", 
						"enabled": true, 
						"email": "bob@hal.local",
						"firstName": "Bob",
						"lastName": "Builder",
						"emailVerified": true,
						"credentials": [{"type": "password", "value": "password"}], 
						"groups": ["user-ro"] 
					}
				],
				"groups": [
					{ "name": "admin" },
					{ "name": "user-ro" }
				],
				"clients": [
					{
						"clientId": "vault",
						"enabled": true,
						"clientAuthenticatorType": "client-secret",
						"secret": "supersecret",
						"redirectUris": ["http://localhost:8250/oidc/callback", "http://vault.localhost:8200/ui/vault/auth/oidc/oidc/callback", "http://127.0.0.1:8250/oidc/callback"],
						"standardFlowEnabled": true,
						"protocolMappers": [
							{
								"name": "groups",
								"protocol": "openid-connect",
								"protocolMapper": "oidc-group-membership-mapper",
								"config": {
									"claim.name": "groups",
									"full.path": "false",
									"id.token.claim": "true",
									"access.token.claim": "true",
									"userinfo.token.claim": "true"
								}
							}
						]
					}
				]
			}`

			homeDir, _ := os.UserHomeDir()
			configDir := filepath.Join(homeDir, ".hal", "keycloak")
			os.MkdirAll(configDir, 0755)
			realmPath := filepath.Join(configDir, "hal-realm.json")
			_ = os.WriteFile(realmPath, []byte(realmJSON), 0644)

			fmt.Printf("🚀 Booting Keycloak container (quay.io/keycloak/keycloak:%s)...\n", keycloakVersion)
			_ = exec.Command(engine, "rm", "-f", "hal-keycloak").Run()

			kcArgs := []string{
				"run", "-d",
				"--name", "hal-keycloak",
				"--network", "hal-net",
				"--network-alias", "keycloak.localhost",
				"-p", "8081:8081",
				"-e", "KEYCLOAK_ADMIN=admin",
				"-e", "KEYCLOAK_ADMIN_PASSWORD=admin",
				"-e", "KC_HTTP_PORT=8081",
			}

			volFlag := fmt.Sprintf("%s:/opt/keycloak/data/import/hal-realm.json", realmPath)
			if isPodman {
				volFlag += ":Z"
			}
			kcArgs = append(kcArgs, "-v", volFlag)
			kcArgs = append(kcArgs, fmt.Sprintf("quay.io/keycloak/keycloak:%s", keycloakVersion), "start-dev", "--import-realm")

			if err := exec.Command(engine, kcArgs...).Run(); err != nil {
				fmt.Printf("❌ Failed to boot Keycloak: %v\n", err)
				return
			}

			fmt.Println("⏳ Waiting for Keycloak OIDC endpoints to become active...")
			if err := waitForKeycloak("http://127.0.0.1:8081/realms/hal/.well-known/openid-configuration", 45); err != nil {
				fmt.Println("❌ Keycloak failed to start in time.")
				return
			}

			fmt.Println("⚙️  Configuring Vault OIDC Auth, KV Engine, Policies, and External Groups...")

			// 1. Enable KV-V2 Secrets Engine
			_ = client.Sys().Mount("kv-oidc", &vault.MountInput{
				Type: "kv",
				Options: map[string]string{
					"version": "2",
				},
			})

			// 2. Seed a test secret for Bob to read
			_, err = client.Logical().Write("kv-oidc/data/team1", map[string]interface{}{
				"data": map[string]interface{}{
					"example": "password",
				},
			})
			if err != nil {
				fmt.Printf("⚠️  Warning: Failed to seed test secret: %v\n", err)
			}

			// 3. Create Policies
			_ = client.Sys().PutPolicy("admin", `path "*" { capabilities = ["create", "read", "update", "delete", "list", "sudo"] }`)
			_ = client.Sys().PutPolicy("user-ro", `
path "kv-oidc/+/" { capabilities = ["list"] }
path "kv-oidc/data/team1" { capabilities = ["read", "list"] }
path "kv-oidc/metadata/team1" { capabilities = ["read", "list"] }
`)

			// 4. Enable OIDC Auth
			_ = client.Sys().EnableAuthWithOptions("oidc", &vault.EnableAuthOptions{Type: "oidc"})

			// 5. Configure OIDC
			_, err = client.Logical().Write("auth/oidc/config", map[string]interface{}{
				"oidc_discovery_url": "http://keycloak.localhost:8081/realms/hal",
				"oidc_client_id":     "vault",
				"oidc_client_secret": "supersecret",
				"default_role":       "default",
			})
			if err != nil {
				fmt.Printf("❌ Failed to configure Vault OIDC: %v\n", err)
				return
			}

			// 6. Configure Default Role
			_, _ = client.Logical().Write("auth/oidc/role/default", map[string]interface{}{
				"user_claim":   "preferred_username",
				"groups_claim": "groups",
				"allowed_redirect_uris": []string{
					"http://localhost:8250/oidc/callback",
					"http://vault.localhost:8200/ui/vault/auth/oidc/oidc/callback",
				},
				"oidc_scopes": []string{"openid", "profile", "email"},
			})

			// 7. Map Groups
			auths, _ := client.Sys().ListAuth()
			oidcAccessor := auths["oidc/"].Accessor

			setupExternalGroup(client, "admin", oidcAccessor, []string{"admin"})
			setupExternalGroup(client, "user-ro", oidcAccessor, []string{"user-ro"})

			fmt.Println("\n✅ Vault OIDC & KV Integration Complete!")
			fmt.Println("---------------------------------------------------------")
			fmt.Println("🔗 UI Login:    http://vault.localhost:8200")
			fmt.Println("                (Select 'OIDC' and leave the role blank)")
			fmt.Println("\n🔗 CLI Login:   vault login -method=oidc")
			fmt.Println("\n👤 Test Users:  alice / password (Admin)")
			fmt.Println("                bob   / password (Read-Only on kv-oidc/team1)")
			fmt.Println("---------------------------------------------------------")
		}
	},
}

// -----------------------------------------------------------------------------
// Helper Functions
// -----------------------------------------------------------------------------

func setupExternalGroup(client *vault.Client, groupName string, accessor string, policies []string) {
	grpResp, err := client.Logical().Write("identity/group", map[string]interface{}{
		"name":     groupName,
		"type":     "external",
		"policies": policies,
	})
	if err != nil || grpResp == nil {
		return
	}

	groupID := grpResp.Data["id"].(string)

	_, _ = client.Logical().Write("identity/group-alias", map[string]interface{}{
		"name":           groupName,
		"mount_accessor": accessor,
		"canonical_id":   groupID,
	})
}

func waitForKeycloak(url string, maxRetries int) error {
	client := http.Client{Timeout: 2 * time.Second}
	for i := 0; i < maxRetries; i++ {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout")
}

func init() {
	// Standard Lifecycle Flags
	vaultOidcCmd.Flags().BoolVarP(&oidcEnable, "enable", "e", false, "Deploy Keycloak and fully configure Vault OIDC auth")
	vaultOidcCmd.Flags().BoolVarP(&oidcDisable, "disable", "d", false, "Remove Keycloak and strip the OIDC configuration from Vault")
	vaultOidcCmd.Flags().BoolVarP(&oidcForce, "force", "f", false, "Force a clean redeployment of the OIDC environment")

	// Feature-Specific Flags
	vaultOidcCmd.Flags().StringVar(&keycloakVersion, "keycloak-version", "24.0.4", "Version of the Keycloak container image to deploy")

	Cmd.AddCommand(vaultOidcCmd)
}
