package vault

// agentic_iam_vault.go configures Vault for the Agentic IAM lab (ADR 0004,
// decisions 5 and 6): the OAuth resource server profile that trusts the
// vault-agentic issuer, the persona and demo agent entities, the internal
// groups that carry the personas' own rights, the Agent Registry record that
// holds the ceiling, and the lab's own database mount.
//
// Every ensure* function converges to the same state when re-run (the update
// verb), and teardownAgenticIAMVault only removes lab-owned objects.
//
// Naming: `hal vault oidc --scim` pushes every Authentik user and group into
// Vault as SCIM-managed entities and groups (alice, bob, finance, ...), and
// refuses pre-existing non-SCIM groups of the same name. The lab therefore
// prefixes its persona entities and groups with "agentic-iam-", looks them up
// by those names only, and never reads, changes or deletes a SCIM object. The
// persona binding comes from the entity alias (issuer + external_id = the JWT
// sub), so the token still says alice. The demo agent keeps the plain name
// finance-agent: SCIM does not push the Authentik Actor.

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	vault "github.com/hashicorp/vault/api"
)

const (
	// agenticIAMPrefix marks the lab's policies, persona entities and groups.
	agenticIAMPrefix = "agentic-iam-"

	// Ownership marker set as metadata on the lab's entities and groups, and as
	// the owner of its Agent Registry record. Teardown and update only touch
	// objects that carry it.
	agenticIAMMarkerKey   = "hal_lab"
	agenticIAMMarkerValue = "agentic-iam"
	agenticIAMOwner       = "hal-agentic-iam-lab"

	// agenticIAMProfileName is the OAuth resource server profile that validates
	// the OBO tokens of Authentik's vault-agentic application.
	agenticIAMProfileName = "authentik"

	// The demo agent: its entity, its alias external_id (the act.sub of the OBO
	// token) and its Agent Registry display name are all finance-agent.
	agenticIAMAgentName        = "finance-agent"
	agenticIAMAgentDescription = "HAL Agentic IAM lab demo agent: reads ACME financial reports and forecasts on behalf of a persona."

	// Policies.
	agenticIAMFinancePolicy = agenticIAMPrefix + "finance"               // persona rights of the finance group
	agenticIAMCeilingPolicy = agenticIAMPrefix + "finance-agent-ceiling" // the demo agent's ceiling

	// The lab's database mount and its single connection.
	agenticDBMount      = "agentic-db"
	agenticDBConnection = "acme"
	agenticDBDefaultTTL = "5m"
	agenticDBMaxTTL     = "15m"
	// Ephemeral MariaDB users read as obo-<role>-<random>, at most 31
	// characters, within the 32-character limit of mysql-database-plugin.
	agenticDBUsernameTemplate = "obo-{{.RoleName | truncate 18}}-{{random 8}}"
)

// agenticDBRole is one read-only Vault role on the lab's mount: SELECT on one
// acme table, nothing else.
type agenticDBRole struct {
	Name  string // Vault role; creds at agentic-db/creds/<Name>
	Table string // table of the acme schema
}

var (
	agenticDBQuarterlyResults = agenticDBRole{Name: "quarterly-results", Table: "quarterly_results"}
	agenticDBForecasts        = agenticDBRole{Name: "forecasts", Table: "forecasts"}
	agenticDBPayroll          = agenticDBRole{Name: "payroll", Table: "payroll"}

	agenticDBRoles = []agenticDBRole{agenticDBQuarterlyResults, agenticDBForecasts, agenticDBPayroll}

	// The ceiling allows financial reports and forecasts, not payroll
	// (scenario case 4 is refused by the ceiling alone).
	agenticIAMCeilingRoles = []agenticDBRole{agenticDBQuarterlyResults, agenticDBForecasts}
)

// agenticIAMPersona is a persona that Vault knows: an entity bound to the JWT
// sub by an alias. charlie has none, because the IdP refuses his delegation.
type agenticIAMPersona struct {
	Username string // Authentik username = JWT sub = alias external_id
	Group    string // the internal Vault group that carries their rights
}

// EntityName is the persona's lab-owned Vault entity (and alias) name.
func (p agenticIAMPersona) EntityName() string { return agenticIAMPrefix + p.Username }

// agenticIAMGroup is an internal Vault group that mirrors an Authentik group.
// HAL sets its members; nothing syncs them from a claim.
type agenticIAMGroup struct {
	Name     string
	Policies []string
}

var (
	agenticIAMFinanceGroup     = agenticIAMGroup{Name: agenticIAMPrefix + "finance", Policies: []string{agenticIAMFinancePolicy}}
	agenticIAMEngineeringGroup = agenticIAMGroup{Name: agenticIAMPrefix + "engineering"} // nothing on the lab's mount

	agenticIAMGroups = []agenticIAMGroup{agenticIAMFinanceGroup, agenticIAMEngineeringGroup}

	agenticIAMPersonas = []agenticIAMPersona{
		{Username: "alice", Group: agenticIAMFinanceGroup.Name},
		{Username: "bob", Group: agenticIAMEngineeringGroup.Name},
	}
)

// agenticIAMVaultConfig is what the agentic-iam command passes to the Vault
// side.
type agenticIAMVaultConfig struct {
	// Issuer of the vault-agentic application, as Authentik puts it in "iss":
	// integrations.AuthentikOIDCIssuer("vault-agentic"), i.e.
	// http://authentik.localhost:9100/application/o/vault-agentic/
	Issuer string
	// ClientID of the vault-agentic application: the profile's only audience.
	ClientID string
	// BrokerUser and BrokerPassword are the lab's MariaDB broker, created by
	// agenticIAMSeedSQL, which must run first. Vault rotates the password.
	BrokerUser     string
	BrokerPassword string
}

func (c agenticIAMVaultConfig) validate() error {
	u, err := url.Parse(strings.TrimSpace(c.Issuer))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("invalid issuer %q: want an http(s) URL", c.Issuer)
	}
	if strings.TrimSpace(c.ClientID) == "" {
		return errors.New("missing vault-agentic client ID")
	}
	return validateAgenticIAMBroker(c.BrokerUser, c.BrokerPassword)
}

// agenticIAMVaultState is what configureAgenticIAMVault set up, for the
// command's summary and status output.
type agenticIAMVaultState struct {
	ConfigID         string            // profile config_id (part of the synthesised alias mount accessor)
	PersonaEntityIDs map[string]string // persona username -> entity ID
	AgentEntityID    string
	RegistrationID   string // Agent Registry record ID
}

// configureAgenticIAMVault applies the whole Vault side of the lab, in
// dependency order. Run checkAgenticIAMPrerequisites first, and apply
// agenticIAMSeedSQL to MariaDB beforehand: this function connects with the
// broker password it is given, then rotates it.
func configureAgenticIAMVault(client *vault.Client, cfg agenticIAMVaultConfig) (*agenticIAMVaultState, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	issuer := normalizeIssuer(cfg.Issuer)
	state := &agenticIAMVaultState{PersonaEntityIDs: map[string]string{}}

	if err := ensureAgenticIAMPolicies(client); err != nil {
		return nil, err
	}

	configID, err := ensureOAuthResourceServerProfile(client, cfg.Issuer, cfg.ClientID)
	if err != nil {
		return nil, err
	}
	state.ConfigID = configID

	members := map[string][]string{}
	for _, p := range agenticIAMPersonas {
		id, err := ensureLabEntity(client, p.EntityName())
		if err != nil {
			return nil, err
		}
		if err := ensureOAuthEntityAlias(client, id, oauthAliasSpec{Name: p.EntityName(), Issuer: issuer, ExternalID: p.Username}, configID); err != nil {
			return nil, err
		}
		state.PersonaEntityIDs[p.Username] = id
		members[p.Group] = append(members[p.Group], id)
	}

	for _, g := range agenticIAMGroups {
		if err := ensureLabGroup(client, g, members[g.Name]); err != nil {
			return nil, err
		}
	}

	agentID, err := ensureLabEntity(client, agenticIAMAgentName)
	if err != nil {
		return nil, err
	}
	if err := ensureOAuthEntityAlias(client, agentID, oauthAliasSpec{Name: agenticIAMAgentName, Issuer: issuer, ExternalID: agenticIAMAgentName}, configID); err != nil {
		return nil, err
	}
	state.AgentEntityID = agentID

	regID, err := ensureAgentRegistration(client, agentID)
	if err != nil {
		return nil, err
	}
	state.RegistrationID = regID

	if err := ensureAgenticDB(client, cfg.BrokerUser, cfg.BrokerPassword); err != nil {
		return nil, err
	}
	return state, nil
}

// ─── policies ────────────────────────────────────────────────────────────────

// agenticDBCredsPath is the Vault path that issues credentials for a role.
func agenticDBCredsPath(role agenticDBRole) string {
	return agenticDBMount + "/creds/" + role.Name
}

// agenticIAMReadPolicy renders an HCL policy with read on the creds path of
// each role.
func agenticIAMReadPolicy(roles []agenticDBRole) string {
	var b strings.Builder
	for _, r := range roles {
		fmt.Fprintf(&b, "path %q {\n  capabilities = [\"read\"]\n}\n", agenticDBCredsPath(r))
	}
	return b.String()
}

// ensureAgenticIAMPolicies writes the finance persona policy (all three creds
// paths) and the demo agent's ceiling (no payroll). PutPolicy overwrites, so
// re-running converges.
func ensureAgenticIAMPolicies(client *vault.Client) error {
	if err := client.Sys().PutPolicy(agenticIAMFinancePolicy, agenticIAMReadPolicy(agenticDBRoles)); err != nil {
		return fmt.Errorf("write policy %s: %w", agenticIAMFinancePolicy, err)
	}
	if err := client.Sys().PutPolicy(agenticIAMCeilingPolicy, agenticIAMReadPolicy(agenticIAMCeilingRoles)); err != nil {
		return fmt.Errorf("write policy %s: %w", agenticIAMCeilingPolicy, err)
	}
	return nil
}

// ─── OAuth resource server profile ───────────────────────────────────────────

// normalizeIssuer applies Vault's issuer_id normalisation: trailing slashes
// trimmed, lowercased. Authentik's "iss" ends with a slash.
func normalizeIssuer(issuer string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(issuer), "/"))
}

// jwksURIForIssuer returns Authentik's JWKS endpoint for an application issuer
// (<issuer>/jwks/). The case of the path is kept.
func jwksURIForIssuer(issuer string) string {
	return strings.TrimRight(strings.TrimSpace(issuer), "/") + "/jwks/"
}

// oauthResourceServerPath is the API path of the lab's profile.
func oauthResourceServerPath() string {
	return oauthResourceServerConfigPath + "/" + agenticIAMProfileName
}

// oauthResourceServerPayload builds the profile body. issuer_id is immutable,
// so it is only sent on create. RAR stays mandatory
// (optional_authorization_details=false): it carries the task scope.
func oauthResourceServerPayload(issuer, clientID string, create bool) map[string]any {
	payload := map[string]any{
		"use_jwks":                       true,
		"jwks_uri":                       jwksURIForIssuer(issuer),
		"audiences":                      []string{clientID},
		"user_claim":                     "sub",
		"actor_claim":                    "act.sub",
		"supported_algorithms":           []string{"RS256"},
		"optional_authorization_details": false,
		"enabled":                        true,
	}
	if create {
		payload["issuer_id"] = normalizeIssuer(issuer)
	}
	return payload
}

// oauthProfileNeedsRecreate reports whether an existing profile differs in an
// immutable field: issuer_id, or a unique_id_claim other than the default jti.
func oauthProfileNeedsRecreate(existing map[string]any, issuer string) bool {
	if normalizeIssuer(stringField(existing, "issuer_id")) != normalizeIssuer(issuer) {
		return true
	}
	claim := stringField(existing, "unique_id_claim")
	return claim != "" && claim != "jti"
}

// ensureOAuthResourceServerProfile creates or updates the "authentik" profile
// and returns its config_id. When an immutable field differs, it deletes and
// recreates the profile; the config_id then changes, which is why
// ensureOAuthEntityAlias is given the new one.
func ensureOAuthResourceServerProfile(client *vault.Client, issuer, clientID string) (string, error) {
	path := oauthResourceServerPath()
	existing, err := readIfExists(client, path)
	if err != nil {
		return "", fmt.Errorf("read OAuth resource server profile %s: %w", agenticIAMProfileName, err)
	}

	create := existing == nil
	if existing != nil && oauthProfileNeedsRecreate(existing.Data, issuer) {
		if _, err := client.Logical().Delete(path); err != nil {
			return "", fmt.Errorf("delete OAuth resource server profile %s (issuer changed): %w", agenticIAMProfileName, err)
		}
		create = true
	}

	if _, err := client.Logical().Write(path, oauthResourceServerPayload(issuer, clientID, create)); err != nil {
		return "", fmt.Errorf("write OAuth resource server profile %s: %w", agenticIAMProfileName, err)
	}

	written, err := client.Logical().Read(path)
	if err != nil {
		return "", fmt.Errorf("read back OAuth resource server profile %s: %w", agenticIAMProfileName, err)
	}
	if written == nil {
		return "", fmt.Errorf("OAuth resource server profile %s is missing after write", agenticIAMProfileName)
	}
	configID := stringField(written.Data, "config_id")
	if configID == "" {
		return "", fmt.Errorf("OAuth resource server profile %s has no config_id", agenticIAMProfileName)
	}
	return configID, nil
}

// ─── entities and aliases ────────────────────────────────────────────────────

// agenticIAMMetadata is the ownership marker of the lab's entities and groups.
func agenticIAMMetadata() map[string]string {
	return map[string]string{agenticIAMMarkerKey: agenticIAMMarkerValue}
}

// hasAgenticIAMMarker reports whether an identity object's data carries the
// lab's ownership marker.
func hasAgenticIAMMarker(data map[string]any) bool {
	md, _ := data["metadata"].(map[string]any)
	v, _ := md[agenticIAMMarkerKey].(string)
	return v == agenticIAMMarkerValue
}

// ensureLabEntity returns the ID of the lab-owned entity with this name,
// creating it with the ownership marker if missing. It refuses an entity of
// that name that the lab does not own.
func ensureLabEntity(client *vault.Client, name string) (string, error) {
	existing, err := readIfExists(client, "identity/entity/name/"+name)
	if err != nil {
		return "", fmt.Errorf("read entity %s: %w", name, err)
	}
	if existing != nil {
		if !hasAgenticIAMMarker(existing.Data) {
			return "", fmt.Errorf("entity %s exists but is not owned by the Agentic IAM lab (no %s=%s metadata); delete or rename it",
				name, agenticIAMMarkerKey, agenticIAMMarkerValue)
		}
		if id := stringField(existing.Data, "id"); id != "" {
			return id, nil
		}
		return "", fmt.Errorf("entity %s has no ID", name)
	}

	resp, err := client.Logical().Write("identity/entity", map[string]any{
		"name":     name,
		"metadata": agenticIAMMetadata(),
	})
	if err != nil {
		return "", fmt.Errorf("create entity %s: %w", name, err)
	}
	if resp == nil || stringField(resp.Data, "id") == "" {
		return "", fmt.Errorf("create entity %s: no ID returned", name)
	}
	return stringField(resp.Data, "id"), nil
}

// oauthAliasSpec is an entity alias that binds a JWT claim to an entity. With
// issuer set, Vault synthesises the mount accessor from the profile
// (oauth-resource-server_<namespace>_<config_id>). Without issuer, the alias
// is accepted but authentication fails later with "no alias found".
type oauthAliasSpec struct {
	Name       string // lab-owned alias name (= the entity name)
	Issuer     string // normalised issuer, as stored in the profile
	ExternalID string // the claim value: sub for a persona, act.sub for the demo agent
}

// splitLabAliases sorts an entity's aliases named spec.Name into the one that
// is current (bound to the profile's config_id, same issuer and external_id
// when Vault reports them) and stale ones to delete. Aliases with another name
// are not the lab's and are ignored.
func splitLabAliases(aliases []any, spec oauthAliasSpec, configID string) (current string, stale []string) {
	for _, raw := range aliases {
		a, ok := raw.(map[string]any)
		if !ok || stringField(a, "name") != spec.Name {
			continue
		}
		id := stringField(a, "id")
		if id == "" {
			continue
		}
		matches := current == "" &&
			configID != "" && strings.Contains(stringField(a, "mount_accessor"), configID) &&
			fieldMatches(a, "issuer", spec.Issuer, normalizeIssuer) &&
			fieldMatches(a, "external_id", spec.ExternalID, nil)
		if matches {
			current = id
		} else {
			stale = append(stale, id)
		}
	}
	return current, stale
}

// fieldMatches is true when the field is absent or equal to want (after norm,
// if given). Absent means this Vault does not report it.
func fieldMatches(data map[string]any, key, want string, norm func(string) string) bool {
	got, present := data[key].(string)
	if !present || got == "" {
		return true
	}
	if norm != nil {
		return norm(got) == norm(want)
	}
	return got == want
}

// ensureOAuthEntityAlias makes the entity carry exactly one alias named
// spec.Name, bound to the current profile. A profile recreated after an
// issuer change has a new config_id, so the old alias is deleted and a new one
// created. Should this Vault report aliases in another shape, the alias is
// simply recreated on each run, which is still correct.
func ensureOAuthEntityAlias(client *vault.Client, entityID string, spec oauthAliasSpec, configID string) error {
	entity, err := client.Logical().Read("identity/entity/id/" + entityID)
	if err != nil {
		return fmt.Errorf("read entity %s: %w", entityID, err)
	}
	if entity == nil {
		return fmt.Errorf("entity %s (%s) not found", spec.Name, entityID)
	}
	aliases, _ := entity.Data["aliases"].([]any)
	current, stale := splitLabAliases(aliases, spec, configID)
	for _, id := range stale {
		if _, err := client.Logical().Delete("identity/entity-alias/id/" + id); err != nil {
			return fmt.Errorf("delete stale alias %s of %s: %w", id, spec.Name, err)
		}
	}
	if current != "" {
		return nil
	}

	if _, err := client.Logical().Write("identity/entity-alias", oauthAliasPayload(entityID, spec)); err != nil {
		return fmt.Errorf("create alias %s (issuer=%s, external_id=%s): %w", spec.Name, spec.Issuer, spec.ExternalID, err)
	}
	return nil
}

// oauthAliasPayload builds the entity alias body. There is no mount_accessor:
// Vault derives it from issuer.
func oauthAliasPayload(entityID string, spec oauthAliasSpec) map[string]any {
	return map[string]any{
		"name":         spec.Name,
		"canonical_id": entityID,
		"issuer":       spec.Issuer,
		"external_id":  spec.ExternalID,
	}
}

// ─── groups ──────────────────────────────────────────────────────────────────

// ensureLabGroup creates or updates an internal group with these policies and
// exactly these member entities. It refuses a group of that name that the lab
// does not own.
func ensureLabGroup(client *vault.Client, g agenticIAMGroup, memberIDs []string) error {
	path := "identity/group/name/" + g.Name
	existing, err := readIfExists(client, path)
	if err != nil {
		return fmt.Errorf("read group %s: %w", g.Name, err)
	}
	if existing != nil && !hasAgenticIAMMarker(existing.Data) {
		return fmt.Errorf("group %s exists but is not owned by the Agentic IAM lab (no %s=%s metadata); delete or rename it",
			g.Name, agenticIAMMarkerKey, agenticIAMMarkerValue)
	}
	if _, err := client.Logical().Write(path, labGroupPayload(g, memberIDs, existing == nil)); err != nil {
		return fmt.Errorf("write group %s: %w", g.Name, err)
	}
	return nil
}

// labGroupPayload builds the group body. The type cannot change after
// creation, so it is only sent on create.
func labGroupPayload(g agenticIAMGroup, memberIDs []string, create bool) map[string]any {
	policies := g.Policies
	if policies == nil {
		policies = []string{}
	}
	if memberIDs == nil {
		memberIDs = []string{}
	}
	payload := map[string]any{
		"policies":          policies,
		"member_entity_ids": memberIDs,
		"metadata":          agenticIAMMetadata(),
	}
	if create {
		payload["type"] = "internal"
	}
	return payload
}

// ─── Agent Registry ──────────────────────────────────────────────────────────

// agentRegistrationPayload builds the agent-registry/register body. With id
// set, Vault updates that record instead of creating one. ceiling_policies
// gets default and default-ceiling added by Vault.
func agentRegistrationPayload(entityID, registrationID string) map[string]any {
	payload := map[string]any{
		"display_name":     agenticIAMAgentName,
		"description":      agenticIAMAgentDescription,
		"entity_id":        entityID,
		"owner":            agenticIAMOwner,
		"ceiling_policies": []string{agenticIAMCeilingPolicy},
	}
	if registrationID != "" {
		payload["id"] = registrationID
	}
	return payload
}

// findAgentRegistration returns the demo agent's record, looked up by display
// name then by entity ID (one record per entity), or nil.
func findAgentRegistration(client *vault.Client, entityID string) (*vault.Secret, error) {
	lookups := []string{agentRegistryMount + "/registration/display-name/" + agenticIAMAgentName}
	if entityID != "" {
		lookups = append(lookups, agentRegistryMount+"/registration/entity-id/"+entityID)
	}
	for _, path := range lookups {
		rec, err := readIfExists(client, path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		if rec != nil {
			return rec, nil
		}
	}
	return nil, nil
}

// ensureAgentRegistration registers the demo agent's entity with its ceiling,
// or updates the existing record, and returns the record ID. Vault refuses
// OAuth credentials from an entity with no record.
func ensureAgentRegistration(client *vault.Client, entityID string) (string, error) {
	existing, err := findAgentRegistration(client, entityID)
	if err != nil {
		return "", err
	}
	registrationID := ""
	if existing != nil {
		if owner := stringField(existing.Data, "owner"); owner != agenticIAMOwner {
			return "", fmt.Errorf("agent registration %s exists with owner %q, not %q: it is not the Agentic IAM lab's",
				agenticIAMAgentName, owner, agenticIAMOwner)
		}
		registrationID = stringField(existing.Data, "id")
	}

	resp, err := client.Logical().Write(agentRegistryMount+"/register", agentRegistrationPayload(entityID, registrationID))
	if err != nil {
		return "", fmt.Errorf("register demo agent %s: %w", agenticIAMAgentName, err)
	}
	if resp != nil {
		if id := stringField(resp.Data, "id"); id != "" {
			return id, nil
		}
	}
	if registrationID != "" {
		return registrationID, nil
	}
	return "", fmt.Errorf("register demo agent %s: no registration ID returned", agenticIAMAgentName)
}

// ─── database mount ──────────────────────────────────────────────────────────

// agenticDBConnectionURL is the DSN of the shared MariaDB on hal-net.
func agenticDBConnectionURL() string {
	return fmt.Sprintf("{{username}}:{{password}}@tcp(%s:%d)/", vaultMariaDBContainer, vaultMariaDBPort)
}

// agenticDBConnectionPayload builds the acme connection body.
func agenticDBConnectionPayload(brokerUser, brokerPassword string) map[string]any {
	allowed := make([]string, 0, len(agenticDBRoles))
	for _, r := range agenticDBRoles {
		allowed = append(allowed, r.Name)
	}
	return map[string]any{
		"plugin_name":       "mysql-database-plugin",
		"connection_url":    agenticDBConnectionURL(),
		"username":          brokerUser,
		"password":          brokerPassword,
		"allowed_roles":     allowed,
		"username_template": agenticDBUsernameTemplate,
	}
}

// agenticDBRolePayload builds a role body: an ephemeral user with SELECT on
// one acme table only, dropped when its short lease ends.
func agenticDBRolePayload(role agenticDBRole) map[string]any {
	return map[string]any{
		"db_name": agenticDBConnection,
		"creation_statements": []string{
			"CREATE USER '{{name}}'@'%' IDENTIFIED BY '{{password}}';",
			fmt.Sprintf("GRANT SELECT ON %s.%s TO '{{name}}'@'%%';", agenticIAMSchema, role.Table),
		},
		"revocation_statements": []string{"DROP USER IF EXISTS '{{name}}'@'%';"},
		"default_ttl":           agenticDBDefaultTTL,
		"max_ttl":               agenticDBMaxTTL,
	}
}

// ensureAgenticDB mounts agentic-db/ if missing (an existing mount is kept, so
// live leases survive an update), writes the acme connection with the broker
// credentials, rotates the broker password so only Vault knows it, and writes
// the three roles. agenticIAMSeedSQL must have run first: it resets the broker
// password to the one given here.
func ensureAgenticDB(client *vault.Client, brokerUser, brokerPassword string) error {
	mounts, err := client.Sys().ListMounts()
	if err != nil {
		return fmt.Errorf("list mounts: %w", err)
	}
	if m, ok := mounts[agenticDBMount+"/"]; ok && m != nil {
		if m.Type != "database" {
			return fmt.Errorf("%s/ is already mounted with type %q, not database", agenticDBMount, m.Type)
		}
	} else if err := client.Sys().Mount(agenticDBMount, &vault.MountInput{
		Type:        "database",
		Description: "HAL Agentic IAM lab: dynamic MariaDB credentials for the acme schema",
	}); err != nil {
		return fmt.Errorf("mount %s/: %w", agenticDBMount, err)
	}

	configPath := agenticDBMount + "/config/" + agenticDBConnection
	if _, err := client.Logical().Write(configPath, agenticDBConnectionPayload(brokerUser, brokerPassword)); err != nil {
		return fmt.Errorf("configure connection %s: %w", configPath, err)
	}
	if _, err := client.Logical().Write(agenticDBMount+"/rotate-root/"+agenticDBConnection, map[string]any{}); err != nil {
		return fmt.Errorf("rotate the broker password of %s: %w", configPath, err)
	}

	for _, r := range agenticDBRoles {
		if _, err := client.Logical().Write(agenticDBMount+"/roles/"+r.Name, agenticDBRolePayload(r)); err != nil {
			return fmt.Errorf("write role %s: %w", r.Name, err)
		}
	}
	return nil
}

// ─── teardown ────────────────────────────────────────────────────────────────

// teardownAgenticIAMVault removes the lab from Vault in reverse order: it
// force-revokes the agentic-db/ leases (Vault drops the ephemeral MariaDB
// users), unmounts agentic-db/, then deletes the Agent Registry record, the
// profile, the aliases and entities, the groups and the policies. It is
// best-effort: it carries on after a failure and returns them all joined.
// Objects without the lab's marker are left alone; missing ones are fine.
func teardownAgenticIAMVault(client *vault.Client) error {
	var errs []error
	fail := func(what string, err error) {
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", what, err))
		}
	}

	// 1. The database mount, leases first.
	mounts, err := client.Sys().ListMounts()
	fail("list mounts", err)
	if m, ok := mounts[agenticDBMount+"/"]; ok && m != nil && m.Type == "database" {
		fail("revoke "+agenticDBMount+"/ leases", client.Sys().RevokeForce(agenticDBMount+"/"))
		fail("unmount "+agenticDBMount+"/", client.Sys().Unmount(agenticDBMount))
	}

	// 2. The Agent Registry record, while the demo agent entity still exists.
	rec, err := readIfExists(client, agentRegistryMount+"/registration/display-name/"+agenticIAMAgentName)
	fail("read the demo agent registration", err)
	if rec != nil && stringField(rec.Data, "owner") == agenticIAMOwner {
		_, err := client.Logical().Delete(agentRegistryMount + "/registration/display-name/" + agenticIAMAgentName)
		fail("delete the demo agent registration", err)
	}

	// 3. The profile. Deleting a missing one succeeds, and without the
	// license feature no profile can exist.
	if _, err := client.Logical().Delete(oauthResourceServerPath()); err != nil && !isVaultNotFound(err) && !isFeatureNotEnabled(err) {
		fail("delete OAuth resource server profile "+agenticIAMProfileName, err)
	}

	// 4. Aliases, then entities.
	entityNames := []string{agenticIAMAgentName}
	for _, p := range agenticIAMPersonas {
		entityNames = append(entityNames, p.EntityName())
	}
	for _, name := range entityNames {
		fail("delete entity "+name, deleteLabEntity(client, name))
	}

	// 5. Groups.
	for _, g := range agenticIAMGroups {
		fail("delete group "+g.Name, deleteLabIdentityObject(client, "identity/group/name/"+g.Name))
	}

	// 6. Policies: the prefix makes them the lab's.
	for _, p := range []string{agenticIAMFinancePolicy, agenticIAMCeilingPolicy} {
		fail("delete policy "+p, client.Sys().DeletePolicy(p))
	}

	return errors.Join(errs...)
}

// deleteLabEntity deletes the lab's aliases of a lab-owned entity (those named
// like the entity), then the entity itself.
func deleteLabEntity(client *vault.Client, name string) error {
	entity, err := readIfExists(client, "identity/entity/name/"+name)
	if err != nil || entity == nil || !hasAgenticIAMMarker(entity.Data) {
		return err
	}
	aliases, _ := entity.Data["aliases"].([]any)
	for _, raw := range aliases {
		a, ok := raw.(map[string]any)
		if !ok || stringField(a, "name") != name || stringField(a, "id") == "" {
			continue
		}
		if _, err := client.Logical().Delete("identity/entity-alias/id/" + stringField(a, "id")); err != nil && !isVaultNotFound(err) {
			return fmt.Errorf("delete alias %s: %w", stringField(a, "id"), err)
		}
	}
	_, err = client.Logical().Delete("identity/entity/name/" + name)
	return err
}

// deleteLabIdentityObject deletes an identity object read at path if it
// carries the lab's marker.
func deleteLabIdentityObject(client *vault.Client, path string) error {
	obj, err := readIfExists(client, path)
	if err != nil || obj == nil || !hasAgenticIAMMarker(obj.Data) {
		return err
	}
	_, err = client.Logical().Delete(path)
	return err
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// readIfExists reads a path and returns nil, nil when the object is missing.
// Vault answers a missing object with 404, or for the Agent Registry with 400
// "agent with provided display_name does not exist".
func readIfExists(client *vault.Client, path string) (*vault.Secret, error) {
	secret, err := client.Logical().Read(path)
	if err != nil {
		if isVaultNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return secret, nil
}

// isVaultNotFound reports a "no such object" answer from Vault.
func isVaultNotFound(err error) bool {
	var respErr *vault.ResponseError
	if !errors.As(err, &respErr) {
		return false
	}
	switch respErr.StatusCode {
	case http.StatusNotFound:
		return true
	case http.StatusBadRequest:
		for _, msg := range respErr.Errors {
			lower := strings.ToLower(msg)
			if strings.Contains(lower, "does not exist") || strings.Contains(lower, "not found") {
				return true
			}
		}
	}
	return false
}

// stringField returns data[key] as a string, or "".
func stringField(data map[string]any, key string) string {
	s, _ := data[key].(string)
	return s
}
