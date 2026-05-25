package terraform

// TFE SCIM provisioning — Authentik outbound SCIM → TFE.
//
// SCIM in TFE requires SAML SSO to be enabled first.
//
// TFE SCIM maps:
//   SCIM User  → TFE User (created on first push)
//   SCIM Group → TFE Team (created automatically if it doesn't exist)
//
// SCIM tokens are org-scoped in TFE and generated via the API.
// Authentik must reach TFE from inside docker/podman (container-to-container)
// using the proxy container hostname and internal port, with TLS verification
// disabled (TFE uses a self-signed certificate in the lab environment).

import (
	"encoding/json"
	"fmt"
	"strings"

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
	// Authentik reaches TFE via the proxy container on hal-net.
	// TFE SCIM base path: /api/scim/v2
	scimEndpoint := fmt.Sprintf("https://%s:%d/api/scim/v2", proxyContainer, proxyPort)

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

	// 7. Force initial sync of all users and groups.
	if err := syncTFESCIMObjects(aktClient, providerPK); err != nil {
		fmt.Printf("  ⚠️  SCIM sync warning: %v\n", err)
	}

	return nil
}

// createTFESCIMToken creates a new SCIM token in TFE for the given organization.
// Returns the plain-text token value.
func createTFESCIMToken(baseURL, apiToken, orgName string) (string, error) {
	url := fmt.Sprintf("%s/api/v2/organizations/%s/scim-tokens", baseURL, orgName)
	body, _, err := integrations.TFERequest("POST", url, apiToken, map[string]interface{}{})
	if err != nil {
		// If a token already exists, TFE may return 422. Try to list and reuse.
		if listBody, _, listErr := integrations.TFERequest("GET", url, apiToken, nil); listErr == nil {
			if token, parseErr := extractFirstSCIMToken(listBody); parseErr == nil && token != "" {
				// We can't retrieve the plain-text token from a list response —
				// TFE only returns the token value at creation time.
				// Delete all existing tokens and create a fresh one.
				_ = deleteAllTFESCIMTokens(baseURL, apiToken, orgName)
				body2, _, err2 := integrations.TFERequest("POST", url, apiToken, map[string]interface{}{})
				if err2 != nil {
					return "", fmt.Errorf("recreate scim token: %w", err2)
				}
				return extractSCIMTokenValue(body2)
			}
		}
		return "", fmt.Errorf("POST %s: %w", url, err)
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

// syncTFESCIMObjects pushes all Authentik users and groups to TFE via SCIM sync.
func syncTFESCIMObjects(aktClient *integrations.AuthentikClient, providerPK int) error {
	fmt.Println("  ⚙️  Syncing users and groups to TFE...")

	users, err := aktClient.GetAllUsers()
	if err != nil {
		return fmt.Errorf("list authentik users: %w", err)
	}
	usersOK := 0
	for _, u := range users {
		if syncErr := aktClient.SyncSCIMObject(providerPK, "authentik.core.models.User", u.PK); syncErr != nil {
			fmt.Printf("  ⚠️  Sync failed for user %q: %v\n", u.Username, syncErr)
		} else {
			usersOK++
		}
	}

	groups, err := aktClient.GetAllGroups()
	if err != nil {
		return fmt.Errorf("list authentik groups: %w", err)
	}
	groupsOK := 0
	for _, g := range groups {
		if syncErr := aktClient.SyncSCIMObject(providerPK, "authentik.core.models.Group", g.PK); syncErr != nil {
			// TFE SCIM may reject built-in Authentik groups — only warn.
			fmt.Printf("  ⚠️  Sync failed for group %q: %v\n", g.Name, syncErr)
		} else {
			groupsOK++
		}
	}

	fmt.Printf("  ✅ Synced %d user(s) and %d group(s) to TFE\n", usersOK, groupsOK)
	return nil
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
