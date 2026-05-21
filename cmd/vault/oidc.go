package vault

import (
	"fmt"
	"strings"

	"hal/internal/global"
	"hal/internal/integrations"

	vault "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
)

const (
	authentikVaultSlug   = "hashicorp-vault"
	oidcSharedServiceKey = "vault-oidc"
	oidcKVMount          = "kv-oidc"
)

var (
	oidcEnable         bool
	oidcDisable        bool
	oidcUpdate         bool
	oidcWithSCIM       bool
	oidcAuthentikImage string
	oidcAuthentikTag   string
)

var vaultOidcCmd = &cobra.Command{
	Use:   "oidc [status|enable|disable|update]",
	Short: "Deploy Authentik IdP and configure Vault OIDC auth",
	Long: `Spin up Authentik as a shared Identity Provider and wire up Vault OIDC auth.

The same Authentik stack is reused by 'hal tf saml' — it is only torn down
when no other product has registered against it.

Actions:
  status   Show Authentik stack health and Vault OIDC state (default)
  enable   Start Authentik, provision demo users/groups, configure Vault OIDC
  disable  Remove Vault OIDC and tear down Authentik if no other product uses it
  update   Re-provision Vault OIDC providers (keeps Authentik running)

Demo users created in Authentik:
  alice / password  → group: admin  → Vault policy: admin (all paths)
  bob   / password  → group: user-ro → Vault policy: user-ro (kv-oidc/team1 read)

Flags:
  --scim   [Vault Enterprise] Also wire SCIM provisioning from Authentik to Vault`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := parseLifecycleAction(args, &oidcEnable, &oidcDisable, &oidcUpdate); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		// Early exit: --scim requires Vault Enterprise. Check before doing any work.
		if oidcWithSCIM {
			if client, err := GetHealthyClient(); err == nil {
				if health, err := client.Sys().Health(); err == nil {
					v := health.Version
					if !strings.Contains(v, "ent") && !strings.Contains(v, "+") {
						fmt.Println("❌ Error: --scim requires Vault Enterprise.")
						fmt.Println("   💡 Your current Vault version is Community Edition.")
						fmt.Println()
						fmt.Println("   To deploy Vault Enterprise, run:")
						fmt.Println("   hal vault delete")
						fmt.Println("   export VAULT_LICENSE='your_license_string'")
						fmt.Println("   # or: export VAULT_LICENSE_PATH='/path/to/vault.hclic'")
						fmt.Println("   hal vault create --edition ent")
						return
					}
				}
			}
		}

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		client, vaultErr := GetHealthyClient()

		// ── STATUS ──────────────────────────────────────────────────────────────
		if !oidcEnable && !oidcDisable && !oidcUpdate {
			runOIDCStatus(engine, client, vaultErr)
			return
		}

		// ── DISABLE ─────────────────────────────────────────────────────────────
		if oidcDisable {
			runOIDCDisable(engine, client, vaultErr)
			return
		}

		// ── UPDATE ──────────────────────────────────────────────────────────────
		if oidcUpdate {
			fmt.Println("♻️  Update: cleaning Vault OIDC configuration for re-provision...")
			cleanVaultOIDC(client, vaultErr)

			// Remove the Authentik application + provider so they can be re-created
			// with fresh credentials on the re-provision pass below.
			secrets, err := integrations.LoadOrCreateAuthentikSecrets()
			if err == nil {
				aktClient := integrations.NewAuthentikClient(secrets.BootstrapToken)
				fmt.Println("  ⚙️  Removing Authentik application and provider for re-creation...")
				_ = aktClient.DeleteApplicationBySlug(authentikVaultSlug)
				_ = aktClient.DeleteOAuth2ProviderByName("vault-oidc-provider")
				_ = aktClient.DeleteSCIMProviderByName("vault-scim-provider")
			}
		}

		// ── ENABLE / UPDATE (continue) ───────────────────────────────────────────
		runOIDCEnable(engine, client, vaultErr)
	},
}

// ─── status ───────────────────────────────────────────────────────────────────

func runOIDCStatus(engine string, client *vault.Client, vaultErr error) {
	fmt.Println("🔍 Vault OIDC / Authentik IdP Status")
	fmt.Println()

	integrations.PrintAuthentikStatus(engine)
	fmt.Println()

	authMounted := false
	kvMounted := false
	if vaultErr == nil {
		auths, _ := client.Sys().ListAuth()
		_, authMounted = auths["oidc/"]
		mounts, _ := client.Sys().ListMounts()
		_, kvMounted = mounts[oidcKVMount+"/"]
	}

	icon := func(ok bool) string {
		if ok {
			return "✅"
		}
		return "❌"
	}

	fmt.Printf("  %s Vault OIDC auth   : oidc/\n", icon(authMounted))
	fmt.Printf("  %s Vault KV sandbox  : %s/\n", icon(kvMounted), oidcKVMount)
	fmt.Println()

	fmt.Println("💡 Next Step:")
	akt := integrations.IsAuthentikRunning(engine)
	switch {
	case !akt && !authMounted:
		fmt.Println("   hal vault oidc enable")
	case akt && authMounted:
		fmt.Println("   vault login -method=oidc")
		fmt.Printf("   Authentik UI: %s/if/admin/\n", integrations.AuthentikAdminURL())
	default:
		fmt.Println("   Environment partially degraded — run: hal vault oidc update")
	}
}

// ─── enable ───────────────────────────────────────────────────────────────────

func runOIDCEnable(engine string, client *vault.Client, vaultErr error) {
	if vaultErr != nil {
		fmt.Printf("❌ Vault must be running and healthy: %v\n", vaultErr)
		return
	}

	if global.DryRun {
		fmt.Println("[DRY RUN] Would start Authentik, provision users/groups, configure Vault OIDC")
		return
	}

	fmt.Println("🔐 hal vault oidc — deploying Authentik IdP + Vault OIDC")
	fmt.Println()

	// 1. Load or generate Authentik secrets
	secrets, err := integrations.LoadOrCreateAuthentikSecrets()
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}

	// 2. Start Authentik if not already up
	firstBoot := false
	if integrations.IsAuthentikRunning(engine) {
		fmt.Println("  ✅ Authentik stack already running — skipping start")
	} else {
		firstBoot = true
		fmt.Println("  Starting Authentik stack...")
		if err := integrations.StartAuthentikStack(engine, oidcAuthentikImage, oidcAuthentikTag, secrets); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}
		if err := integrations.WaitAuthentikHealthy(); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}
	}

	// Always verify the bootstrap token is accepted before any API calls.
	// The token is created by the worker async, so it may not be ready even
	// if the server health check passed — and on a re-provision (update) the
	// container is already running but we still need a valid token.
	if err := integrations.WaitAuthentikTokenReady(secrets.BootstrapToken); err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}

	if firstBoot {
		printAuthentikCredentials(secrets)
	}

	// 3. Register this product as a consumer
	if err := global.AddSharedServiceConsumer(integrations.AuthentikSharedServiceKey, oidcSharedServiceKey); err != nil {
		fmt.Printf("⚠️  Could not register shared service consumer: %v\n", err)
	}

	// 4. Provision Authentik: groups, users, OAuth2 provider, application
	fmt.Println("  ⚙️  Provisioning Authentik (users, groups, OAuth2 provider, application)...")
	aktClient := integrations.NewAuthentikClient(secrets.BootstrapToken)
	clientID, clientSecret, err := provisionAuthentikForVault(aktClient)
	if err != nil {
		fmt.Printf("❌ Authentik provisioning failed: %v\n", err)
		return
	}

	// 5. Verify Vault container can reach the discovery URL
	issuer := integrations.AuthentikOIDCIssuer(authentikVaultSlug)
	fmt.Println("  ⏳ Verifying Vault can reach Authentik OIDC discovery URL...")
	if err := integrations.WaitVaultVisibleAuthentik(engine, issuer); err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}

	// 6. Configure Vault OIDC
	fmt.Println("  ⚙️  Configuring Vault OIDC auth, policies, and identity groups...")
	if err := configureVaultOIDC(client, issuer, clientID, clientSecret); err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}

	// 7. SCIM provisioning (Vault Enterprise + Authentik outbound sync)
	if oidcWithSCIM {
		fmt.Println()
		fmt.Println("🔗 Configuring SCIM provisioning (Authentik → Vault)...")
		if err := configureSCIM(client, aktClient, authentikVaultSlug); err != nil {
			fmt.Printf("❌ SCIM setup failed: %v\n", err)
			fmt.Println("   OIDC is still active. Fix the error and re-run with --scim to retry.")
		}
	}

	global.RefreshHalHealth(engine)
	printOIDCSuccess(issuer, secrets)
}

// ─── disable ──────────────────────────────────────────────────────────────────

func runOIDCDisable(engine string, client *vault.Client, vaultErr error) {
	if global.DryRun {
		fmt.Println("[DRY RUN] Would remove Vault OIDC mounts, policies, and external groups")
		fmt.Println("[DRY RUN] Would deregister vault-oidc from Authentik shared service")
		return
	}

	fmt.Println("🛑 Tearing down Vault OIDC environment...")
	cleanVaultOIDC(client, vaultErr)

	remaining, err := global.RemoveSharedServiceConsumer(integrations.AuthentikSharedServiceKey, oidcSharedServiceKey)
	if err != nil {
		fmt.Printf("⚠️  Could not update shared service registry: %v\n", err)
	}

	if len(remaining) == 0 {
		fmt.Println("  No other products depend on Authentik — stopping the stack...")
		if err := integrations.StopAuthentikStack(engine, true); err != nil {
			fmt.Printf("⚠️  Warning during stack teardown: %v\n", err)
		}
		fmt.Println("  ✅ Authentik stack stopped and volumes removed")
	} else {
		// Authentik stays up for other consumers — remove the SCIM provider that pointed at Vault.
		fmt.Printf("  ℹ️  Authentik still in use by: %s — stack left running\n", strings.Join(remaining, ", "))
		if secrets, err := integrations.LoadOrCreateAuthentikSecrets(); err == nil {
			aktClient := integrations.NewAuthentikClient(secrets.BootstrapToken)
			_ = aktClient.DeleteSCIMProviderByName("vault-scim-provider")
		}
	}

	global.RefreshHalHealth(engine)
	fmt.Println("✅ Vault OIDC removed")
}

// ─── Authentik provisioning ───────────────────────────────────────────────────

// provisionAuthentikForVault creates the demo users, groups, OAuth2 provider, and
// application in Authentik. Returns (clientID, clientSecret, error).
func provisionAuthentikForVault(c *integrations.AuthentikClient) (string, string, error) {
	// Groups
	adminGroupPK, err := c.CreateGroup("admin")
	if err != nil {
		return "", "", fmt.Errorf("create group admin: %w", err)
	}
	userROGroupPK, err := c.CreateGroup("user-ro")
	if err != nil {
		return "", "", fmt.Errorf("create group user-ro: %w", err)
	}

	// Users
	if err := c.CreateUser("alice", "Alice Admin", "alice@hal.local", "password", []string{adminGroupPK}); err != nil {
		return "", "", fmt.Errorf("create user alice: %w", err)
	}
	if err := c.CreateUser("bob", "Bob Builder", "bob@hal.local", "password", []string{userROGroupPK}); err != nil {
		return "", "", fmt.Errorf("create user bob: %w", err)
	}

	// OAuth2 provider prerequisites
	flowPK, err := c.GetDefaultAuthorizationFlowPK()
	if err != nil {
		return "", "", fmt.Errorf("get authorization flow: %w", err)
	}
	invalidationFlowPK, err := c.GetDefaultInvalidationFlowPK()
	if err != nil {
		return "", "", fmt.Errorf("get invalidation flow: %w", err)
	}
	keyPK, err := c.GetFirstSigningKeyPK()
	if err != nil {
		return "", "", fmt.Errorf("get signing key: %w", err)
	}
	groupsScopePK, err := c.GetGroupsScopeMappingPK()
	if err != nil {
		return "", "", fmt.Errorf("get groups scope: %w", err)
	}

	// OAuth2 provider
	redirectURIs := []integrations.AuthentikRedirectURI{
		{MatchingMode: "strict", URL: "http://localhost:8250/oidc/callback"},
		{MatchingMode: "strict", URL: "http://vault.localhost:8200/ui/vault/auth/oidc/oidc/callback"},
		{MatchingMode: "strict", URL: "http://127.0.0.1:8250/oidc/callback"},
	}
	providerPK, clientID, clientSecret, err := c.CreateOAuth2Provider(
		"vault-oidc-provider", flowPK, invalidationFlowPK, keyPK, groupsScopePK, redirectURIs,
	)
	if err != nil {
		return "", "", fmt.Errorf("create OAuth2 provider: %w", err)
	}

	// Application — set meta_launch_url so the Authentik portal tile points to the
	// browser-accessible Vault UI OIDC page, not an auto-computed internal URL.
	vaultUIURL := "http://vault.localhost:8200/ui/vault/auth/oidc"
	if err := c.CreateApplication("HashiCorp Vault", authentikVaultSlug, providerPK, vaultUIURL); err != nil {
		return "", "", fmt.Errorf("create application: %w", err)
	}

	return clientID, clientSecret, nil
}

// ─── Vault OIDC configuration ─────────────────────────────────────────────────

func configureVaultOIDC(client *vault.Client, issuer, clientID, clientSecret string) error {
	// KV-V2 secrets engine
	_ = client.Sys().Mount(oidcKVMount, &vault.MountInput{
		Type:    "kv",
		Options: map[string]string{"version": "2"},
	})

	// Seed a demo secret for bob
	_, _ = client.Logical().Write(oidcKVMount+"/data/team1", map[string]interface{}{
		"data": map[string]interface{}{"example": "password"},
	})

	// Policies
	_ = client.Sys().PutPolicy("admin", `path "*" { capabilities = ["create", "read", "update", "delete", "list", "sudo"] }`)
	_ = client.Sys().PutPolicy("user-ro", `
path "kv-oidc/+/" { capabilities = ["list"] }
path "kv-oidc/data/team1" { capabilities = ["read", "list"] }
path "kv-oidc/metadata/team1" { capabilities = ["read", "list"] }
`)

	// Enable OIDC auth
	_ = client.Sys().EnableAuthWithOptions("oidc", &vault.EnableAuthOptions{Type: "oidc"})

	// Configure OIDC — retry because the token endpoint may still initialise
	var writeErr error
	for i := 0; i < 15; i++ {
		_, writeErr = client.Logical().Write("auth/oidc/config", map[string]interface{}{
			"oidc_discovery_url": issuer,
			"oidc_client_id":     clientID,
			"oidc_client_secret": clientSecret,
			"default_role":       "default",
		})
		if writeErr == nil {
			break
		}
		if !strings.Contains(strings.ToLower(writeErr.Error()), "error checking oidc discovery url") {
			return writeErr
		}
	}
	if writeErr != nil {
		return writeErr
	}

	// Default role — groups claim is "groups" (populated by Authentik's groups scope)
	// 127.0.0.1 is included alongside localhost because macOS may use either address
	// for the loopback listener opened by `vault login -method=oidc`.
	_, _ = client.Logical().Write("auth/oidc/role/default", map[string]interface{}{
		"user_claim":   "preferred_username",
		"groups_claim": "groups",
		"allowed_redirect_uris": []string{
			"http://localhost:8250/oidc/callback",
			"http://127.0.0.1:8250/oidc/callback",
			"http://vault.localhost:8200/ui/vault/auth/oidc/oidc/callback",
		},
		"oidc_scopes": []string{"openid", "profile", "email", "groups"},
	})

	// External groups with group aliases.
	// When SCIM is enabled, group creation and membership are fully managed by
	// Authentik outbound SCIM — skip creating them here to avoid 409 conflicts
	// when Authentik tries to push the same group names.
	if !oidcWithSCIM {
		auths, _ := client.Sys().ListAuth()
		accessor := auths["oidc/"].Accessor
		setupExternalGroup(client, "admin", accessor, []string{"admin"})
		setupExternalGroup(client, "user-ro", accessor, []string{"user-ro"})
	}

	return nil
}

func cleanVaultOIDC(client *vault.Client, vaultErr error) {
	if vaultErr != nil || client == nil {
		fmt.Println("  ⚠️  Vault offline — skipping Vault-internal cleanup")
		return
	}
	cleanVaultSCIM(client)
	_ = client.Sys().DisableAuth("oidc")
	_ = client.Sys().Unmount(oidcKVMount)
	_ = client.Sys().DeletePolicy("admin")
	_ = client.Sys().DeletePolicy("user-ro")
	// Delete identity groups by name. SCIM-managed groups are owned by Vault's
	// SCIM layer and can't be deleted via the identity API — those are cleaned up
	// by cleanVaultSCIM above (which deletes the SCIM client, removing its groups).
	_, _ = client.Logical().Delete("identity/group/name/admin")
	_, _ = client.Logical().Delete("identity/group/name/user-ro")
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func setupExternalGroup(client *vault.Client, groupName, accessor string, policies []string) {
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

func printAuthentikCredentials(secrets *integrations.AuthentikSecrets) {
	fmt.Println()
	fmt.Println("  🔑 Authentik — first boot credentials")
	fmt.Printf("     Admin UI  : %s/if/admin/\n", integrations.AuthentikAdminURL())
	fmt.Println("     Username  : akadmin")
	fmt.Printf("     Password  : %s\n", secrets.AdminPassword)
	fmt.Printf("     Saved at  : %s\n", integrations.AuthentikEnvPath())
	fmt.Println()
	fmt.Println("     To reset: docker exec hal-authentik-server ak changepassword akadmin")
	fmt.Println()
}

func printOIDCSuccess(issuer string, _ *integrations.AuthentikSecrets) {
	fmt.Println()
	fmt.Println("✅ Vault OIDC + Authentik IdP ready!")
	fmt.Println()
	fmt.Println("  UI login   : http://vault.localhost:8200  (select 'OIDC', leave role blank)")
	fmt.Println("  CLI login  : vault login -method=oidc")
	fmt.Println()
	fmt.Println("  Demo users:")
	fmt.Println("    alice / password  →  admin policy (full access)")
	fmt.Println("    bob   / password  →  user-ro policy (kv-oidc/team1 read)")
	fmt.Println()
	fmt.Printf("  Authentik  : %s/if/admin/\n", integrations.AuthentikAdminURL())
	fmt.Printf("  OIDC issuer: %s\n", issuer)
}

func init() {
	vaultOidcCmd.Flags().BoolVarP(&oidcEnable, "enable", "e", false, "Start Authentik and configure Vault OIDC")
	vaultOidcCmd.Flags().BoolVarP(&oidcDisable, "disable", "d", false, "Remove Vault OIDC and tear down Authentik if unused")
	vaultOidcCmd.Flags().BoolVarP(&oidcUpdate, "update", "u", false, "Re-provision Vault OIDC providers")
	_ = vaultOidcCmd.Flags().MarkHidden("enable")
	_ = vaultOidcCmd.Flags().MarkHidden("disable")
	_ = vaultOidcCmd.Flags().MarkHidden("update")

	vaultOidcCmd.Flags().BoolVar(&oidcWithSCIM, "scim", false, "[Vault Enterprise] Also configure SCIM provisioning from Authentik")
	vaultOidcCmd.Flags().StringVar(&oidcAuthentikImage, "authentik-image", integrations.AuthentikDefaultImage, "Authentik container image")
	vaultOidcCmd.Flags().StringVar(&oidcAuthentikTag, "authentik-tag", integrations.AuthentikDefaultTag, "Authentik image tag")

	Cmd.AddCommand(vaultOidcCmd)
}
