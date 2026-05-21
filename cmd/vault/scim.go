package vault

// SCIM provisioning — Vault Enterprise + Authentik outbound sync.
//
// Vault SCIM maps:
//   SCIM User  → Vault Entity
//   SCIM Group → Vault internal identity Group
//
// alias_mount_accessor is intentionally omitted — the OIDC auth method handles
// entity alias creation when users log in. SCIM's primary role here is group
// sync: groups created in Authentik automatically appear in Vault.
//
// Vault SCIM is a beta feature requiring Vault Enterprise.

import (
	"fmt"
	"strings"

	"hal/internal/integrations"

	vault "github.com/hashicorp/vault/api"
)

const (
	scimClientName = "authentik-scim"
	scimEntityName = "scim-client-authentik"
	scimTokenRole  = "authentik-scim"
	scimPolicyName = "scim-client"
	// Authentik reaches Vault via Docker internal DNS on hal-net.
	scimVaultBaseURL = "http://hal-vault:8200/v1/identity/scim/v2"
)

// configureSCIM activates Vault Enterprise SCIM, creates a bearer-token-authenticated
// SCIM client, and wires Authentik to push groups and users into Vault.
// appSlug is the Authentik application slug to assign the SCIM backchannel provider to.
func configureSCIM(client *vault.Client, aktClient *integrations.AuthentikClient, appSlug string) error {
	// 0. Remove any groups that were pre-created by a prior non-SCIM OIDC enable.
	// When `hal vault oidc enable` is run first, it creates `admin` and `user-ro` as
	// external Vault groups. If the user then re-runs with --scim, those groups are
	// still present and Authentik SCIM will hit 409 trying to create the same names.
	// Only delete groups that are NOT already SCIM-managed (scim_client_id empty).
	fmt.Println("  ⚙️  Checking for pre-existing non-SCIM identity groups...")
	for _, name := range []string{"admin", "user-ro"} {
		resp, err := client.Logical().Read("identity/group/name/" + name)
		if err != nil || resp == nil {
			continue // doesn't exist — nothing to do
		}
		if scimID, _ := resp.Data["scim_client_id"].(string); scimID != "" {
			fmt.Printf("  ℹ️  Group %q already SCIM-managed — skipping\n", name)
			continue
		}
		// External group created by the non-SCIM path — remove it so SCIM can own it.
		if _, err := client.Logical().Delete("identity/group/name/" + name); err != nil {
			fmt.Printf("  ⚠️  Could not remove pre-existing group %q: %v\n", name, err)
		} else {
			fmt.Printf("  🗑️  Removed pre-existing non-SCIM group %q\n", name)
		}
	}

	// 1. Activate the SCIM feature flag.
	// "Activating the SCIM flag is a one-time action" — ignore errors that indicate
	// the flag is already set so re-runs are safe.
	fmt.Println("  ⚙️  Activating Vault SCIM feature flag...")
	if _, err := client.Logical().Write("sys/activation-flags/enable-scim/activate", nil); err != nil {
		if !strings.Contains(err.Error(), "already") && !strings.Contains(err.Error(), "activated") {
			return fmt.Errorf("activate SCIM flag: %w", err)
		}
		fmt.Println("  ℹ️  SCIM flag already active — continuing")
	}

	// 2. SCIM client policy.
	// "patch" capability is required in addition to "update" for HTTP PATCH requests
	// (Vault 1.14+). Without it, Authentik's group membership PATCH is rejected with
	// permission denied, leaving group member_entity_ids permanently empty.
	fmt.Println("  ⚙️  Writing scim-client policy...")
	if err := client.Sys().PutPolicy(scimPolicyName, `
path "identity/scim/v2/*" {
  capabilities = ["create", "read", "update", "patch", "delete", "list"]
}
`); err != nil {
		return fmt.Errorf("write scim policy: %w", err)
	}

	// 3. Vault entity representing the SCIM client.
	fmt.Println("  ⚙️  Creating Vault entity for SCIM client...")
	entityID, err := upsertEntity(client, scimEntityName)
	if err != nil {
		return err
	}
	fmt.Printf("  ℹ️  SCIM entity ID: %s\n", entityID)

	// 4. Auth mount accessors.
	auths, err := client.Sys().ListAuth()
	if err != nil {
		return fmt.Errorf("list auth mounts: %w", err)
	}
	tokenMount, ok := auths["token/"]
	if !ok {
		return fmt.Errorf("token auth mount not found")
	}
	tokenAccessor := tokenMount.Accessor
	oidcMount, ok := auths["oidc/"]
	if !ok {
		return fmt.Errorf("oidc auth mount not found — run 'hal vault oidc enable' first")
	}
	oidcAccessor := oidcMount.Accessor
	fmt.Printf("  ℹ️  Token accessor: %s  OIDC accessor: %s\n", tokenAccessor, oidcAccessor)

	// 5. Vault SCIM client.
	// alias_mount_accessor = oidc/ so each SCIM-provisioned user gets an entity alias
	// on the OIDC mount automatically. This means the SCIM entity and the OIDC login
	// entity are the same — group memberships sync'd via SCIM are immediately visible
	// when the user logs in via OIDC.
	// alias_mount_accessor is immutable after creation, so we skip create if the client
	// already exists and let cleanVaultSCIM handle full teardown on disable.
	fmt.Println("  ⚙️  Creating Vault SCIM client (authentik-scim)...")
	existing, _ := client.Logical().Read("identity/scim/client/" + scimClientName)
	if existing == nil {
		_, err := client.Logical().Write("identity/scim/client/"+scimClientName, map[string]interface{}{
			"access_grant_principal": entityID,
			"alias_mount_accessor":   oidcAccessor,
		})
		if err != nil {
			return fmt.Errorf("create scim client: %w\n\n  Diagnostics: entityID=%q oidcAccessor=%q\n  Hint: run 'vault delete identity/scim/client/%s' then retry",
				err, entityID, oidcAccessor, scimClientName)
		}
	} else {
		fmt.Println("  ℹ️  SCIM client already exists — skipping creation")
	}

	// 6. Entity alias on token auth mount so a minted token resolves to the SCIM entity.
	fmt.Println("  ⚙️  Creating entity alias on token mount...")
	_, aliasErr := client.Logical().Write("identity/entity-alias", map[string]interface{}{
		"name":           scimClientName,
		"mount_accessor": tokenAccessor,
		"canonical_id":   entityID,
	})
	if aliasErr != nil && !strings.Contains(aliasErr.Error(), "combination of mount and entity alias already exists") {
		return fmt.Errorf("create entity alias: %w", aliasErr)
	}

	// 7. Token role that pins allowed entity aliases.
	fmt.Println("  ⚙️  Creating SCIM token role...")
	if _, err := client.Logical().Write("auth/token/roles/"+scimTokenRole, map[string]interface{}{
		"allowed_entity_aliases":  []string{scimClientName},
		"orphan":                  true,
		"token_no_default_policy": true,
		"token_period":            "768h", // 32-day renewable
	}); err != nil {
		return fmt.Errorf("create token role: %w", err)
	}

	// 8. Mint bearer token that Authentik will send to Vault SCIM endpoints.
	fmt.Println("  ⚙️  Minting SCIM bearer token...")
	sec, err := client.Auth().Token().CreateWithRole(&vault.TokenCreateRequest{
		Policies:        []string{scimPolicyName},
		NoDefaultPolicy: true,
		EntityAlias:     scimClientName,
	}, scimTokenRole)
	if err != nil {
		return fmt.Errorf("mint scim bearer token: %w", err)
	}
	bearerToken := sec.Auth.ClientToken

	// 9. Configure Authentik outbound SCIM provider → Vault.
	fmt.Println("  ⚙️  Configuring Authentik SCIM provider → Vault...")
	userMappings, groupMappings, err := aktClient.GetDefaultSCIMPropertyMappings()
	if err != nil {
		return fmt.Errorf("get scim property mappings: %w", err)
	}

	providerPK, err := aktClient.CreateSCIMProvider(
		"vault-scim-provider",
		scimVaultBaseURL,
		bearerToken,
		userMappings,
		groupMappings,
	)
	if err != nil {
		return fmt.Errorf("create authentik scim provider: %w", err)
	}

	// 10. Assign the SCIM provider as a backchannel provider on the Vault application.
	// Without this step, Authentik never activates the outbound sync.
	fmt.Println("  ⚙️  Assigning SCIM provider as backchannel on Vault application...")
	if err := aktClient.SetApplicationBackchannelProviders(appSlug, []int{providerPK}); err != nil {
		return fmt.Errorf("assign backchannel provider: %w", err)
	}

	// 11. Verify the provider is reachable and show sync status.
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

	fmt.Println()
	fmt.Println("  ✅ SCIM provisioning configured")
	fmt.Printf("     Endpoint : %s\n", scimVaultBaseURL)
	fmt.Println()
	fmt.Println("  📋 SCIM behaviour:")
	fmt.Println("     • User creation   → auto-propagated to Vault  ✅")
	fmt.Println("     • Group creation  → auto-propagated to Vault  ✅")
	fmt.Println("     • Group membership changes → NOT auto-propagated ⚠️")
	fmt.Println()
	fmt.Println("     Authentik does not fire an outbound SCIM event when a user is")
	fmt.Println("     added to / removed from a group. To propagate membership changes,")
	fmt.Println("     trigger a per-object group sync as an Authentik admin:")
	fmt.Println()
	fmt.Printf("     # 1. Find the group PK\n")
	fmt.Printf("     curl -s -H 'Authorization: Bearer %s' \\\n", aktClient.Token())
	fmt.Printf("       '%s/api/v3/core/groups/?search=<group-name>' | jq '.results[0].pk'\n", aktClient.BaseURL())
	fmt.Println()
	fmt.Printf("     # 2. Trigger per-object sync for that group (replace <pk>)\n")
	fmt.Printf("     curl -s -X POST -H 'Authorization: Bearer %s' \\\n", aktClient.Token())
	fmt.Printf("       -H 'Content-Type: application/json' \\\n")
	fmt.Printf("       -d '{\"sync_object_model\":\"authentik.core.models.Group\",\"sync_object_id\":\"<pk>\"}' \\\n")
	fmt.Printf("       '%s/api/v3/providers/scim/%d/sync/object/'\n", aktClient.BaseURL(), providerPK)
	fmt.Println()
	fmt.Println("     Verify in Vault:")
	fmt.Println("       vault list identity/group/name")
	fmt.Println("       vault read identity/group/name/<group-name>")
	fmt.Println()
	return nil
}

// upsertEntity creates a Vault entity by name, or reads the existing one if it
// already exists. Returns the entity ID.
func upsertEntity(client *vault.Client, name string) (string, error) {
	resp, err := client.Logical().Write("identity/entity", map[string]interface{}{"name": name})
	if err == nil && resp != nil && resp.Data != nil {
		if id, ok := resp.Data["id"].(string); ok && id != "" {
			return id, nil
		}
	}
	// Entity already exists or response body was empty — look up by name.
	lookup, err2 := client.Logical().Read("identity/entity/name/" + name)
	if err2 != nil || lookup == nil {
		if err != nil {
			return "", fmt.Errorf("create entity %q: %w", name, err)
		}
		return "", fmt.Errorf("entity %q not found after create", name)
	}
	id, _ := lookup.Data["id"].(string)
	if id == "" {
		return "", fmt.Errorf("entity %q has empty ID", name)
	}
	return id, nil
}

// cleanVaultSCIM removes Vault SCIM configuration. No-op if resources do not exist.
func cleanVaultSCIM(client *vault.Client) {
	_, _ = client.Logical().Delete("identity/scim/client/" + scimClientName)
	_, _ = client.Logical().Delete("auth/token/roles/" + scimTokenRole)
	_ = client.Sys().DeletePolicy(scimPolicyName)
}
