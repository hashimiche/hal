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
	keycloakVersion string
	oidcDestroy     bool
)

var vaultOidcCmd = &cobra.Command{
	Use:   "oidc",
	Short: "Deploy Keycloak and fully configure Vault OIDC auth (or destroy it with --destroy)",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		// 1. Try to get the client
		client, vaultErr := GetHealthyClient()

		// 2. If we are DEPLOYING, we demand Vault is healthy.
		if !oidcDestroy && vaultErr != nil {
			fmt.Printf("❌ %v\n", vaultErr)
			return
		}

		isPodman := strings.Contains(engine, "podman")

		// ==========================================
		// THE DESTROY LOGIC (--destroy)
		// ==========================================
		if oidcDestroy {
			fmt.Println("⚙️  Removing Keycloak container...")
			_ = exec.Command(engine, "rm", "-f", "hal-keycloak").Run()

			fmt.Println("⚙️  Connecting to Vault API for cleanup...")
			// Only attempt Vault cleanup if Vault is actually alive
			if vaultErr == nil && client != nil {
				fmt.Println("⚙️  Disabling 'oidc/' auth path...")
				_ = client.Sys().DisableAuth("oidc")

				fmt.Println("⚙️  Removing 'kv-oidc/' secrets engine...")
				_ = client.Sys().Unmount("kv-oidc")

				fmt.Println("⚙️  Removing OIDC policies...")
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

			fmt.Println("✅ OIDC integration, KV engine, and identity data successfully removed!")
			return
		}

		// ==========================================
		// THE DEPLOY LOGIC (Default)
		// ==========================================
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

		fmt.Printf("⚙️  Booting Keycloak container (quay.io/keycloak/keycloak:%s)...\n", keycloakVersion)
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

		fmt.Println("⚙️  Waiting for Keycloak OIDC endpoints to become active...")
		if err := waitForKeycloak("http://127.0.0.1:8081/realms/hal/.well-known/openid-configuration", 45); err != nil {
			fmt.Println("❌ Keycloak failed to start in time.")
			return
		}

		fmt.Println("⚙️  Configuring Vault OIDC Auth, KV Engine, Policies, and External Groups...")
		config := vault.DefaultConfig()
		if os.Getenv("VAULT_ADDR") == "" {
			config.Address = "http://127.0.0.1:8200"
		}

		// 1. Enable KV-V2 Secrets Engine
		_ = client.Sys().Mount("kv-oidc", &vault.MountInput{
			Type: "kv",
			Options: map[string]string{
				"version": "2",
			},
		})

		// 2. Seed a test secret for Bob to read
		fmt.Println("⚙️  Seeding test secret at 'kv-oidc/team1'...")
		_, err = client.Logical().Write("kv-oidc/data/team1", map[string]interface{}{
			"data": map[string]interface{}{
				"example": "password",
			},
		})
		if err != nil {
			fmt.Printf("⚠️  Warning: Failed to seed test secret: %v\n", err)
		}

		// 3. Create Policies (With corrected UI listing permissions)
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
	},
}

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
	vaultOidcCmd.Flags().StringVar(&keycloakVersion, "keycloak-version", "26.5.6", "Version of the Keycloak container image to deploy")
	vaultOidcCmd.Flags().BoolVar(&oidcDestroy, "destroy", false, "Remove Keycloak and strip the OIDC configuration from Vault")
	Cmd.AddCommand(vaultOidcCmd)
}
