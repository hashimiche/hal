package terraform

// TFE SCIM provisioning — Authentik outbound SCIM → TFE.
//
// SCIM in TFE requires SAML SSO to be enabled first.
//
// TFE SCIM maps:
//   SCIM User  → TFE User (created on first push)
//   SCIM Group → TFE Team (created automatically if it doesn't exist)
//
// TFE 2.0+ (introduced SCIM): tokens are site-admin scoped:
//   POST   /api/v2/admin/scim-tokens
//   SCIM must be explicitly enabled: PATCH /api/v2/admin/scim-settings {enabled:true}
//   SCIM base path: /scim/v2
//
// TFE 1.x fallback (no SCIM support in practice; kept for compatibility):
//   POST   /api/v2/organizations/:org/scim-tokens
//   SCIM base path: /api/scim/v2
//
// Authentik must reach TFE from inside docker/podman (container-to-container)
// using the proxy container hostname and internal port, with TLS verification
// disabled (TFE uses a self-signed certificate in the lab environment).

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"hal/internal/integrations"
)

// configureTFESCIM creates a TFE SCIM token, configures an Authentik outbound
// SCIM provider targeting TFE, assigns it as backchannel on the TFE application,
// and runs an initial objects sync so users/groups are populated immediately.
func configureTFESCIM(
	baseURL, apiToken string,
	aktClient *integrations.AuthentikClient,
	appSlug, scimProviderName, orgName, target string,
) error {
	// 0. Enable TFE SCIM (TFE 2.0+ requires explicit activation after SAML is on).
	fmt.Println("  ⚙️  Enabling TFE SCIM provisioning...")
	if err := enableTFESCIMSettings(baseURL, apiToken); err != nil {
		return fmt.Errorf("enable TFE SCIM settings: %w", err)
	}

	// 1. Create a SCIM token in TFE.
	fmt.Println("  ⚙️  Creating TFE SCIM token...")
	scimToken, err := createTFESCIMToken(baseURL, apiToken, orgName)
	if err != nil {
		return fmt.Errorf("create TFE SCIM token: %w", err)
	}
	fmt.Println("  ✅ TFE SCIM token created")

	// 2. Build the SCIM endpoint URL that Authentik will use (container-to-container).
	proxyContainer := tfeSAMLProxyContainerForTarget(target)
	proxyPort := tfeSAMLProxyPortForTarget(target)
	// TFE 2.0+ SCIM base path: /scim/v2  (TFE 1.x used /api/scim/v2 but had no real SCIM support)
	scimEndpoint := fmt.Sprintf("https://%s:%d/scim/v2", proxyContainer, proxyPort)

	// 3. Get SCIM property mappings from Authentik.
	fmt.Println("  ⚙️  Fetching Authentik SCIM property mappings...")
	userMappings, groupMappings, err := aktClient.GetDefaultSCIMPropertyMappings()
	if err != nil {
		return fmt.Errorf("get scim property mappings: %w", err)
	}

	// 4. Upsert Authentik SCIM provider → TFE.
	// disable_ssl_verification is set via compatibility_mode field or
	// the provider-level verify_ssl flag (Authentik 2024+).
	fmt.Println("  ⚙️  Configuring Authentik SCIM provider → TFE...")
	providerPK, err := upsertTFESCIMProvider(
		aktClient, scimProviderName, scimEndpoint, scimToken, userMappings, groupMappings,
	)
	if err != nil {
		return fmt.Errorf("upsert Authentik SCIM provider: %w", err)
	}

	// 5. Assign SCIM provider as backchannel on the TFE application.
	fmt.Println("  ⚙️  Assigning SCIM provider as backchannel on TFE application...")
	if err := aktClient.SetApplicationBackchannelProviders(appSlug, []int{providerPK}); err != nil {
		return fmt.Errorf("assign backchannel provider: %w", err)
	}

	// 6. Check provider sync status.
	fmt.Println("  🔍 Checking SCIM provider sync status...")
	if status, err := aktClient.GetSCIMSyncStatus(providerPK); err == nil && status != nil {
		if healthy, ok := status["healthy"].(bool); ok {
			if healthy {
				fmt.Println("  ✅ SCIM provider healthy")
			} else {
				fmt.Println("  ⚠️  SCIM provider reports unhealthy — check Authentik task log")
			}
		}
	}

	// 7. Seed TFE via Authentik's per-object SCIM sync API, then link SCIM groups
	// to org teams. Using Authentik's sync engine keeps the implementation aligned
	// with the documented Authentik → TFE integration model.
	if err := authentikInitialSCIMSync(baseURL, scimToken, apiToken, orgName, providerPK, aktClient); err != nil {
		return fmt.Errorf("SCIM seed: %w", err)
	}

	return nil
}

// requireTFESCIMVersion gates the SCIM flow on TFE 2.0+. Returns a descriptive
// error when the running TFE version is below 2.0 so the user gets an
// actionable message before any state is changed.
// If the version header is absent the check is skipped (benefit of the doubt).
func requireTFESCIMVersion(baseURL, apiToken string) error {
	version, err := integrations.GetTFEVersion(baseURL, apiToken)
	if err != nil || version == "" {
		// Can't determine version — let SCIM attempt and surface any real error.
		return nil
	}
	// Strip a leading "v" (e.g. "v2.0.2" → "2.0.2") then parse the major part.
	clean := strings.TrimPrefix(version, "v")
	majorStr := strings.SplitN(clean, ".", 2)[0]
	major, parseErr := strconv.Atoi(majorStr)
	if parseErr != nil {
		return nil // unrecognised format — skip gate
	}
	if major < 2 {
		return fmt.Errorf("--scim requires TFE 2.0 or later (detected: %s)\n   Upgrade TFE or omit --scim", version)
	}
	return nil
}

// enableTFESCIMSettings enables SCIM on TFE via the admin settings API (TFE 2.0+).
// Checks current state first: if SCIM is already enabled the PATCH is skipped so
// TFE's 422 "cannot re-enable" response is never triggered.
func enableTFESCIMSettings(baseURL, apiToken string) error {
	settingsURL := fmt.Sprintf("%s/api/v2/admin/scim-settings", baseURL)

	// Read current state before attempting to change it.
	body, _, getErr := integrations.TFERequest("GET", settingsURL, apiToken, nil)
	if getErr != nil {
		if strings.Contains(getErr.Error(), "404") {
			// TFE 1.x — no SCIM settings endpoint, skip silently.
			return nil
		}
		// Any other GET failure: fall through and try PATCH anyway.
	} else {
		var resp map[string]interface{}
		if json.Unmarshal(body, &resp) == nil {
			data, _ := resp["data"].(map[string]interface{})
			attrs, _ := data["attributes"].(map[string]interface{})
			if enabled, _ := attrs["enabled"].(bool); enabled {
				fmt.Println("  ✅ TFE SCIM already enabled")
				return nil
			}
		}
	}

	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type":       "scim-settings",
			"attributes": map[string]interface{}{"enabled": true},
		},
	}
	_, status, err := integrations.TFERequest("PATCH", settingsURL, apiToken, payload)
	if err != nil && (status == 404 || status == 422) {
		// 404: TFE 1.x — no endpoint.
		// 422: SCIM already active or state conflict — treat as non-fatal and proceed.
		if status == 422 {
			fmt.Println("  ⚠️  TFE SCIM settings already active (422) — proceeding")
		}
		return nil
	}
	return err
}

// disableTFESCIMOnTFE removes all TFE SCIM tokens and disables SCIM settings.
// Called by the disable flow to leave TFE in a clean state for the next enable.
func disableTFESCIMOnTFE(baseURL, apiToken, orgName string) {
	_ = deleteAllTFESCIMTokens(baseURL, apiToken, orgName)
	settingsURL := fmt.Sprintf("%s/api/v2/admin/scim-settings", baseURL)
	_, _, _ = integrations.TFERequest("DELETE", settingsURL, apiToken, nil)
	fmt.Println("  ✅ TFE SCIM tokens and settings cleared")
}

// createTFESCIMToken creates a new SCIM token in TFE.
// Tries the TFE 2.0+ site-admin endpoint first; falls back to the legacy
// org-scoped endpoint for TFE 1.x compatibility.
func createTFESCIMToken(baseURL, apiToken, orgName string) (string, error) {
	// TFE 2.0+: site-admin scoped token.
	adminURL := fmt.Sprintf("%s/api/v2/admin/scim-tokens", baseURL)
	adminPayload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "authentication-tokens",
			"attributes": map[string]interface{}{
				"description": "hal-scim-integration",
			},
		},
	}
	if body, _, err := integrations.TFERequest("POST", adminURL, apiToken, adminPayload); err == nil {
		if token, parseErr := extractSCIMTokenValue(body); parseErr == nil && token != "" {
			return token, nil
		}
	}
	// TFE 2.0+: a token may already exist — clear and recreate.
	if listBody, _, listErr := integrations.TFERequest("GET", adminURL, apiToken, nil); listErr == nil {
		if _, parseErr := extractFirstSCIMToken(listBody); parseErr == nil {
			_ = deleteAllTFESCIMTokens(baseURL, apiToken, orgName)
			if body2, _, err2 := integrations.TFERequest("POST", adminURL, apiToken, adminPayload); err2 == nil {
				if token, parseErr := extractSCIMTokenValue(body2); parseErr == nil && token != "" {
					return token, nil
				}
			}
		}
	}

	// TFE 1.x fallback: org-scoped token endpoint.
	orgURL := fmt.Sprintf("%s/api/v2/organizations/%s/scim-tokens", baseURL, orgName)
	body, _, err := integrations.TFERequest("POST", orgURL, apiToken, map[string]interface{}{})
	if err != nil {
		// If a token already exists, TFE may return 422. Delete and recreate.
		if listBody, _, listErr := integrations.TFERequest("GET", orgURL, apiToken, nil); listErr == nil {
			if _, parseErr := extractFirstSCIMToken(listBody); parseErr == nil {
				_ = deleteAllTFESCIMTokensOrg(baseURL, apiToken, orgName)
				body2, _, err2 := integrations.TFERequest("POST", orgURL, apiToken, map[string]interface{}{})
				if err2 != nil {
					return "", fmt.Errorf("recreate scim token (org): %w", err2)
				}
				return extractSCIMTokenValue(body2)
			}
		}
		return "", fmt.Errorf("POST %s: %w", orgURL, err)
	}
	return extractSCIMTokenValue(body)
}

func extractSCIMTokenValue(body []byte) (string, error) {
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("decode scim token response: %w", err)
	}
	data, _ := resp["data"].(map[string]interface{})
	if data == nil {
		return "", fmt.Errorf("unexpected scim token response shape: %s", string(body))
	}
	attrs, _ := data["attributes"].(map[string]interface{})
	if attrs == nil {
		return "", fmt.Errorf("no attributes in scim token response")
	}
	token, _ := attrs["token"].(string)
	if token == "" {
		return "", fmt.Errorf("scim token value is empty in response")
	}
	return token, nil
}

func extractFirstSCIMToken(body []byte) (string, error) {
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}
	data, _ := resp["data"].([]interface{})
	if len(data) == 0 {
		return "", fmt.Errorf("no scim tokens found")
	}
	item, _ := data[0].(map[string]interface{})
	id, _ := item["id"].(string)
	return id, nil
}

func deleteAllTFESCIMTokens(baseURL, apiToken, orgName string) error {
	// TFE 2.0+: site-admin endpoint.
	adminURL := fmt.Sprintf("%s/api/v2/admin/scim-tokens", baseURL)
	if body, _, err := integrations.TFERequest("GET", adminURL, apiToken, nil); err == nil {
		var resp map[string]interface{}
		if json.Unmarshal(body, &resp) == nil {
			data, _ := resp["data"].([]interface{})
			for _, item := range data {
				entry, _ := item.(map[string]interface{})
				id, _ := entry["id"].(string)
				if id != "" {
					_, _, _ = integrations.TFERequest("DELETE", adminURL+"/"+id, apiToken, nil)
				}
			}
			return nil
		}
	}
	// TFE 1.x fallback.
	return deleteAllTFESCIMTokensOrg(baseURL, apiToken, orgName)
}

// deleteAllTFESCIMTokensOrg deletes all SCIM tokens via the legacy org-scoped endpoint.
func deleteAllTFESCIMTokensOrg(baseURL, apiToken, orgName string) error {
	url := fmt.Sprintf("%s/api/v2/organizations/%s/scim-tokens", baseURL, orgName)
	body, _, err := integrations.TFERequest("GET", url, apiToken, nil)
	if err != nil {
		return err
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return err
	}
	data, _ := resp["data"].([]interface{})
	for _, item := range data {
		entry, _ := item.(map[string]interface{})
		id, _ := entry["id"].(string)
		if id != "" {
			deleteURL := fmt.Sprintf("%s/api/v2/organizations/%s/scim-tokens/%s", baseURL, orgName, id)
			_, _, _ = integrations.TFERequest("DELETE", deleteURL, apiToken, nil)
		}
	}
	return nil
}

// upsertTFESCIMProvider creates or updates the Authentik outbound SCIM provider
// for TFE. Uses CreateSCIMProvider with verify_ssl disabled via a custom request.
// TFE uses a self-signed certificate, so SSL verification must be disabled.
func upsertTFESCIMProvider(
	aktClient *integrations.AuthentikClient,
	name, scimEndpoint, bearerToken string,
	userMappingPKs, groupMappingPKs []string,
) (int, error) {
	// Check if provider already exists.
	existingPK, _, err := aktClient.GetSCIMProviderByName(name)
	if err != nil {
		return 0, fmt.Errorf("lookup scim provider %q: %w", name, err)
	}
	if existingPK != 0 {
		if err := aktClient.UpdateSCIMProviderWithVerifySSL(existingPK, name, scimEndpoint, bearerToken, userMappingPKs, groupMappingPKs, false); err != nil {
			return 0, fmt.Errorf("update scim provider %q: %w", name, err)
		}
		return existingPK, nil
	}
	return aktClient.CreateSCIMProviderWithVerifySSL(name, scimEndpoint, bearerToken, userMappingPKs, groupMappingPKs, false)
}

// authentikInitialSCIMSync triggers Authentik's per-object SCIM sync for every
// user and group, then links the resulting TFE SCIM groups to org teams.
//
// TFE 2.0 with a site-admin SCIM token: SCIM groups are instance-level (not
// org-scoped). The scim-group-mapping admin API is the documented way to link each
// SCIM group to an org team. Without it, users never appear in the org.
//
// After team mappings are created, groups are re-synced so TFE immediately
// reconciles team membership from the current SCIM group state. Without this
// second sync, reconciliation waits until the next event-driven update
// (e.g. the first SAML login).
func authentikInitialSCIMSync(baseURL, scimToken, apiToken, orgName string, providerPK int, aktClient *integrations.AuthentikClient) error {
	fmt.Println("  ⚙️  Syncing users and groups to TFE via Authentik SCIM...")

	// 1. Sync all users via Authentik's per-object SCIM sync API.
	users, err := aktClient.GetAllUsers()
	if err != nil {
		return fmt.Errorf("get Authentik users: %w", err)
	}
	usersOK := 0
	for _, u := range users {
		if err := aktClient.SyncSCIMObject(providerPK, "authentik.core.models.User", u.PK); err != nil {
			fmt.Printf("  ⚠️  Sync user %q: %v\n", u.Username, err)
		} else {
			usersOK++
		}
	}
	if usersOK == 0 && len(users) > 0 {
		return fmt.Errorf("all %d user(s) failed to sync via Authentik SCIM", len(users))
	}

	// 2. Sync all groups (first pass) so TFE SCIM has group records with members.
	groups, err := aktClient.GetGroupsWithMembers()
	if err != nil {
		return fmt.Errorf("get Authentik groups: %w", err)
	}
	for _, g := range groups {
		if err := aktClient.SyncSCIMObject(providerPK, "authentik.core.models.Group", g.PK); err != nil {
			fmt.Printf("  ⚠️  Sync group %q: %v\n", g.Name, err)
		}
	}

	// 3. Link SCIM groups to org teams.
	// TFE 2.0 site-admin SCIM token: groups are instance-level, not org-scoped.
	// scim-group-mapping is the documented TFE API to associate a SCIM group with
	// an org team. Without it, users never appear in the org after SAML login.
	fmt.Printf("  ⚙️  Linking SCIM groups to TFE org teams in %q...\n", orgName)
	scimBase := strings.TrimSuffix(baseURL, "/") + "/scim/v2"
	scimClient := newTFESCIMClient()
	linkedGroups := 0
	for _, g := range groups {
		if !validTFETeamName(g.Name) {
			fmt.Printf("  ⚠️  Group %q: skipped — not a valid TFE team name\n", g.Name)
			continue
		}
		scimGroupID, err := findTFESCIMGroupByName(scimClient, scimBase, scimToken, g.Name)
		if err != nil {
			fmt.Printf("  ⚠️  Find SCIM group %q in TFE: %v\n", g.Name, err)
			continue
		}
		if err := linkSCIMGroupToOrgTeam(baseURL, apiToken, orgName, g.Name, scimGroupID); err != nil {
			fmt.Printf("  ⚠️  Team link %q: %v\n", g.Name, err)
		} else {
			linkedGroups++
		}
	}

	// 4. Re-sync groups now that team mappings exist.
	// When Authentik pushes a SCIM group update and TFE has a scim-group-mapping,
	// TFE reconciles team membership from the SCIM group state. This second sync
	// ensures membership is applied immediately rather than waiting for the first
	// SAML login to trigger the event-driven backchannel update.
	if linkedGroups > 0 {
		fmt.Println("  ⚙️  Re-syncing groups to trigger team membership reconciliation...")
		for _, g := range groups {
			if validTFETeamName(g.Name) {
				_ = aktClient.SyncSCIMObject(providerPK, "authentik.core.models.Group", g.PK)
			}
		}
	}

	fmt.Printf("  ✅ Synced %d user(s) and %d group(s), linked %d team(s) in org %q\n",
		usersOK, len(groups), linkedGroups, orgName)
	return nil
}

// newTFESCIMClient returns an HTTP client configured for the lab TFE instance.
// InsecureSkipVerify is required because TFE uses a self-signed certificate.
func newTFESCIMClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec — self-signed lab cert
		},
		Timeout: 15 * time.Second,
	}
}

// findTFESCIMGroupByName queries TFE's SCIM Groups endpoint for a group by displayName
// and returns its TFE SCIM UUID. Called after Authentik has synced the group to TFE.
func findTFESCIMGroupByName(client *http.Client, scimBase, token, name string) (string, error) {
	filter := url.QueryEscape(fmt.Sprintf(`displayName eq "%s"`, name))
	req, err := http.NewRequest("GET", scimBase+"/Groups?filter="+filter, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/scim+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	if json.Unmarshal(raw, &result) != nil {
		return "", fmt.Errorf("decode SCIM Groups response: %s", string(raw))
	}
	resources, _ := result["Resources"].([]interface{})
	if len(resources) == 0 {
		return "", fmt.Errorf("group %q not found in TFE SCIM", name)
	}
	item, _ := resources[0].(map[string]interface{})
	id, _ := item["id"].(string)
	if id == "" {
		return "", fmt.Errorf("group %q: no id in SCIM response", name)
	}
	return id, nil
}

// linkSCIMGroupToOrgTeam creates or finds a TFE org team for the given group name,
// then links the SCIM group to that team via the admin scim-group-mapping API.
// Once linked, TFE uses SCIM group membership to drive org team membership on
// each SCIM group update pushed by Authentik.
func linkSCIMGroupToOrgTeam(baseURL, apiToken, orgName, groupName, scimGroupID string) error {
	if !validTFETeamName(groupName) {
		return fmt.Errorf("skipped: %q is not a valid TFE team name (only letters, numbers, -, _)", groupName)
	}
	teamID, err := getOrCreateTFETeam(baseURL, apiToken, orgName, groupName)
	if err != nil {
		return fmt.Errorf("get/create team: %w", err)
	}
	mapURL := fmt.Sprintf("%s/api/v2/admin/teams/%s/scim-group-mapping", baseURL, teamID)
	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "scim-group-mapping",
			"attributes": map[string]interface{}{
				"scim-group-id": scimGroupID,
			},
		},
	}
	_, status, err := integrations.TFERequest("POST", mapURL, apiToken, payload)
	if err != nil && status != 409 { // 409 = already linked, treat as success
		return fmt.Errorf("link SCIM group to team: %w", err)
	}
	return nil
}

// validTFETeamName reports whether name is a valid TFE team name (letters, numbers, -, _).
func validTFETeamName(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// getOrCreateTFETeam returns the TFE team ID for teamName in orgName, creating
// the team if it does not already exist.
func getOrCreateTFETeam(baseURL, apiToken, orgName, teamName string) (string, error) {
	listURL := fmt.Sprintf("%s/api/v2/organizations/%s/teams?filter%%5Bnames%%5D=%s",
		baseURL, orgName, url.QueryEscape(teamName))
	if body, _, err := integrations.TFERequest("GET", listURL, apiToken, nil); err == nil {
		if id := extractJSONAPIFirstID(body); id != "" {
			return id, nil
		}
	}
	createURL := fmt.Sprintf("%s/api/v2/organizations/%s/teams", baseURL, orgName)
	createPayload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "teams",
			"attributes": map[string]interface{}{
				"name":       teamName,
				"visibility": "organization",
			},
		},
	}
	createBody, _, err := integrations.TFERequest("POST", createURL, apiToken, createPayload)
	if err != nil {
		return "", fmt.Errorf("create team %q: %w", teamName, err)
	}
	id := extractJSONAPIFirstID(createBody)
	if id == "" {
		return "", fmt.Errorf("no id in create team response")
	}
	return id, nil
}

// extractJSONAPIFirstID extracts the id from a JSON:API response (list or single object).
func extractJSONAPIFirstID(body []byte) string {
	var resp map[string]interface{}
	if json.Unmarshal(body, &resp) != nil {
		return ""
	}
	if dataArr, ok := resp["data"].([]interface{}); ok && len(dataArr) > 0 {
		if item, ok := dataArr[0].(map[string]interface{}); ok {
			id, _ := item["id"].(string)
			return id
		}
	}
	if dataObj, ok := resp["data"].(map[string]interface{}); ok {
		id, _ := dataObj["id"].(string)
		return id
	}
	return ""
}

// cleanTFESCIMTokens deletes all SCIM tokens for the org. Called on disable.
func cleanTFESCIMTokens(baseURL, apiToken, orgName string) {
	if apiToken == "" {
		return
	}
	_ = deleteAllTFESCIMTokens(baseURL, apiToken, orgName)
}

// cleanTFESCIMTokensForTarget is the disable-time SCIM cleanup helper.
func cleanTFESCIMTokensForTarget(baseURL, apiToken, target string) {
	orgName := tfeSAMLOrgName
	if orgName == "" {
		orgName = defaultSAMLOrgName
	}
	_ = strings.ToLower(target) // referenced to avoid unused import
	cleanTFESCIMTokens(baseURL, apiToken, orgName)
}

// cleanPreSCIMTFETeams removes TFE teams that were manually created by a prior
// non-SCIM 'hal tf saml enable'. When --scim is active, Authentik SCIM owns team
// creation; pre-existing manually-created teams must be removed first so the SCIM
// sync creates them fresh and the user can see the provisioning in action.
func cleanPreSCIMTFETeams(baseURL, apiToken, orgName string) {
	fmt.Println("  ⚙️  Checking for pre-existing non-SCIM TFE teams (admins, devs)...")
	tbody, _, err := integrations.TFERequest("GET", baseURL+"/api/v2/organizations/"+orgName+"/teams", apiToken, nil)
	if err != nil {
		fmt.Printf("  ⚠️  Could not list TFE teams: %v\n", err)
		return
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(tbody, &resp); err != nil {
		return
	}
	data, _ := resp["data"].([]interface{})
	scimOwnedTeams := map[string]bool{"admins": true, "devs": true}
	for _, t := range data {
		team, _ := t.(map[string]interface{})
		id, _ := team["id"].(string)
		attrs, _ := team["attributes"].(map[string]interface{})
		name, _ := attrs["name"].(string)
		if scimOwnedTeams[name] && id != "" {
			_, _, _ = integrations.TFERequest("DELETE", baseURL+"/api/v2/teams/"+id, apiToken, nil)
			fmt.Printf("  🗑️  Removed pre-existing non-SCIM team %q\n", name)
		}
	}
}
