package integrations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// ─── Idempotent Authentik helpers ─────────────────────────────────────────────
//
// The Create* helpers in authentik.go predate labs that re-reconcile on update.
// The Ensure* helpers below look an object up by its unique key first, then
// update it in place or create it. Running them twice never duplicates an
// object, and never rotates a client secret or a token key.
//
// Field names follow the Authentik 2026.8 OpenAPI schema (/api/v3/schema/).

// AuthentikPersonaPassword is the password of every persona HAL creates in
// Authentik. Like the other Authentik labs, it is a hard-coded lab credential.
const AuthentikPersonaPassword = "password"

// AuthentikGrantTokenExchange is the RFC 8693 token exchange grant type.
const AuthentikGrantTokenExchange = "urn:ietf:params:oauth:grant-type:token-exchange"

// AuthentikOAuth2TokenEndpoint returns the token endpoint shared by every
// Authentik OAuth2 provider. Like AuthentikOIDCIssuer, it uses
// authentik.localhost so the issuer is the same from the host and from hal-net.
func AuthentikOAuth2TokenEndpoint() string {
	return AuthentikAdminURL() + "/application/o/token/"
}

// AuthentikOAuth2AuthorizeEndpoint returns the authorization endpoint shared by
// every Authentik OAuth2 provider.
func AuthentikOAuth2AuthorizeEndpoint() string {
	return AuthentikAdminURL() + "/application/o/authorize/"
}

// AuthentikOIDCJWKSURI returns the JWKS URI of the application with this slug.
func AuthentikOIDCJWKSURI(slug string) string {
	return AuthentikOIDCIssuer(slug) + "jwks/"
}

// AuthentikOIDCEndSessionEndpoint returns the RP-initiated logout endpoint of the
// application with this slug. It runs the provider's invalidation flow, then
// redirects to post_logout_redirect_uri, which requires id_token_hint once the
// provider registers post-logout redirect URIs.
func AuthentikOIDCEndSessionEndpoint(slug string) string {
	return AuthentikOIDCIssuer(slug) + "end-session/"
}

// AuthentikLogoutFlowSlug is Authentik's default invalidation flow. Its user
// logout stage ends the Authentik session, so that the next login asks who is
// signing in. default-provider-invalidation-flow, which GetDefaultInvalidationFlowPK
// prefers, has no stage: it only says "You've logged out of <app>" and keeps the
// session, so the next login silently signs the same user back in.
const AuthentikLogoutFlowSlug = "default-invalidation-flow"

// GetFlowPKBySlug returns the pk of the flow with this slug.
func (c *AuthentikClient) GetFlowPKBySlug(slug string) (string, error) {
	type flow struct {
		PK   string `json:"pk"`
		Slug string `json:"slug"`
	}
	flows, err := akFind[flow](c, "/api/v3/flows/instances/", url.Values{"slug": {slug}})
	if err != nil {
		return "", fmt.Errorf("look up flow %q: %w", slug, err)
	}
	for _, f := range flows {
		if f.Slug == slug {
			return f.PK, nil
		}
	}
	return "", fmt.Errorf("flow %q not found", slug)
}

// akList is the envelope of a paginated Authentik list response.
type akList[T any] struct {
	Results []T `json:"results"`
}

type akGroup struct {
	PK   string `json:"pk"`
	Name string `json:"name"`
}

type akUser struct {
	PK       int      `json:"pk"`
	Username string   `json:"username"`
	Type     string   `json:"type"`
	Groups   []string `json:"groups"`
}

type akOAuth2Provider struct {
	PK           int    `json:"pk"`
	Name         string `json:"name"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

type akApplication struct {
	Slug    string `json:"slug"`
	PBMUUID string `json:"pbm_uuid"`
}

type akScopeMapping struct {
	PK   string `json:"pk"`
	Name string `json:"name"`
}

type akPolicyBinding struct {
	PK      string  `json:"pk"`
	Group   *string `json:"group"`
	Policy  *string `json:"policy"`
	User    *int    `json:"user"`
	Enabled bool    `json:"enabled"`
	Negate  bool    `json:"negate"`
}

type akToken struct {
	Identifier string `json:"identifier"`
	Intent     string `json:"intent"`
	User       int    `json:"user"`
	Expiring   bool   `json:"expiring"`
}

// doInto is do with a typed response: a non-nil out receives the decoded body.
func (c *AuthentikClient) doInto(method, path string, body, out any) (int, error) {
	raw, status, err := c.send(method, path, body)
	if err != nil || out == nil || len(raw) == 0 {
		return status, err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return status, fmt.Errorf("decode response: %w", err)
	}
	return status, nil
}

// akFind lists the objects at path that match query. Lookups filter on a unique
// field, so one page is enough.
func akFind[T any](c *AuthentikClient, path string, query url.Values) ([]T, error) {
	var page akList[T]
	if _, err := c.doInto("GET", path+"?"+query.Encode(), nil, &page); err != nil {
		return nil, err
	}
	return page.Results, nil
}

// akDelete deletes the object at path. A missing object is not an error.
func (c *AuthentikClient) akDelete(path string) error {
	status, err := c.doInto("DELETE", path, nil, nil)
	if status == http.StatusNotFound {
		return nil
	}
	return err
}

// orEmpty turns a nil slice into an empty one, so that it is sent as [] rather
// than null: Authentik rejects null for list fields such as redirect_uris.
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// ─── Groups and users ─────────────────────────────────────────────────────────

func (c *AuthentikClient) findGroup(name string) (*akGroup, error) {
	groups, err := akFind[akGroup](c, "/api/v3/core/groups/", url.Values{"name": {name}})
	if err != nil {
		return nil, err
	}
	for i := range groups {
		if groups[i].Name == name {
			return &groups[i], nil
		}
	}
	return nil, nil
}

// EnsureGroup returns the pk of the group with this name, creating it if absent.
func (c *AuthentikClient) EnsureGroup(name string) (string, error) {
	existing, err := c.findGroup(name)
	if err != nil {
		return "", fmt.Errorf("look up group %q: %w", name, err)
	}
	if existing != nil {
		return existing.PK, nil
	}
	var created akGroup
	if _, err := c.doInto("POST", "/api/v3/core/groups/", map[string]any{
		"name":         name,
		"is_superuser": false,
	}, &created); err != nil {
		return "", fmt.Errorf("create group %q: %w", name, err)
	}
	return created.PK, nil
}

// DeleteGroupByName deletes the group with this name. Its memberships go with it.
// No-op if not found.
func (c *AuthentikClient) DeleteGroupByName(name string) error {
	existing, err := c.findGroup(name)
	if err != nil || existing == nil {
		return err
	}
	return c.akDelete("/api/v3/core/groups/" + existing.PK + "/")
}

// findUser looks a user up by username. Core Actors are users too, so this also
// finds the demo agent's actor.
func (c *AuthentikClient) findUser(username string) (*akUser, error) {
	users, err := akFind[akUser](c, "/api/v3/core/users/", url.Values{"username": {username}})
	if err != nil {
		return nil, err
	}
	for i := range users {
		if users[i].Username == username {
			return &users[i], nil
		}
	}
	return nil, nil
}

// EnsureUser returns the pk of the user with this username, and makes it a member
// of every group in groupPKs. An absent user is created as an internal user with
// this password. An existing user keeps its password, its profile and its other
// groups: personas are shared, since hal vault oidc and hal tf saml create alice
// and bob too.
func (c *AuthentikClient) EnsureUser(username, displayName, email, password string, groupPKs []string) (int, error) {
	existing, err := c.findUser(username)
	if err != nil {
		return 0, fmt.Errorf("look up user %q: %w", username, err)
	}
	if existing != nil {
		member := make(map[string]bool, len(existing.Groups))
		for _, pk := range existing.Groups {
			member[pk] = true
		}
		for _, groupPK := range groupPKs {
			if member[groupPK] {
				continue
			}
			if err := c.AddUserToGroup(groupPK, existing.PK); err != nil {
				return 0, fmt.Errorf("add user %q to group %s: %w", username, groupPK, err)
			}
		}
		return existing.PK, nil
	}

	var created akUser
	if _, err := c.doInto("POST", "/api/v3/core/users/", map[string]any{
		"username":  username,
		"name":      displayName,
		"email":     email,
		"type":      "internal",
		"is_active": true,
		"groups":    orEmpty(groupPKs),
	}, &created); err != nil {
		return 0, fmt.Errorf("create user %q: %w", username, err)
	}
	if _, err := c.doInto("POST", fmt.Sprintf("/api/v3/core/users/%d/set_password/", created.PK), map[string]any{
		"password": password,
	}, nil); err != nil {
		return 0, fmt.Errorf("set password of user %q: %w", username, err)
	}
	return created.PK, nil
}

// AddUserToGroup adds a user to a group. Adding an existing member is a no-op.
func (c *AuthentikClient) AddUserToGroup(groupPK string, userPK int) error {
	_, err := c.doInto("POST", "/api/v3/core/groups/"+groupPK+"/add_user/", map[string]any{"pk": userPK}, nil)
	return err
}

// DeleteUserByUsername deletes the user with this username, with its tokens.
// It also deletes a core Actor, which is a user. No-op if not found.
func (c *AuthentikClient) DeleteUserByUsername(username string) error {
	existing, err := c.findUser(username)
	if err != nil || existing == nil {
		return err
	}
	return c.akDelete(fmt.Sprintf("/api/v3/core/users/%d/", existing.PK))
}

// ─── OAuth2 providers and applications ────────────────────────────────────────

// AuthentikOAuth2ProviderSpec is the desired state of a confidential OAuth2
// provider. Every provider HAL creates uses sub_mode=user_username, so that sub,
// and act.sub in an OBO token, is a username. It also includes its claims in the
// ID token, and has a per-provider issuer (see AuthentikOIDCIssuer).
type AuthentikOAuth2ProviderSpec struct {
	Name                string
	AuthorizationFlowPK string
	// InvalidationFlowPK is the flow the end-session endpoint runs. Only a flow
	// with a user logout stage ends the Authentik session (see
	// GetFlowPKBySlug and AuthentikLogoutFlowSlug).
	InvalidationFlowPK string
	SigningKeyPK       string
	GrantTypes         []string
	// RedirectURIs are matched strictly. Machine clients have none.
	RedirectURIs []string
	// PostLogoutRedirectURIs are the allowed post_logout_redirect_uri values of
	// the end-session endpoint, matched strictly.
	PostLogoutRedirectURIs []string
	// AccessTokenValidity, e.g. "hours=1", is the lifetime of the access tokens
	// this provider issues. Empty leaves Authentik's value unchanged.
	AccessTokenValidity string
	PropertyMappingPKs  []string
	// FederatedProviderPKs are the providers whose JWTs this provider accepts as
	// subject_token and actor_token in a token exchange.
	FederatedProviderPKs []int
}

// AuthentikOAuth2Client is an OAuth2 provider as its client sees it.
type AuthentikOAuth2Client struct {
	ProviderPK   int
	ClientID     string
	ClientSecret string
}

func (c *AuthentikClient) findOAuth2Provider(name string) (*akOAuth2Provider, error) {
	providers, err := akFind[akOAuth2Provider](c, "/api/v3/providers/oauth2/", url.Values{"name": {name}})
	if err != nil {
		return nil, err
	}
	for i := range providers {
		if providers[i].Name == name {
			return &providers[i], nil
		}
	}
	return nil, nil
}

// EnsureOAuth2Provider creates the provider named spec.Name, or brings an
// existing one to spec. An existing provider keeps its client ID and secret.
func (c *AuthentikClient) EnsureOAuth2Provider(spec AuthentikOAuth2ProviderSpec) (AuthentikOAuth2Client, error) {
	redirectURIs := make([]AuthentikRedirectURI, 0, len(spec.RedirectURIs)+len(spec.PostLogoutRedirectURIs))
	for _, u := range spec.RedirectURIs {
		redirectURIs = append(redirectURIs, AuthentikRedirectURI{MatchingMode: "strict", URL: u, RedirectURIType: "authorization"})
	}
	for _, u := range spec.PostLogoutRedirectURIs {
		redirectURIs = append(redirectURIs, AuthentikRedirectURI{MatchingMode: "strict", URL: u, RedirectURIType: "logout"})
	}
	body := map[string]any{
		"name":                       spec.Name,
		"authorization_flow":         spec.AuthorizationFlowPK,
		"invalidation_flow":          spec.InvalidationFlowPK,
		"signing_key":                spec.SigningKeyPK,
		"client_type":                "confidential",
		"grant_types":                orEmpty(spec.GrantTypes),
		"sub_mode":                   "user_username",
		"issuer_mode":                "per_provider",
		"include_claims_in_id_token": true,
		"property_mappings":          orEmpty(spec.PropertyMappingPKs),
		"redirect_uris":              redirectURIs,
		"jwt_federation_providers":   orEmpty(spec.FederatedProviderPKs),
	}
	if spec.AccessTokenValidity != "" {
		body["access_token_validity"] = spec.AccessTokenValidity
	}

	existing, err := c.findOAuth2Provider(spec.Name)
	if err != nil {
		return AuthentikOAuth2Client{}, fmt.Errorf("look up OAuth2 provider %q: %w", spec.Name, err)
	}
	var p akOAuth2Provider
	if existing != nil {
		// client_id and client_secret are left out, so they survive an update.
		_, err = c.doInto("PATCH", fmt.Sprintf("/api/v3/providers/oauth2/%d/", existing.PK), body, &p)
	} else {
		_, err = c.doInto("POST", "/api/v3/providers/oauth2/", body, &p)
	}
	if err != nil {
		return AuthentikOAuth2Client{}, fmt.Errorf("ensure OAuth2 provider %q: %w", spec.Name, err)
	}
	return AuthentikOAuth2Client{ProviderPK: p.PK, ClientID: p.ClientID, ClientSecret: p.ClientSecret}, nil
}

// EnsureApplication points the application with this slug at providerPK, creating
// it if absent, and returns its pbm_uuid, the target of its policy bindings. Its
// policy_engine_mode is "any": a user passes if one of its bindings passes.
// launchURL may be empty.
func (c *AuthentikClient) EnsureApplication(name, slug string, providerPK int, launchURL string) (string, error) {
	body := map[string]any{
		"name":               name,
		"slug":               slug,
		"provider":           providerPK,
		"policy_engine_mode": "any",
	}
	if launchURL != "" {
		body["meta_launch_url"] = launchURL
	}

	var app akApplication
	status, err := c.doInto("GET", "/api/v3/core/applications/"+slug+"/", nil, &app)
	switch {
	case status == http.StatusNotFound:
		_, err = c.doInto("POST", "/api/v3/core/applications/", body, &app)
	case err == nil:
		_, err = c.doInto("PATCH", "/api/v3/core/applications/"+slug+"/", body, &app)
	}
	if err != nil {
		return "", fmt.Errorf("ensure application %q: %w", slug, err)
	}
	if app.PBMUUID == "" {
		return "", fmt.Errorf("ensure application %q: no pbm_uuid in response", slug)
	}
	return app.PBMUUID, nil
}

// applicationPBMUUID returns the pbm_uuid of the application with this slug, or
// "" if it does not exist.
func (c *AuthentikClient) applicationPBMUUID(slug string) (string, error) {
	var app akApplication
	status, err := c.doInto("GET", "/api/v3/core/applications/"+slug+"/", nil, &app)
	if status == http.StatusNotFound {
		return "", nil
	}
	return app.PBMUUID, err
}

// ─── Scope mappings ───────────────────────────────────────────────────────────

// GetManagedScopeMappingPKs returns the pks of Authentik's built-in scope mappings
// for these scopes, e.g. "openid" for goauthentik.io/providers/oauth2/scope-openid.
// Matching on the managed key, not the scope name, ignores custom mappings that
// reuse a standard scope name. A missing built-in mapping is an error.
func (c *AuthentikClient) GetManagedScopeMappingPKs(scopes []string) ([]string, error) {
	query := url.Values{}
	for _, s := range scopes {
		query.Add("managed", "goauthentik.io/providers/oauth2/scope-"+s)
	}
	type managedMapping struct {
		PK      string `json:"pk"`
		Managed string `json:"managed"`
	}
	mappings, err := akFind[managedMapping](c, "/api/v3/propertymappings/provider/scope/", query)
	if err != nil {
		return nil, err
	}
	byManaged := make(map[string]string, len(mappings))
	for _, m := range mappings {
		byManaged[m.Managed] = m.PK
	}
	pks := make([]string, 0, len(scopes))
	for _, s := range scopes {
		pk, ok := byManaged["goauthentik.io/providers/oauth2/scope-"+s]
		if !ok {
			return nil, fmt.Errorf("built-in scope mapping %q not found", s)
		}
		pks = append(pks, pk)
	}
	return pks, nil
}

func (c *AuthentikClient) findScopeMapping(name string) (*akScopeMapping, error) {
	mappings, err := akFind[akScopeMapping](c, "/api/v3/propertymappings/provider/scope/", url.Values{"name": {name}})
	if err != nil {
		return nil, err
	}
	for i := range mappings {
		if mappings[i].Name == name {
			return &mappings[i], nil
		}
	}
	return nil, nil
}

// EnsureScopeMapping returns the pk of the scope mapping with this name, creating
// it or updating its scope and expression. The expression is Python run by
// Authentik when the scope is granted; the dict it returns is merged into the
// token's claims.
func (c *AuthentikClient) EnsureScopeMapping(name, scopeName, description, expression string) (string, error) {
	body := map[string]any{
		"name":        name,
		"scope_name":  scopeName,
		"description": description,
		"expression":  expression,
	}
	existing, err := c.findScopeMapping(name)
	if err != nil {
		return "", fmt.Errorf("look up scope mapping %q: %w", name, err)
	}
	var m akScopeMapping
	if existing != nil {
		_, err = c.doInto("PATCH", "/api/v3/propertymappings/provider/scope/"+existing.PK+"/", body, &m)
	} else {
		_, err = c.doInto("POST", "/api/v3/propertymappings/provider/scope/", body, &m)
	}
	if err != nil {
		return "", fmt.Errorf("ensure scope mapping %q: %w", name, err)
	}
	return m.PK, nil
}

// DeleteScopeMappingByName deletes the scope mapping with this name. No-op if not found.
func (c *AuthentikClient) DeleteScopeMappingByName(name string) error {
	existing, err := c.findScopeMapping(name)
	if err != nil || existing == nil {
		return err
	}
	return c.akDelete("/api/v3/propertymappings/provider/scope/" + existing.PK + "/")
}

// ─── Policy bindings ──────────────────────────────────────────────────────────

// SetApplicationGroupBindings makes groupPKs the only policy bindings of the
// application whose pbm_uuid is targetPBM. With the application's
// policy_engine_mode=any, only members of one of these groups pass, which is how
// Authentik refuses a token exchange for a persona outside them. Any other
// binding on the target is deleted, so the target must belong to the caller.
// A nil groupPKs deletes every binding.
func (c *AuthentikClient) SetApplicationGroupBindings(targetPBM string, groupPKs []string) error {
	bindings, err := akFind[akPolicyBinding](c, "/api/v3/policies/bindings/", url.Values{
		"target":    {targetPBM},
		"page_size": {"100"},
	})
	if err != nil {
		return fmt.Errorf("list policy bindings: %w", err)
	}

	wanted := make(map[string]bool, len(groupPKs))
	for _, pk := range groupPKs {
		wanted[pk] = true
	}
	bound := make(map[string]bool, len(groupPKs))
	for _, b := range bindings {
		// A disabled or negated binding of a wanted group would not grant access,
		// so it is replaced rather than kept.
		if b.Group != nil && b.Policy == nil && b.User == nil && b.Enabled && !b.Negate &&
			wanted[*b.Group] && !bound[*b.Group] {
			bound[*b.Group] = true
			continue
		}
		if err := c.akDelete("/api/v3/policies/bindings/" + b.PK + "/"); err != nil {
			return fmt.Errorf("delete policy binding %s: %w", b.PK, err)
		}
	}

	for order, groupPK := range groupPKs {
		if bound[groupPK] {
			continue
		}
		if _, err := c.doInto("POST", "/api/v3/policies/bindings/", map[string]any{
			"target":  targetPBM,
			"group":   groupPK,
			"order":   order,
			"enabled": true,
			"negate":  false,
		}, nil); err != nil {
			return fmt.Errorf("bind group %s: %w", groupPK, err)
		}
		bound[groupPK] = true
	}
	return nil
}

// ─── App passwords ────────────────────────────────────────────────────────────

// EnsureAppPassword returns the key of the non-expiring app-password token with
// this identifier, owned by userPK. An existing token is kept, so its key
// survives an update. A token that has drifted (another user, intent or expiry)
// is recreated: Authentik cannot change a token's user, and a PATCH without
// "user" would hand the token to the caller.
func (c *AuthentikClient) EnsureAppPassword(identifier string, userPK int, description string) (string, error) {
	tokens, err := akFind[akToken](c, "/api/v3/core/tokens/", url.Values{"identifier": {identifier}})
	if err != nil {
		return "", fmt.Errorf("look up token %q: %w", identifier, err)
	}
	keep := false
	for _, t := range tokens {
		if t.Identifier != identifier {
			continue
		}
		if t.User == userPK && t.Intent == "app_password" && !t.Expiring {
			keep = true
			break
		}
		if err := c.DeleteTokenByIdentifier(identifier); err != nil {
			return "", fmt.Errorf("delete drifted token %q: %w", identifier, err)
		}
	}
	if !keep {
		if _, err := c.doInto("POST", "/api/v3/core/tokens/", map[string]any{
			"identifier":  identifier,
			"intent":      "app_password",
			"user":        userPK,
			"description": description,
			"expiring":    false,
		}, nil); err != nil {
			return "", fmt.Errorf("create token %q: %w", identifier, err)
		}
	}

	var view struct {
		Key string `json:"key"`
	}
	if _, err := c.doInto("GET", "/api/v3/core/tokens/"+identifier+"/view_key/", nil, &view); err != nil {
		return "", fmt.Errorf("read key of token %q: %w", identifier, err)
	}
	if view.Key == "" {
		return "", fmt.Errorf("read key of token %q: empty key", identifier)
	}
	return view.Key, nil
}

// DeleteTokenByIdentifier deletes the token with this identifier. No-op if not found.
func (c *AuthentikClient) DeleteTokenByIdentifier(identifier string) error {
	return c.akDelete("/api/v3/core/tokens/" + identifier + "/")
}

// ─── Core Actors ──────────────────────────────────────────────────────────────

// authentikShell runs a Python script with `ak shell` inside hal-authentik-server
// and returns its standard output. It is a variable so that tests can fake it.
var authentikShell = func(engine, script string) (string, error) {
	cmd := exec.Command(engine, "exec", "-i", AuthentikServerContainer, "ak", "shell")
	cmd.Stdin = strings.NewReader(script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("ak shell in %s: %w\n%s", AuthentikServerContainer, err, lastLines(stderr.String(), 10))
	}
	return stdout.String(), nil
}

// lastLines returns the last n lines of s, where a Python traceback ends.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// authentikActorScript creates the core Actor if absent and prints its user pk.
// %[1]s and %[2]s are the username and display name as JSON strings, which are
// valid Python string literals. An Actor is a User subclass, so a plain user
// already holding the username is reported rather than clashing on insert.
const authentikActorScript = `from authentik.core.models import Actor, User, UserTypes

username = %[1]s
name = %[2]s
actors = Actor.objects.including_expired()
if User.objects.filter(username=username).exists() and not actors.filter(username=username).exists():
    print("HAL_ACTOR_ERROR=a user that is not an actor already has this username")
else:
    actor, created = actors.get_or_create(
        username=username,
        defaults={
            "name": name,
            "parent": None,
            "policy_behavior": "none",
            "type": UserTypes.SERVICE_ACCOUNT,
            "expiring": False,
        },
    )
    if created:
        actor.set_unusable_password()
    if created or actor.expiring:
        actor.expiring = False
        actor.save()
    print(f"HAL_ACTOR_PK={actor.pk}")
`

var authentikActorUsernameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// EnsureAuthentikActor returns the user pk of the core Actor with this username,
// creating it if absent. The Actor has no parent, policy_behavior=none, type
// service_account, no usable password, and does not expire. With no parent, it
// may act for any persona, and must present a JWT as actor_token.
//
// Core Actors have no REST API in Authentik 2026.8 (the REST "agents" API is
// the Enterprise Agent, an Actor bound to one parent user), so this one goes
// through `ak shell`. The returned pk works with the users and tokens APIs,
// which is how the Actor gets its app password.
func EnsureAuthentikActor(engine, username, displayName string) (int, error) {
	if !authentikActorUsernameRE.MatchString(username) {
		return 0, fmt.Errorf("invalid actor username %q", username)
	}
	u, _ := json.Marshal(username)
	n, _ := json.Marshal(displayName)
	out, err := authentikShell(engine, fmt.Sprintf(authentikActorScript, u, n))
	if err != nil {
		return 0, fmt.Errorf("ensure actor %q: %w", username, err)
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if msg, ok := strings.CutPrefix(line, "HAL_ACTOR_ERROR="); ok {
			return 0, fmt.Errorf("ensure actor %q: %s", username, msg)
		}
		if v, ok := strings.CutPrefix(line, "HAL_ACTOR_PK="); ok {
			pk, err := strconv.Atoi(v)
			if err != nil {
				return 0, fmt.Errorf("ensure actor %q: unexpected pk %q", username, v)
			}
			return pk, nil
		}
	}
	return 0, fmt.Errorf("ensure actor %q: ak shell printed no actor pk:\n%s", username, lastLines(out, 10))
}
