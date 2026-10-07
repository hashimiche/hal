package integrations

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ─── Agentic IAM lab: Authentik side (ADR 0004) ───────────────────────────────
//
// Authentik issues the OBO token by token exchange on vault-agentic:
//   - a persona logs in on hal-chat, and its access token is the subject_token;
//   - the demo agent logs in on hal-demo-agent as the core Actor finance-agent,
//     with its app password, and that access token is the actor_token;
//   - vault-agentic federates both providers and issues the OBO token, with
//     sub = the persona, act.sub = finance-agent, aud = its own client ID, and one
//     authorization_details entry per task-scope scope;
//   - a policy binding on vault-agentic only lets finance and engineering
//     delegate, so the exchange is refused for charlie (sales) with invalid_grant.
//
// Ownership: every object below is found again by a name reserved for this lab,
// so RemoveAgenticIAMAuthentik never touches what hal vault oidc or hal tf saml
// own. alice and bob are the exception: they are shared personas (see
// AgenticIAMRemoveOptions).

const (
	// AgenticIAMAuthentikConsumer is the lab's consumer name for the shared
	// Authentik service (AuthentikSharedServiceKey).
	AgenticIAMAuthentikConsumer = "vault-agentic-iam"

	// Application slugs. A slug also names the provider and the issuer path.
	AgenticIAMChatSlug      = "hal-chat"
	AgenticIAMDemoAgentSlug = "hal-demo-agent"
	AgenticIAMVaultSlug     = "vault-agentic"

	// AgenticIAMActorUsername is the core Actor the demo agent logs in as, and
	// the OBO token's act.sub.
	AgenticIAMActorUsername = "finance-agent"

	// Task-scope scopes. Each one adds one authorization_details entry.
	AgenticIAMScopeFinanceReports = "vault:finance-reports"
	AgenticIAMScopeForecasts      = "vault:forecasts"
	AgenticIAMScopePayroll        = "vault:payroll"

	agenticIAMActorName          = "Finance demo agent"
	agenticIAMActorTokenID       = "hal-agentic-iam-finance-agent"
	agenticIAMScopeMappingPrefix = "hal: Agentic IAM "

	// Access token lifetimes. A persona stays logged in to the chat for a whole
	// demo, and an OBO token, like the demo agent's own token, outlives one task.
	agenticIAMChatTokenValidity      = "hours=1"
	agenticIAMDemoAgentTokenValidity = "minutes=10"
	agenticIAMOBOTokenValidity       = "minutes=10"
)

// AgenticIAMPersona is a persona of the Agentic IAM lab and the Authentik group
// of its team.
type AgenticIAMPersona struct {
	Username string
	Name     string
	Group    string
	// Delegates is true when the persona may delegate to the demo agent: its
	// group is bound on vault-agentic.
	Delegates bool
	// Shared is true when other Authentik labs create the same user.
	Shared bool
}

// AgenticIAMPersonas are the lab's personas. alice and bob are shared with
// hal vault oidc and hal tf saml; charlie belongs to this lab only.
var AgenticIAMPersonas = []AgenticIAMPersona{
	{Username: "alice", Name: "Alice", Group: "finance", Delegates: true, Shared: true},
	{Username: "bob", Name: "Bob", Group: "engineering", Delegates: true, Shared: true},
	{Username: "charlie", Name: "Charlie", Group: "sales"},
}

// AgenticIAMAuthentikConfig is what the lab's Authentik configuration depends on.
type AgenticIAMAuthentikConfig struct {
	// ChatPublicURL is the chat's PUBLIC_URL as the browser reaches it, e.g.
	// "http://localhost:8095". hal-chat accepts <ChatPublicURL>/callback as its
	// redirect URI and <ChatPublicURL>/ as its post-logout redirect URI, both
	// matched strictly, and the Authentik portal tile launches <ChatPublicURL>/.
	ChatPublicURL string
	// DBMount is the lab's Vault database mount, e.g. "agentic-db".
	DBMount string
	// The Vault roles each task-scope scope grants a read on, under
	// <DBMount>/creds/.
	FinanceReportsRole string
	ForecastsRole      string
	PayrollRole        string
}

// AgenticIAMAuthentikApp is one of the lab's OAuth2 applications.
type AgenticIAMAuthentikApp struct {
	Slug string
	AuthentikOAuth2Client
}

// AgenticIAMAuthentik is what the rest of the lab needs from Authentik.
type AgenticIAMAuthentik struct {
	// Chat is hal-chat: personas log in here (authorization code with PKCE).
	Chat AgenticIAMAuthentikApp
	// DemoAgent is hal-demo-agent: the demo agent logs in here with
	// client_credentials, username=ActorUsername and password=ActorAppPassword.
	DemoAgent AgenticIAMAuthentikApp
	// VaultAgentic is vault-agentic: the token exchange that issues the OBO token
	// is authenticated with its client. Its client ID is the token's aud.
	VaultAgentic AgenticIAMAuthentikApp

	ActorUsername    string
	ActorAppPassword string

	// Endpoints and issuers use authentik.localhost:9100, from the host and from
	// hal-net alike: Authentik derives its issuer from the request Host.
	TokenEndpoint     string
	AuthorizeEndpoint string
	ChatIssuer        string
	// ChatEndSessionEndpoint ends the persona's Authentik session, then
	// redirects to <ChatPublicURL>/. The chat must send id_token_hint along with
	// post_logout_redirect_uri.
	ChatEndSessionEndpoint string
	VaultAgenticIssuer     string
	VaultAgenticJWKSURI    string

	// ScopePaths maps each task-scope scope to the Vault path its
	// authorization_details entry names.
	ScopePaths map[string]string
}

// AgenticIAMRemoveOptions says what RemoveAgenticIAMAuthentik may delete beyond
// the lab's own objects.
type AgenticIAMRemoveOptions struct {
	// DeleteSharedPersonas also deletes the alice and bob users. Set it only when
	// no other Authentik consumer remains: see AgenticIAMSharedPersonasInUse.
	DeleteSharedPersonas bool
}

// AgenticIAMSharedPersonasInUse reports whether another Authentik consumer may
// still log in as alice or bob. consumers is the shared-service consumer list of
// AuthentikSharedServiceKey, typically what RemoveSharedServiceConsumer returns
// once the lab has deregistered; the lab's own name is ignored either way.
//
// Every other consumer today (vault-oidc, tfe-saml, tfe-bis-saml) creates alice
// and bob, and a future one may too, so any remaining consumer counts.
func AgenticIAMSharedPersonasInUse(consumers []string) bool {
	for _, c := range consumers {
		if c != AgenticIAMAuthentikConsumer {
			return true
		}
	}
	return false
}

// agenticIAMDelegatingGroups returns the groups bound on vault-agentic.
func agenticIAMDelegatingGroups() []string {
	var groups []string
	for _, p := range AgenticIAMPersonas {
		if p.Delegates {
			groups = append(groups, p.Group)
		}
	}
	return groups
}

// agenticIAMScopes returns the task-scope scopes in a fixed order, each with the
// Vault path it grants a read on.
func agenticIAMScopes(cfg AgenticIAMAuthentikConfig) []struct{ Scope, Path string } {
	mount := strings.Trim(cfg.DBMount, "/")
	return []struct{ Scope, Path string }{
		{AgenticIAMScopeFinanceReports, mount + "/creds/" + cfg.FinanceReportsRole},
		{AgenticIAMScopeForecasts, mount + "/creds/" + cfg.ForecastsRole},
		{AgenticIAMScopePayroll, mount + "/creds/" + cfg.PayrollRole},
	}
}

// agenticIAMScopeMappingName returns the lab's name for a scope's mapping.
func agenticIAMScopeMappingName(scope string) string {
	return agenticIAMScopeMappingPrefix + scope
}

// rarScopeExpression returns the scope mapping expression that emits one RAR
// (RFC 9396) entry for a Vault path:
//
//	return {"authorization_details":[{"type":"vault:path_access","path":"<path>","capabilities":["read"]}]}
//
// Authentik deep-merges the results of all granted scopes, so several scopes
// yield one concatenated authorization_details list. The field names are those of
// Vault's RAR type specification. The dict is written as JSON, which is a valid
// Python literal here (strings and lists only), so the path needs no escaping.
func rarScopeExpression(path string) string {
	type pathAccess struct {
		Type         string   `json:"type"`
		Path         string   `json:"path"`
		Capabilities []string `json:"capabilities"`
	}
	claim := map[string][]pathAccess{
		"authorization_details": {{Type: "vault:path_access", Path: path, Capabilities: []string{"read"}}},
	}
	raw, _ := json.Marshal(claim)
	return "return " + string(raw)
}

// chatURL returns <ChatPublicURL><path>, with exactly one slash between them.
func (cfg AgenticIAMAuthentikConfig) chatURL(path string) string {
	return strings.TrimRight(cfg.ChatPublicURL, "/") + path
}

func (cfg AgenticIAMAuthentikConfig) validate() error {
	u, err := url.Parse(cfg.ChatPublicURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("agentic IAM: chat public URL %q is not an http(s) URL", cfg.ChatPublicURL)
	}
	if strings.Trim(cfg.DBMount, "/") == "" {
		return fmt.Errorf("agentic IAM: no Vault database mount")
	}
	if cfg.FinanceReportsRole == "" || cfg.ForecastsRole == "" || cfg.PayrollRole == "" {
		return fmt.Errorf("agentic IAM: a Vault role is missing")
	}
	return nil
}

// EnsureAgenticIAMAuthentik configures the Agentic IAM lab in the shared
// Authentik, and returns what the chat, the demo agent and Vault need. It is
// idempotent, so `hal vault agentic-iam update` re-runs it: existing objects are
// updated in place, and client secrets and the actor's app password are kept.
// engine is the container engine running hal-authentik-server.
func EnsureAgenticIAMAuthentik(engine string, c *AuthentikClient, cfg AgenticIAMAuthentikConfig) (*AgenticIAMAuthentik, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	// Provider prerequisites, the same as hal vault oidc.
	flowPK, err := c.GetDefaultAuthorizationFlowPK()
	if err != nil {
		return nil, fmt.Errorf("get authorization flow: %w", err)
	}
	invalidationFlowPK, err := c.GetDefaultInvalidationFlowPK()
	if err != nil {
		return nil, fmt.Errorf("get invalidation flow: %w", err)
	}
	// hal-chat's logout must end the Authentik session, or the next login
	// silently signs the same persona back in.
	logoutFlowPK, err := c.GetFlowPKBySlug(AuthentikLogoutFlowSlug)
	if err != nil {
		return nil, fmt.Errorf("get logout flow: %w", err)
	}
	keyPK, err := c.GetFirstSigningKeyPK()
	if err != nil {
		return nil, fmt.Errorf("get signing key: %w", err)
	}
	standardPKs, err := c.GetManagedScopeMappingPKs([]string{"openid", "profile", "email"})
	if err != nil {
		return nil, fmt.Errorf("get standard scope mappings: %w", err)
	}

	// Groups and personas.
	groupPKs := map[string]string{}
	for _, p := range AgenticIAMPersonas {
		if _, ok := groupPKs[p.Group]; ok {
			continue
		}
		pk, err := c.EnsureGroup(p.Group)
		if err != nil {
			return nil, err
		}
		groupPKs[p.Group] = pk
	}
	for _, p := range AgenticIAMPersonas {
		email := p.Username + "@hal.local"
		if _, err := c.EnsureUser(p.Username, p.Name, email, AuthentikPersonaPassword, []string{groupPKs[p.Group]}); err != nil {
			return nil, err
		}
	}

	// Task-scope scope mappings.
	scopePaths := map[string]string{}
	rarPKs := []string{}
	for _, s := range agenticIAMScopes(cfg) {
		pk, err := c.EnsureScopeMapping(
			agenticIAMScopeMappingName(s.Scope), s.Scope,
			fmt.Sprintf("Agentic IAM task scope: read %s. Created by hal.", s.Path),
			rarScopeExpression(s.Path),
		)
		if err != nil {
			return nil, err
		}
		rarPKs = append(rarPKs, pk)
		scopePaths[s.Scope] = s.Path
	}

	ensureApp := func(slug, name, launchURL string, spec AuthentikOAuth2ProviderSpec) (AgenticIAMAuthentikApp, string, error) {
		spec.Name = slug
		spec.AuthorizationFlowPK = flowPK
		if spec.InvalidationFlowPK == "" {
			spec.InvalidationFlowPK = invalidationFlowPK
		}
		spec.SigningKeyPK = keyPK
		client, err := c.EnsureOAuth2Provider(spec)
		if err != nil {
			return AgenticIAMAuthentikApp{}, "", err
		}
		pbm, err := c.EnsureApplication(name, slug, client.ProviderPK, launchURL)
		if err != nil {
			return AgenticIAMAuthentikApp{}, "", err
		}
		return AgenticIAMAuthentikApp{Slug: slug, AuthentikOAuth2Client: client}, pbm, nil
	}

	// hal-chat: personas log in with authorization code. PKCE (S256), state and
	// nonce need no setting, and the token endpoint takes client_secret_basic as
	// well as client_secret_post.
	chat, _, err := ensureApp(AgenticIAMChatSlug, "HAL chat (Agentic IAM)", cfg.chatURL("/"), AuthentikOAuth2ProviderSpec{
		InvalidationFlowPK:     logoutFlowPK,
		GrantTypes:             AuthentikOAuth2AuthorizationGrants,
		RedirectURIs:           []string{cfg.chatURL("/callback")},
		PostLogoutRedirectURIs: []string{cfg.chatURL("/")},
		AccessTokenValidity:    agenticIAMChatTokenValidity,
		PropertyMappingPKs:     standardPKs,
	})
	if err != nil {
		return nil, err
	}

	// hal-demo-agent: the demo agent's own login, client_credentials only.
	demoAgent, _, err := ensureApp(AgenticIAMDemoAgentSlug, "HAL demo agent (Agentic IAM)", "", AuthentikOAuth2ProviderSpec{
		GrantTypes:          []string{"client_credentials"},
		AccessTokenValidity: agenticIAMDemoAgentTokenValidity,
		PropertyMappingPKs:  standardPKs,
	})
	if err != nil {
		return nil, err
	}

	// vault-agentic: token exchange only, trusting the JWTs of both logins.
	vaultAgentic, vaultPBM, err := ensureApp(AgenticIAMVaultSlug, "Vault (Agentic IAM)", "", AuthentikOAuth2ProviderSpec{
		GrantTypes:           []string{AuthentikGrantTokenExchange},
		AccessTokenValidity:  agenticIAMOBOTokenValidity,
		PropertyMappingPKs:   append(append([]string{}, standardPKs...), rarPKs...),
		FederatedProviderPKs: []int{chat.ProviderPK, demoAgent.ProviderPK},
	})
	if err != nil {
		return nil, err
	}

	// The IdP decision point: only these groups may delegate to the demo agent.
	var delegating []string
	for _, g := range agenticIAMDelegatingGroups() {
		delegating = append(delegating, groupPKs[g])
	}
	if err := c.SetApplicationGroupBindings(vaultPBM, delegating); err != nil {
		return nil, fmt.Errorf("bind delegating groups on %s: %w", AgenticIAMVaultSlug, err)
	}

	// The demo agent's identity and its password on hal-demo-agent.
	actorPK, err := EnsureAuthentikActor(engine, AgenticIAMActorUsername, agenticIAMActorName)
	if err != nil {
		return nil, err
	}
	actorPassword, err := c.EnsureAppPassword(agenticIAMActorTokenID, actorPK,
		"Agentic IAM: the demo agent's password on hal-demo-agent. Created by hal.")
	if err != nil {
		return nil, err
	}

	return &AgenticIAMAuthentik{
		Chat:                   chat,
		DemoAgent:              demoAgent,
		VaultAgentic:           vaultAgentic,
		ActorUsername:          AgenticIAMActorUsername,
		ActorAppPassword:       actorPassword,
		TokenEndpoint:          AuthentikOAuth2TokenEndpoint(),
		AuthorizeEndpoint:      AuthentikOAuth2AuthorizeEndpoint(),
		ChatIssuer:             AuthentikOIDCIssuer(AgenticIAMChatSlug),
		ChatEndSessionEndpoint: AuthentikOIDCEndSessionEndpoint(AgenticIAMChatSlug),
		VaultAgenticIssuer:     AuthentikOIDCIssuer(AgenticIAMVaultSlug),
		VaultAgenticJWKSURI:    AuthentikOIDCJWKSURI(AgenticIAMVaultSlug),
		ScopePaths:             scopePaths,
	}, nil
}

// RemoveAgenticIAMAuthentik deletes the lab's objects from the shared Authentik:
// its applications with their bindings, its providers and scope mappings, the
// actor with its app password, charlie, and the lab's groups, which takes the
// memberships it gave alice and bob with them. alice and bob themselves are only
// deleted with opts.DeleteSharedPersonas. Missing objects are skipped, and every
// step is attempted, so a partial lab is cleaned up too.
func RemoveAgenticIAMAuthentik(c *AuthentikClient, opts AgenticIAMRemoveOptions) error {
	var errs []error
	try := func(what string, err error) {
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", what, err))
		}
	}

	// Bindings would go with the application; deleting them first keeps the
	// IdP decision point closed if the application delete fails.
	pbm, err := c.applicationPBMUUID(AgenticIAMVaultSlug)
	try("look up application "+AgenticIAMVaultSlug, err)
	if pbm != "" {
		try("delete bindings of "+AgenticIAMVaultSlug, c.SetApplicationGroupBindings(pbm, nil))
	}
	// vault-agentic first: it federates the two others.
	for _, slug := range []string{AgenticIAMVaultSlug, AgenticIAMDemoAgentSlug, AgenticIAMChatSlug} {
		try("delete application "+slug, c.DeleteApplicationBySlug(slug))
		try("delete OAuth2 provider "+slug, c.DeleteOAuth2ProviderByName(slug))
	}
	for _, scope := range []string{AgenticIAMScopeFinanceReports, AgenticIAMScopeForecasts, AgenticIAMScopePayroll} {
		try("delete scope mapping "+scope, c.DeleteScopeMappingByName(agenticIAMScopeMappingName(scope)))
	}

	// The token would go with the actor; it is deleted on its own in case the
	// actor was already removed by hand.
	try("delete token "+agenticIAMActorTokenID, c.DeleteTokenByIdentifier(agenticIAMActorTokenID))
	try("delete actor "+AgenticIAMActorUsername, c.DeleteUserByUsername(AgenticIAMActorUsername))

	groups := map[string]bool{}
	for _, p := range AgenticIAMPersonas {
		if !p.Shared || opts.DeleteSharedPersonas {
			try("delete persona "+p.Username, c.DeleteUserByUsername(p.Username))
		}
		if !groups[p.Group] {
			groups[p.Group] = true
			try("delete group "+p.Group, c.DeleteGroupByName(p.Group))
		}
	}
	return errors.Join(errs...)
}
