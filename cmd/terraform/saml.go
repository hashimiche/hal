package terraform

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"strings"

	"hal/internal/global"
	"hal/internal/integrations"

	"github.com/spf13/cobra"
)

// ─── shared-service keys and slug helpers ─────────────────────────────────────

const (
	tfeSAMLSharedServiceKeyPrimary = "tfe-saml"
	tfeSAMLSharedServiceKeyTwin    = "tfe-bis-saml"
)

func tfeSAMLSharedServiceKey(target string) string {
	if target == tfeTargetTwin {
		return tfeSAMLSharedServiceKeyTwin
	}
	return tfeSAMLSharedServiceKeyPrimary
}

// tfeSAMLAppSlug returns the Authentik application slug for a TFE SAML integration.
func tfeSAMLAppSlug(target string) string {
	if target == tfeTargetTwin {
		return "tfe-bis-saml"
	}
	return "tfe-saml"
}

// tfeSAMLProviderName returns the Authentik SAML provider name for a target.
func tfeSAMLProviderName(target string) string {
	if target == tfeTargetTwin {
		return "tfe-bis-saml-provider"
	}
	return "tfe-saml-provider"
}

// tfeSAMLSCIMProviderName returns the Authentik SCIM provider name for a target.
func tfeSAMLSCIMProviderName(target string) string {
	if target == tfeTargetTwin {
		return "tfe-bis-scim-provider"
	}
	return "tfe-scim-provider"
}

// tfeSAMLBaseURLForTarget returns the host-accessible TFE base URL for API calls.
// These are the defaults; users can override via --tfe-url.
func tfeSAMLBaseURLForTarget(target string) string {
	if target == tfeTargetTwin {
		return "https://tfe-bis.localhost:9443"
	}
	return "https://tfe.localhost:8443"
}

// tfeSAMLSPBaseURL returns the portless base URL that TFE uses in its own SAML
// SP metadata. TFE derives the ACS URL and entity ID from TFE_HOSTNAME alone —
// it does not include TFE_HTTPS_PORT in these identifiers. When the user
// provides --tfe-url, we strip the port from it to derive the SP base URL.
// This mirrors how Vault OIDC uses Vault's own accessible URL for redirect URIs.
func tfeSAMLSPBaseURL(apiBaseURL, target string) string {
	if apiBaseURL != "" {
		u, err := url.Parse(apiBaseURL)
		if err == nil {
			return u.Scheme + "://" + u.Hostname() // strip port — TFE_HOSTNAME has no port
		}
	}
	if target == tfeTargetTwin {
		return "https://tfe-bis.localhost"
	}
	return "https://tfe.localhost"
}

// tfeSAMLProxyContainerForTarget returns the proxy container name for SCIM
// container-to-container access.
func tfeSAMLProxyContainerForTarget(target string) string {
	if target == tfeTargetTwin {
		return "hal-tfe-bis-proxy"
	}
	return "hal-tfe-proxy"
}

// tfeSAMLProxyPortForTarget returns the HTTPS port that the proxy container
// listens on internally (used for SCIM container-to-container access).
func tfeSAMLProxyPortForTarget(target string) int {
	if target == tfeTargetTwin {
		return 9443
	}
	return 8443
}

// ─── flags ────────────────────────────────────────────────────────────────────

var (
	tfeSAMLEnable         bool
	tfeSAMLDisable        bool
	tfeSAMLUpdate         bool
	tfeSAMLWithSCIM       bool
	tfeSAMLSync           bool
	tfeSAMLAuthentikImage string
	tfeSAMLAuthentikTag   string
	tfeSAMLBaseURL        string
	tfeSAMLOrgName        string
	tfeSAMLAPIToken       string
	tfeSAMLAdminUsername  string
	tfeSAMLAdminEmail     string
	tfeSAMLAdminPassword  string
)

// ─── command ──────────────────────────────────────────────────────────────────

var tfeSAMLCmd = &cobra.Command{
	Use:   "saml [status|enable|disable|update]",
	Short: "Deploy Authentik IdP and configure TFE SAML SSO",
	Long: `Spin up Authentik as a shared Identity Provider and wire up TFE SAML SSO.

The same Authentik stack is reused by 'hal vault oidc' — it is only torn down
when no other product has registered against it.

Actions:
  status   Show Authentik stack health and TFE SAML state (default)
  enable   Start Authentik, provision demo users/groups, configure TFE SAML
  disable  Remove TFE SAML and tear down Authentik if no other product uses it
  update   Re-provision TFE SAML providers (keeps Authentik running)

Demo users created in Authentik:
  alice / password  → group: admins  → TFE team: admins
  bob   / password  → group: devs    → TFE team: devs

Flags:
  --scim   Also wire SCIM provisioning from Authentik to TFE
  --sync   (with --scim) Re-push all Authentik group membership to TFE without full re-provision`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := parseLifecycleAction(args, &tfeSAMLEnable, &tfeSAMLDisable, &tfeSAMLUpdate); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		target, err := normalizeTFETarget(tfeLifecycleTarget)
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}
		if target == tfeTargetBoth {
			fmt.Println("❌ '--target both' is not supported for 'hal tf saml'.")
			fmt.Println("   💡 Use '--target primary' or '--target twin'.")
			return
		}

		// Apply target-specific URL defaults when not explicitly set.
		if tfeSAMLBaseURL == "" {
			tfeSAMLBaseURL = tfeSAMLBaseURLForTarget(target)
		}

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		// ── STATUS ──────────────────────────────────────────────────────────────
		if !tfeSAMLEnable && !tfeSAMLDisable && !tfeSAMLUpdate {
			runTFESAMLStatus(engine, target)
			return
		}

		// ── DISABLE ─────────────────────────────────────────────────────────────
		if tfeSAMLDisable {
			runTFESAMLDisable(engine, target)
			return
		}

		// ── UPDATE ──────────────────────────────────────────────────────────────
		if tfeSAMLUpdate {
			// --scim --sync: re-push all Authentik group membership to TFE without full re-provision.
			if tfeSAMLWithSCIM && tfeSAMLSync {
				fmt.Println("🔄 Syncing SCIM group membership to TFE...")
				secrets, err := integrations.LoadOrCreateAuthentikSecrets()
				if err != nil {
					fmt.Printf("❌ Could not load Authentik secrets: %v\n", err)
					return
				}
				aktClient := integrations.NewAuthentikClient(secrets.BootstrapToken)
				scimProviderName := tfeSAMLSCIMProviderName(target)
				pk, _, err := aktClient.GetSCIMProviderByName(scimProviderName)
				if err != nil || pk == 0 {
					fmt.Printf("❌ SCIM provider %q not found in Authentik.\n", scimProviderName)
					fmt.Println("   Run: hal tf saml enable --scim")
					return
				}
				scimToken, err := aktClient.GetSCIMProviderToken(pk)
				if err != nil || scimToken == "" {
					fmt.Printf("❌ Could not get SCIM token from provider %q: %v\n", scimProviderName, err)
					return
				}
				apiToken, err := bootstrapTFETokenForSAML(engine, target)
				if err != nil {
					fmt.Printf("❌ Could not get TFE API token: %v\n", err)
					return
				}
				orgName := tfeSAMLOrgName
				if orgName == "" {
					orgName = defaultSAMLOrgName
				}
				if err := authentikInitialSCIMSync(tfeSAMLBaseURL, scimToken, apiToken, orgName, pk, aktClient); err != nil {
					fmt.Printf("❌ Sync failed: %v\n", err)
					return
				}
				fmt.Println("✅ Users and groups synced")
				return
			}

			fmt.Println("♻️  Update: cleaning TFE SAML configuration for re-provision...")
			// Get a TFE API token for cleanup.
			apiToken, err := bootstrapTFETokenForSAML(engine, target)
			if err != nil {
				fmt.Printf("⚠️  Could not get TFE token for cleanup: %v — continuing\n", err)
			} else {
				cleanTFESAML(tfeSAMLBaseURL, apiToken)
				clearOldTFESAMLCert(engine)
			}

			// Remove Authentik app + provider for re-creation.
			secrets, err := integrations.LoadOrCreateAuthentikSecrets()
			if err == nil {
				aktClient := integrations.NewAuthentikClient(secrets.BootstrapToken)
				fmt.Println("  ⚙️  Removing Authentik application and provider for re-creation...")
				_ = aktClient.DeleteApplicationBySlug(tfeSAMLAppSlug(target))
				_ = aktClient.DeleteSAMLProviderByName(tfeSAMLProviderName(target))
				_ = aktClient.DeleteSCIMProviderByName(tfeSAMLSCIMProviderName(target))
			}
		}

		// ── ENABLE / UPDATE (continue) ───────────────────────────────────────────
		runTFESAMLEnable(engine, target)
	},
}

// ─── status ───────────────────────────────────────────────────────────────────

func runTFESAMLStatus(engine, target string) {
	fmt.Printf("🔍 TFE SAML / Authentik IdP Status  (target=%s)\n", target)
	fmt.Println()

	integrations.PrintAuthentikStatus(engine)
	fmt.Println()

	baseURL := tfeSAMLBaseURL
	if baseURL == "" {
		baseURL = tfeSAMLBaseURLForTarget(target)
	}

	samlEnabled := false
	tfeReachable := false
	apiToken, tokenErr := bootstrapTFETokenForSAML(engine, target)
	if tokenErr == nil {
		tfeReachable = true
		settings, err := getTFESAMLSettings(baseURL, apiToken)
		if err == nil {
			if attrs, ok := settings["attributes"].(map[string]interface{}); ok {
				enabled, _ := attrs["enabled"].(bool)
				samlEnabled = enabled
			}
		}
	}

	icon := func(ok bool) string {
		if ok {
			return "✅"
		}
		return "❌"
	}

	fmt.Printf("  %s TFE reachable   : %s\n", icon(tfeReachable), baseURL)
	fmt.Printf("  %s TFE SAML SSO    : enabled=%v\n", icon(samlEnabled), samlEnabled)
	fmt.Println()

	fmt.Println("💡 Next Step:")
	akt := integrations.IsAuthentikRunning(engine)
	switch {
	case !akt && !samlEnabled:
		fmt.Println("   hal tf saml enable")
	case akt && samlEnabled:
		fmt.Printf("   Open %s → sign in via SSO\n", baseURL)
		fmt.Printf("   Authentik UI: %s/if/admin/\n", integrations.AuthentikAdminURL())
	default:
		fmt.Println("   Environment partially degraded — run: hal tf saml update")
	}
}

// ─── enable ───────────────────────────────────────────────────────────────────

func runTFESAMLEnable(engine, target string) {
	// Verify TFE is reachable.
	if !global.IsContainerRunning(engine, tfeCoreContainerForTargetNoErr(target)) {
		fmt.Printf("❌ TFE core container is not running for target %q.\n", target)
		fmt.Println("   💡 Run 'hal tf create' first.")
		return
	}

	if global.DryRun {
		fmt.Println("[DRY RUN] Would start Authentik, provision users/groups, configure TFE SAML")
		return
	}

	fmt.Printf("🔐 hal tf saml — deploying Authentik IdP + TFE SAML  (target=%s)\n", target)
	fmt.Println()

	// 1. Load or generate Authentik secrets.
	secrets, err := integrations.LoadOrCreateAuthentikSecrets()
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}

	// 2. Start Authentik if not already up.
	firstBoot := false
	if integrations.IsAuthentikRunning(engine) {
		fmt.Println("  ✅ Authentik stack already running — skipping start")
	} else {
		firstBoot = true
		fmt.Println("  Starting Authentik stack...")
		if err := integrations.StartAuthentikStack(engine, tfeSAMLAuthentikImage, tfeSAMLAuthentikTag, secrets); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}
		if err := integrations.WaitAuthentikHealthy(); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}
	}

	// Always verify bootstrap token is accepted before any API calls.
	if err := integrations.WaitAuthentikTokenReady(secrets.BootstrapToken); err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}

	if firstBoot {
		if err := integrations.WaitAuthentikScopesReady(secrets.BootstrapToken); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}
		if err := integrations.WaitAuthentikFlowsReady(secrets.BootstrapToken); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}
		printTFESAMLAuthentikCredentials(secrets)
	}

	// 3. Register as a shared-service consumer.
	serviceKey := tfeSAMLSharedServiceKey(target)
	if err := global.AddSharedServiceConsumer(integrations.AuthentikSharedServiceKey, serviceKey); err != nil {
		fmt.Printf("⚠️  Could not register shared service consumer: %v\n", err)
	}

	// Start the SAML proxy. It rewrites Authentik's SAML response form action
	// from portless HTTPS (port 443) to the accessible TFE proxy port so the
	// browser can POST the SAML assertion without needing port 443 on the host.
	fmt.Println("  ⚙️  Starting Authentik SAML proxy (port-rewrite for ACS URL)...")
	if err := integrations.StartAuthentikSAMLProxy(engine); err != nil {
		fmt.Printf("❌ Could not start Authentik SAML proxy: %v\n", err)
		return
	}

	// 4. Provision Authentik: groups, users, SAML provider, application.
	fmt.Println("  ⚙️  Provisioning Authentik (users, groups, SAML provider, application)...")
	aktClient := integrations.NewAuthentikClient(secrets.BootstrapToken)
	samlProviderPK, err := provisionAuthentikForTFE(aktClient, target)
	if err != nil {
		fmt.Printf("❌ Authentik provisioning failed: %v\n", err)
		return
	}

	// 5. Fetch IdP metadata and parse SSO URL + certificate.
	fmt.Println("  ⏳ Fetching Authentik SAML metadata...")
	metadataXML, err := aktClient.GetSAMLProviderMetadata(samlProviderPK)
	if err != nil {
		fmt.Printf("❌ Could not fetch SAML metadata: %v\n", err)
		return
	}
	ssoURL, idpCert, err := integrations.ParseSAMLMetadata(metadataXML)
	if err != nil {
		fmt.Printf("❌ Could not parse SAML metadata: %v\n", err)
		return
	}

	// Route the TFE SAML SSO flow through the SAML proxy. The proxy rewrites
	// Authentik's SAML response form action URL from portless port 443 to the
	// accessible TFE proxy port before the browser receives the HTML page.
	if u, err := url.Parse(ssoURL); err == nil && u.Port() == integrations.AuthentikHTTPPort {
		u.Host = u.Hostname() + ":" + integrations.AuthentikSAMLProxyPort
		ssoURL = u.String()
	}

	// 6. Bootstrap TFE API token.
	fmt.Println("  ⏳ Bootstrapping TFE admin token...")
	apiToken, err := bootstrapTFETokenForSAML(engine, target)
	if err != nil {
		fmt.Printf("❌ Could not obtain TFE admin token: %v\n", err)
		return
	}

	// 7. Configure TFE SAML.
	baseURL := tfeSAMLBaseURL
	fmt.Printf("  ⚙️  Configuring TFE SAML settings at %s...\n", baseURL)
	if err := configureTFESAML(baseURL, apiToken, ssoURL, idpCert); err != nil {
		fmt.Printf("❌ TFE SAML configuration failed: %v\n", err)
		return
	}

	// 7b. Ensure demo teams exist in the target org so SAML group → team mapping
	// works on first SSO login. TFE adds users to existing teams whose names match
	// the MemberOf attribute — it does NOT auto-create teams.
	// Skip when --scim is active: Authentik SCIM owns team creation via group sync.
	orgName := tfeSAMLOrgName
	if orgName == "" {
		orgName = defaultSAMLOrgName
	}
	if !tfeSAMLWithSCIM {
		provisionTFESAMLTeams(baseURL, apiToken, orgName)
	}

	// 8. Optional SCIM.
	if tfeSAMLWithSCIM {
		fmt.Println()
		fmt.Println("🔗 Configuring SCIM provisioning (Authentik → TFE)...")
		// Version gate: TFE SCIM requires 2.0+.
		if err := requireTFESCIMVersion(baseURL, apiToken); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}
		// Step 0: remove teams that were manually created by a prior non-SCIM enable
		// so Authentik SCIM takes ownership and the user sees them created via sync.
		cleanPreSCIMTFETeams(baseURL, apiToken, orgName)
		if err := configureTFESCIM(baseURL, apiToken, aktClient, tfeSAMLAppSlug(target), tfeSAMLSCIMProviderName(target), orgName, target); err != nil {
			fmt.Printf("❌ SCIM setup failed: %v\n", err)
			fmt.Println("   SAML is still active. To retry SCIM only, run:")
			fmt.Printf("     hal tf saml update --scim\n")
		}
	}

	global.RefreshHalHealth(engine)
	printTFESAMLSuccess(secrets, target)
}

// ─── disable ──────────────────────────────────────────────────────────────────

func runTFESAMLDisable(engine, target string) {
	if global.DryRun {
		fmt.Println("[DRY RUN] Would disable TFE SAML and deregister from Authentik shared service")
		return
	}

	fmt.Printf("🛑 Tearing down TFE SAML environment  (target=%s)...\n", target)

	baseURL := tfeSAMLBaseURL
	apiToken, err := bootstrapTFETokenForSAML(engine, target)
	if err != nil {
		fmt.Printf("⚠️  Could not get TFE token — skipping TFE SAML cleanup: %v\n", err)
	} else {
		orgName := tfeSAMLOrgName
		if orgName == "" {
			orgName = defaultSAMLOrgName
		}
		cleanTFESAML(baseURL, apiToken)
		disableTFESCIMOnTFE(baseURL, apiToken, orgName)
	}

	serviceKey := tfeSAMLSharedServiceKey(target)
	remaining, err := global.RemoveSharedServiceConsumer(integrations.AuthentikSharedServiceKey, serviceKey)
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
		fmt.Printf("  ℹ️  Authentik still in use by: %s — stack left running\n", strings.Join(remaining, ", "))
		// The SAML proxy is TFE SAML-specific; stop it even when Authentik stays up.
		integrations.StopAuthentikSAMLProxy(engine)
		// Clean up Authentik-side SAML/SCIM artifacts for this target.
		if secrets, err := integrations.LoadOrCreateAuthentikSecrets(); err == nil {
			aktClient := integrations.NewAuthentikClient(secrets.BootstrapToken)
			_ = aktClient.DeleteApplicationBySlug(tfeSAMLAppSlug(target))
			_ = aktClient.DeleteSAMLProviderByName(tfeSAMLProviderName(target))
			_ = aktClient.DeleteSCIMProviderByName(tfeSAMLSCIMProviderName(target))
		}
	}

	global.RefreshHalHealth(engine)
	fmt.Printf("✅ TFE SAML removed  (target=%s)\n", target)
}

// ─── Authentik provisioning ───────────────────────────────────────────────────

const defaultSAMLOrgName = "hal-org"

// provisionAuthentikForTFE creates demo users/groups and a SAML provider + application
// in Authentik for the given TFE target. Returns the Authentik SAML provider PK.
func provisionAuthentikForTFE(c *integrations.AuthentikClient, target string) (int, error) {
	// Groups
	adminsGroupPK, err := c.CreateGroup("admins")
	if err != nil {
		return 0, fmt.Errorf("create group admins: %w", err)
	}
	devsGroupPK, err := c.CreateGroup("devs")
	if err != nil {
		return 0, fmt.Errorf("create group devs: %w", err)
	}

	// Users
	if err := c.CreateUser("alice", "Alice Admin", "alice@hal.local", "password", []string{adminsGroupPK}); err != nil {
		return 0, fmt.Errorf("create user alice: %w", err)
	}
	if err := c.CreateUser("bob", "Bob Builder", "bob@hal.local", "password", []string{devsGroupPK}); err != nil {
		return 0, fmt.Errorf("create user bob: %w", err)
	}

	// SAML provider prerequisites.
	authFlowPK, err := c.GetDefaultAuthorizationFlowPK()
	if err != nil {
		return 0, fmt.Errorf("get authorization flow: %w", err)
	}
	invalidationFlowPK, err := c.GetDefaultInvalidationFlowPK()
	if err != nil {
		return 0, fmt.Errorf("get invalidation flow: %w", err)
	}
	signingKeyPK, err := c.GetFirstSigningKeyPK()
	if err != nil {
		return 0, fmt.Errorf("get signing key: %w", err)
	}

	// Custom SAML property mappings (attribute names match TFE defaults).
	usernamePK, err := c.GetOrCreateSAMLUsernameMapping()
	if err != nil {
		return 0, fmt.Errorf("get SAML username mapping: %w", err)
	}
	groupsPK, err := c.GetOrCreateSAMLGroupsMapping()
	if err != nil {
		return 0, fmt.Errorf("get SAML groups mapping: %w", err)
	}

	baseURL := tfeSAMLBaseURLForTarget(target)
	if tfeSAMLBaseURL != "" {
		baseURL = tfeSAMLBaseURL
	}
	spBase := tfeSAMLSPBaseURL(tfeSAMLBaseURL, target) // portless — matches TFE_HOSTNAME
	acsURL := spBase + "/users/saml/auth"
	audience := spBase + "/users/saml/metadata" // SP entity ID / audience URI

	providerPK, err := c.CreateSAMLProvider(
		tfeSAMLProviderName(target),
		authFlowPK,
		invalidationFlowPK,
		signingKeyPK,
		acsURL,
		audience,
		[]string{usernamePK, groupsPK},
	)
	if err != nil {
		return 0, fmt.Errorf("create SAML provider: %w", err)
	}

	// Application — launch URL points to the TFE UI.
	tfeUIURL := baseURL
	if err := c.CreateApplication("TFE "+strings.Title(target), tfeSAMLAppSlug(target), providerPK, tfeUIURL); err != nil {
		return 0, fmt.Errorf("create application: %w", err)
	}

	return providerPK, nil
}

// configureTFESAML enables SAML on TFE with the Authentik IdP metadata.
func configureTFESAML(baseURL, apiToken, ssoURL, idpCert string) error {
	// Derive the SLO URL from the SSO URL (same base path, slo vs sso segment).
	sloURL := strings.Replace(ssoURL, "/sso/binding/redirect/", "/slo/binding/post/", 1)
	if sloURL == ssoURL {
		// Fallback: strip trailing slash and append /slo/.
		sloURL = strings.TrimSuffix(ssoURL, "/") + "-slo/"
	}

	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "saml-settings",
			"attributes": map[string]interface{}{
				"enabled":          true,
				"debug":            false,
				"idp_cert":         idpCert,
				"sso_endpoint_url": ssoURL,
				"slo_endpoint_url": sloURL,
				"attr_username":    "Username",
				"attr_groups":      "MemberOf",
				"attr_site_admin":  "SiteAdminRole",
				"site_admin_role":  "site-admins",
				// 14-day API token session for SSO users
				"sso_api_token_session_timeout": 1209600,
				// Required for TFE SCIM: provider type must not be "unknown".
				// "saml" is the correct value for a generic SAML 2.0 IdP (Authentik).
				"provider_type": "saml",
			},
		},
	}

	url := fmt.Sprintf("%s/api/v2/admin/saml-settings", baseURL)
	body, status, err := integrations.TFERequest("PATCH", url, apiToken, payload)
	if err != nil {
		detail := strings.TrimSpace(string(body))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("PATCH %s → %d: %s", url, status, detail)
	}
	return nil
}

// getTFESAMLSettings fetches the current TFE SAML settings data object.
func getTFESAMLSettings(baseURL, apiToken string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/api/v2/admin/saml-settings", baseURL)
	body, _, err := integrations.TFERequest("GET", url, apiToken, nil)
	if err != nil {
		return nil, err
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	data, _ := resp["data"].(map[string]interface{})
	return data, nil
}

// provisionTFESAMLTeams ensures the demo Authentik groups (admins, devs) have
// matching teams in the given TFE organization. TFE only adds SSO users to teams
// that already exist — it does not auto-create teams from SAML group names.
func provisionTFESAMLTeams(baseURL, apiToken, orgName string) {
	// Fetch existing team names for the org.
	tbody, _, err := integrations.TFERequest("GET", baseURL+"/api/v2/organizations/"+orgName+"/teams", apiToken, nil)
	if err != nil {
		return
	}
	var teamsResp map[string]interface{}
	if err := json.Unmarshal(tbody, &teamsResp); err != nil {
		return
	}
	data, _ := teamsResp["data"].([]interface{})
	existing := map[string]bool{}
	for _, t := range data {
		team, _ := t.(map[string]interface{})
		attrs, _ := team["attributes"].(map[string]interface{})
		if name, _ := attrs["name"].(string); name != "" {
			existing[name] = true
		}
	}

	// Teams to ensure: admins (manage) and devs (read).
	teamDefs := []struct {
		name   string
		access map[string]interface{}
	}{
		{"admins", map[string]interface{}{"manage-workspaces": true, "manage-projects": true, "manage-modules": true, "manage-providers": true}},
		{"devs", map[string]interface{}{"read-workspaces": true, "read-projects": true}},
	}
	for _, td := range teamDefs {
		if existing[td.name] {
			continue
		}
		payload := map[string]interface{}{
			"data": map[string]interface{}{
				"type": "teams",
				"attributes": map[string]interface{}{
					"name":                td.name,
					"organization-access": td.access,
				},
			},
		}
		_, _, _ = integrations.TFERequest("POST", baseURL+"/api/v2/organizations/"+orgName+"/teams", apiToken, payload)
	}
}

// clearOldTFESAMLCert NULLs out old_idp_cert_encrypted in the TFE database.
// This prevents PEM_read_bio_X509 failures when a previous provision stored a
// malformed (e.g. empty) cert that TFE later moved to old_idp_cert.
// Best-effort: errors are silently ignored.
func clearOldTFESAMLCert(engine string) {
	_ = exec.Command(engine, "exec", "hal-tfe-db", "sh", "-c",
		"psql -U tfe tfe -c 'UPDATE rails.admin_settings_saml SET old_idp_cert_encrypted = NULL WHERE old_idp_cert_encrypted IS NOT NULL;'").Run()
}

// cleanTFESAML disables TFE SAML. No-op if TFE is offline or SAML is already disabled.
func cleanTFESAML(baseURL, apiToken string) {
	if apiToken == "" {
		return
	}
	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "saml-settings",
			"attributes": map[string]interface{}{
				"enabled": false,
			},
		},
	}
	url := fmt.Sprintf("%s/api/v2/admin/saml-settings", baseURL)
	_, _, _ = integrations.TFERequest("PATCH", url, apiToken, payload)
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// tfeCoreContainerForTargetNoErr returns the TFE core container name for the
// target without error (fallback to primary container name on unknown target).
func tfeCoreContainerForTargetNoErr(target string) string {
	name, err := tfeCoreContainerForTarget(target)
	if err != nil {
		return "hal-tfe"
	}
	return name
}

// bootstrapTFETokenForSAML obtains a TFE admin token for the given target.
func bootstrapTFETokenForSAML(engine, target string) (string, error) {
	baseURL := tfeSAMLBaseURL
	if baseURL == "" {
		baseURL = tfeSAMLBaseURLForTarget(target)
	}
	adminUser := tfeSAMLAdminUsername
	if adminUser == "" {
		adminUser = "haladmin"
	}
	adminEmail := tfeSAMLAdminEmail
	if adminEmail == "" {
		adminEmail = "haladmin@localhost"
	}
	adminPass := tfeSAMLAdminPassword
	if adminPass == "" {
		adminPass = "hal9000FTW"
	}
	orgName := tfeSAMLOrgName
	if orgName == "" {
		orgName = defaultSAMLOrgName
	}

	cfg := tfeFoundationConfig{
		BaseURL:       baseURL,
		OrgName:       orgName,
		APIToken:      tfeSAMLAPIToken,
		AdminUsername: adminUser,
		AdminEmail:    adminEmail,
		AdminPassword: adminPass,
	}
	token, _, err := ensureTFEFoundation(engine, cfg)
	return token, err
}

func printTFESAMLAuthentikCredentials(secrets *integrations.AuthentikSecrets) {
	fmt.Println()
	fmt.Println("  🔑 Authentik — first boot credentials")
	fmt.Printf("     Admin UI  : %s/if/admin/\n", integrations.AuthentikAdminURL())
	fmt.Println("     Username  : akadmin")
	fmt.Printf("     Password  : %s\n", secrets.AdminPassword)
	fmt.Printf("     Saved at  : %s\n", integrations.AuthentikEnvPath())
	fmt.Println()
}

func printTFESAMLSuccess(secrets *integrations.AuthentikSecrets, target string) {
	baseURL := tfeSAMLBaseURL
	if baseURL == "" {
		baseURL = tfeSAMLBaseURLForTarget(target)
	}
	fmt.Println()
	fmt.Println("✅ TFE SAML + Authentik IdP ready!")
	fmt.Println()
	fmt.Printf("  TFE UI     : %s  (click 'SSO' to log in)\n", baseURL)
	fmt.Println()
	fmt.Println("  Demo users:")
	fmt.Println("    alice / password  →  group: admins  (TFE team: admins)")
	fmt.Println("    bob   / password  →  group: devs    (TFE team: devs)")
	fmt.Println()
	fmt.Printf("  Authentik admin  : %s/if/admin/\n", integrations.AuthentikAdminURL())
	fmt.Printf("  Authentik login  : akadmin / %s\n", secrets.AdminPassword)
	fmt.Println()
	if tfeSAMLWithSCIM {
		fmt.Println("  SCIM sync        : Authentik → TFE  (teams + users provisioned via outbound SCIM)")
		fmt.Printf("  Sync dashboard   : %s/if/admin/#/core/providers\n", integrations.AuthentikAdminURL())
		fmt.Println("  Re-sync manually : hal tf saml update --scim --sync")
		fmt.Println()
		fmt.Println("  💡 alice/bob and their teams were provisioned by SCIM — not manually created.")
		fmt.Println("     Changes in Authentik (add/remove group members) are pushed to TFE automatically.")
	} else {
		fmt.Println("  💡 Teams 'admins' and 'devs' created in all orgs — alice/bob land in their")
		fmt.Println("     team on first SSO login. Org access is granted automatically.")
		fmt.Println()
		fmt.Println("  Tip: run with --scim to let Authentik provision teams+users via SCIM instead.")
	}
}

func init() {
	tfeSAMLCmd.Flags().BoolVarP(&tfeSAMLEnable, "enable", "e", false, "Start Authentik and configure TFE SAML SSO")
	tfeSAMLCmd.Flags().BoolVarP(&tfeSAMLDisable, "disable", "d", false, "Remove TFE SAML and tear down Authentik if unused")
	tfeSAMLCmd.Flags().BoolVarP(&tfeSAMLUpdate, "update", "u", false, "Re-provision TFE SAML providers")
	_ = tfeSAMLCmd.Flags().MarkHidden("enable")
	_ = tfeSAMLCmd.Flags().MarkHidden("disable")
	_ = tfeSAMLCmd.Flags().MarkHidden("update")

	tfeSAMLCmd.Flags().BoolVar(&tfeSAMLWithSCIM, "scim", false, "Also configure SCIM provisioning from Authentik to TFE")
	tfeSAMLCmd.Flags().BoolVar(&tfeSAMLSync, "sync", false, "With --scim: re-push all group membership to TFE without full re-provision")
	tfeSAMLCmd.Flags().StringVar(&tfeSAMLAuthentikImage, "authentik-image", integrations.AuthentikDefaultImage, "Authentik container image")
	tfeSAMLCmd.Flags().StringVar(&tfeSAMLAuthentikTag, "authentik-tag", integrations.AuthentikDefaultTag, "Authentik image tag")
	tfeSAMLCmd.Flags().StringVar(&tfeSAMLBaseURL, "tfe-url", "", "TFE base URL (default: https://tfe.localhost:8443 for primary)")
	tfeSAMLCmd.Flags().StringVar(&tfeSAMLOrgName, "tfe-org", defaultSAMLOrgName, "TFE organization name")
	tfeSAMLCmd.Flags().StringVar(&tfeSAMLAPIToken, "tfe-token", "", "TFE admin API token (auto-bootstrapped if omitted)")
	tfeSAMLCmd.Flags().StringVar(&tfeSAMLAdminUsername, "tfe-admin-username", "haladmin", "TFE admin username")
	tfeSAMLCmd.Flags().StringVar(&tfeSAMLAdminEmail, "tfe-admin-email", "haladmin@localhost", "TFE admin email")
	tfeSAMLCmd.Flags().StringVar(&tfeSAMLAdminPassword, "tfe-admin-password", "hal9000FTW", "TFE admin password")

	bindTFETargetFlag(tfeSAMLCmd)
	Cmd.AddCommand(tfeSAMLCmd)
}
