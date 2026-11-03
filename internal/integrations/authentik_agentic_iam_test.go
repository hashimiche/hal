package integrations

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

var testAgenticIAMConfig = AgenticIAMAuthentikConfig{
	ChatPublicURL:      "http://localhost:8095",
	DBMount:            "agentic-db",
	FinanceReportsRole: "quarterly-results",
	ForecastsRole:      "forecasts",
	PayrollRole:        "payroll",
}

// seedVaultOIDC puts what `hal vault oidc enable` leaves in Authentik: alice and
// bob with their own groups and passwords, and the OIDC application.
func seedVaultOIDC(f *fakeAuthentik) {
	f.groups = append(f.groups,
		map[string]any{"pk": "group-admin", "name": "admin"},
		map[string]any{"pk": "group-user-ro", "name": "user-ro"},
	)
	f.users = append(f.users,
		map[string]any{"pk": 101, "username": "alice", "name": "Alice Admin", "type": "internal", "groups": []string{"group-admin"}, "password": "oidc-password"},
		map[string]any{"pk": 102, "username": "bob", "name": "Bob Builder", "type": "internal", "groups": []string{"group-user-ro"}, "password": "oidc-password"},
	)
	f.mappings = append(f.mappings, map[string]any{
		"pk": "mapping-oidc-groups", "name": "hal: OIDC groups scope", "scope_name": "groups",
	})
	f.providers = append(f.providers, map[string]any{
		"pk": 900, "name": "vault-oidc-provider", "client_id": "oidc-client", "client_secret": "oidc-secret",
	})
	f.apps = append(f.apps, map[string]any{"slug": "hashicorp-vault", "name": "HashiCorp Vault", "provider": 900, "pbm_uuid": "pbm-oidc"})
}

func groupPK(t *testing.T, f *fakeAuthentik, name string) string {
	t.Helper()
	g := find(f.groups, "name", name)
	if g == nil {
		t.Fatalf("group %q not found", name)
	}
	return fmt.Sprint(g["pk"])
}

func TestEnsureAgenticIAMAuthentikConfiguresTheLab(t *testing.T) {
	f := newFakeAuthentik(t)
	seedVaultOIDC(f)

	got, err := EnsureAgenticIAMAuthentik("podman", f.client(), testAgenticIAMConfig)
	if err != nil {
		t.Fatalf("EnsureAgenticIAMAuthentik: %v", err)
	}

	// Personas: alice and bob are shared, so they keep their password and their
	// hal vault oidc group, and only gain the lab's group.
	for _, p := range []struct {
		username string
		groups   []string
		password string
	}{
		{"alice", []string{"admin", "finance"}, "oidc-password"},
		{"bob", []string{"user-ro", "engineering"}, "oidc-password"},
		{"charlie", []string{"sales"}, AuthentikPersonaPassword},
	} {
		u := find(f.users, "username", p.username)
		if u == nil {
			t.Fatalf("persona %s not found", p.username)
		}
		var want []string
		for _, g := range p.groups {
			want = append(want, groupPK(t, f, g))
		}
		if gotGroups := strs(u["groups"]); !reflect.DeepEqual(gotGroups, want) {
			t.Errorf("%s groups = %v, want %v", p.username, gotGroups, want)
		}
		if u["password"] != p.password {
			t.Errorf("%s password = %v, want %v", p.username, u["password"], p.password)
		}
	}
	if u := find(f.users, "username", "charlie"); u["type"] != "internal" || u["email"] != "charlie@hal.local" {
		t.Errorf("charlie = %v, want an internal user charlie@hal.local", u)
	}

	std := []string{"managed-openid", "managed-profile", "managed-email"}

	// Task-scope scope mappings.
	var rarPKs []string
	for _, s := range []struct{ scope, path string }{
		{"vault:finance-reports", "agentic-db/creds/quarterly-results"},
		{"vault:forecasts", "agentic-db/creds/forecasts"},
		{"vault:payroll", "agentic-db/creds/payroll"},
	} {
		m := find(f.mappings, "name", "hal: Agentic IAM "+s.scope)
		if m == nil {
			t.Fatalf("scope mapping for %s not found", s.scope)
		}
		if m["scope_name"] != s.scope {
			t.Errorf("%s scope_name = %v", s.scope, m["scope_name"])
		}
		want := `return {"authorization_details":[{"type":"vault:path_access","path":"` + s.path + `","capabilities":["read"]}]}`
		if m["expression"] != want {
			t.Errorf("%s expression = %v, want %s", s.scope, m["expression"], want)
		}
		if got.ScopePaths[s.scope] != s.path {
			t.Errorf("ScopePaths[%s] = %q, want %q", s.scope, got.ScopePaths[s.scope], s.path)
		}
		rarPKs = append(rarPKs, fmt.Sprint(m["pk"]))
	}

	// Providers.
	chat := find(f.providers, "name", "hal-chat")
	demo := find(f.providers, "name", "hal-demo-agent")
	vault := find(f.providers, "name", "vault-agentic")
	if chat == nil || demo == nil || vault == nil {
		t.Fatalf("providers = %v, want hal-chat, hal-demo-agent and vault-agentic", f.providers)
	}
	for _, p := range []map[string]any{chat, demo, vault} {
		for field, want := range map[string]any{
			"client_type":                "confidential",
			"sub_mode":                   "user_username",
			"issuer_mode":                "per_provider",
			"include_claims_in_id_token": true,
			"authorization_flow":         "flow-authz-implicit",
			"signing_key":                "key-self-signed",
		} {
			if p[field] != want {
				t.Errorf("%s %s = %v, want %v", p["name"], field, p[field], want)
			}
		}
	}
	// Switching persona: only hal-chat's logout must end the Authentik session.
	for _, tc := range []struct {
		p                  map[string]any
		invalidation, life string
	}{
		{chat, "flow-inval", "hours=1"},
		{demo, "flow-inval-provider", "minutes=10"},
		{vault, "flow-inval-provider", "minutes=10"},
	} {
		if tc.p["invalidation_flow"] != tc.invalidation || tc.p["access_token_validity"] != tc.life {
			t.Errorf("%s invalidation_flow, access_token_validity = %v, %v, want %s, %s", tc.p["name"],
				tc.p["invalidation_flow"], tc.p["access_token_validity"], tc.invalidation, tc.life)
		}
	}
	checkProvider := func(p map[string]any, grants []string, redirects []any, mappings []string, federated []int) {
		t.Helper()
		if g := strs(p["grant_types"]); !reflect.DeepEqual(g, grants) {
			t.Errorf("%s grant_types = %v, want %v", p["name"], g, grants)
		}
		if r, _ := p["redirect_uris"].([]any); !reflect.DeepEqual(r, redirects) {
			t.Errorf("%s redirect_uris = %v, want %v", p["name"], r, redirects)
		}
		if m := strs(p["property_mappings"]); !reflect.DeepEqual(m, mappings) {
			t.Errorf("%s property_mappings = %v, want %v", p["name"], m, mappings)
		}
		if fed := ints(p["jwt_federation_providers"]); !reflect.DeepEqual(fed, federated) {
			t.Errorf("%s jwt_federation_providers = %v, want %v", p["name"], fed, federated)
		}
	}
	checkProvider(chat, []string{"authorization_code", "refresh_token"}, []any{
		map[string]any{"matching_mode": "strict", "url": "http://localhost:8095/callback", "redirect_uri_type": "authorization"},
		map[string]any{"matching_mode": "strict", "url": "http://localhost:8095/", "redirect_uri_type": "logout"},
	}, std, []int{})
	checkProvider(demo, []string{"client_credentials"}, []any{}, std, []int{})
	checkProvider(vault, []string{"urn:ietf:params:oauth:grant-type:token-exchange"}, []any{},
		append(append([]string{}, std...), rarPKs...), []int{asInt(chat["pk"]), asInt(demo["pk"])})

	// Applications.
	for slug, p := range map[string]map[string]any{"hal-chat": chat, "hal-demo-agent": demo, "vault-agentic": vault} {
		a := find(f.apps, "slug", slug)
		if a == nil {
			t.Fatalf("application %s not found", slug)
		}
		if asInt(a["provider"]) != asInt(p["pk"]) || a["policy_engine_mode"] != "any" {
			t.Errorf("application %s = %v, want provider %v and policy_engine_mode any", slug, a, p["pk"])
		}
	}
	if a := find(f.apps, "slug", "hal-chat"); a["meta_launch_url"] != "http://localhost:8095/" {
		t.Errorf("hal-chat meta_launch_url = %v", a["meta_launch_url"])
	}

	// The IdP decision point: finance and engineering may delegate, sales may not.
	vaultApp := find(f.apps, "slug", "vault-agentic")
	var bound []string
	for _, b := range f.bindings {
		if b["target"] != vaultApp["pbm_uuid"] {
			t.Errorf("binding %v on another target than vault-agentic", b)
			continue
		}
		if b["enabled"] != true || b["negate"] != false {
			t.Errorf("binding %v is not enabled and positive", b)
		}
		bound = append(bound, fmt.Sprint(b["group"]))
	}
	if want := []string{groupPK(t, f, "finance"), groupPK(t, f, "engineering")}; !reflect.DeepEqual(bound, want) {
		t.Errorf("vault-agentic bound groups = %v, want %v (finance, engineering)", bound, want)
	}

	// The actor and its app password.
	if len(f.shellScripts) != 1 {
		t.Fatalf("ak shell ran %d times, want 1", len(f.shellScripts))
	}
	actor := find(f.users, "username", "finance-agent")
	if actor == nil {
		t.Fatal("actor finance-agent not found")
	}
	tok := find(f.tokens, "identifier", "hal-agentic-iam-finance-agent")
	if tok == nil {
		t.Fatal("actor token not found")
	}
	if tok["intent"] != "app_password" || tok["expiring"] != false || asInt(tok["user"]) != asInt(actor["pk"]) {
		t.Errorf("actor token = %v, want a non-expiring app_password of user %v", tok, actor["pk"])
	}

	want := AgenticIAMAuthentik{
		Chat: AgenticIAMAuthentikApp{Slug: "hal-chat", AuthentikOAuth2Client: AuthentikOAuth2Client{
			ProviderPK: asInt(chat["pk"]), ClientID: fmt.Sprint(chat["client_id"]), ClientSecret: fmt.Sprint(chat["client_secret"]),
		}},
		DemoAgent: AgenticIAMAuthentikApp{Slug: "hal-demo-agent", AuthentikOAuth2Client: AuthentikOAuth2Client{
			ProviderPK: asInt(demo["pk"]), ClientID: fmt.Sprint(demo["client_id"]), ClientSecret: fmt.Sprint(demo["client_secret"]),
		}},
		VaultAgentic: AgenticIAMAuthentikApp{Slug: "vault-agentic", AuthentikOAuth2Client: AuthentikOAuth2Client{
			ProviderPK: asInt(vault["pk"]), ClientID: fmt.Sprint(vault["client_id"]), ClientSecret: fmt.Sprint(vault["client_secret"]),
		}},
		ActorUsername:          "finance-agent",
		ActorAppPassword:       fmt.Sprint(tok["key"]),
		TokenEndpoint:          "http://authentik.localhost:9100/application/o/token/",
		AuthorizeEndpoint:      "http://authentik.localhost:9100/application/o/authorize/",
		ChatIssuer:             "http://authentik.localhost:9100/application/o/hal-chat/",
		ChatEndSessionEndpoint: "http://authentik.localhost:9100/application/o/hal-chat/end-session/",
		VaultAgenticIssuer:     "http://authentik.localhost:9100/application/o/vault-agentic/",
		VaultAgenticJWKSURI:    "http://authentik.localhost:9100/application/o/vault-agentic/jwks/",
		ScopePaths:             got.ScopePaths,
	}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("result =\n%+v\nwant\n%+v", *got, want)
	}

	// hal vault oidc's objects are untouched.
	if p := find(f.providers, "name", "vault-oidc-provider"); p["client_secret"] != "oidc-secret" || len(p) != 4 {
		t.Errorf("vault-oidc-provider was modified: %v", p)
	}
	if f.count("PATCH /api/v3/core/applications/hashicorp-vault/") != 0 {
		t.Error("the hashicorp-vault application was modified")
	}
}

func TestEnsureAgenticIAMAuthentikIsIdempotent(t *testing.T) {
	f := newFakeAuthentik(t)
	seedVaultOIDC(f)

	first, err := EnsureAgenticIAMAuthentik("podman", f.client(), testAgenticIAMConfig)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	counts := func() []int {
		return []int{len(f.groups), len(f.users), len(f.mappings), len(f.providers), len(f.apps), len(f.bindings), len(f.tokens)}
	}
	before := counts()
	f.calls = nil

	second, err := EnsureAgenticIAMAuthentik("podman", f.client(), testAgenticIAMConfig)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if after := counts(); !reflect.DeepEqual(after, before) {
		t.Errorf("object counts after a second run = %v, want %v", after, before)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("second run returned\n%+v\nwant the same as the first\n%+v", second, first)
	}
	// Existing objects are updated in place, never created again.
	for _, c := range f.calls {
		if strings.HasPrefix(c, "POST ") && !strings.HasSuffix(c, "/add_user/") {
			t.Errorf("second run created something: %s", c)
		}
		if strings.HasPrefix(c, "DELETE ") {
			t.Errorf("second run deleted something: %s", c)
		}
	}
	if f.count("POST /api/v3/core/groups/") != 0 {
		t.Error("second run added a membership that already existed")
	}
	for _, path := range []string{"/api/v3/providers/oauth2/", "/api/v3/core/applications/", "/api/v3/propertymappings/provider/scope/"} {
		if f.count("PATCH "+path) != 3 {
			t.Errorf("second run patched %s %d times, want 3", path, f.count("PATCH "+path))
		}
	}
}

func TestEnsureAgenticIAMAuthentikRepairsDrift(t *testing.T) {
	f := newFakeAuthentik(t)
	first, err := EnsureAgenticIAMAuthentik("docker", f.client(), testAgenticIAMConfig)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Someone edits the lab by hand in the Authentik admin UI.
	vault := find(f.providers, "name", "vault-agentic")
	vault["grant_types"] = []any{"authorization_code"}
	vault["jwt_federation_providers"] = []any{}
	payroll := find(f.mappings, "name", "hal: Agentic IAM vault:payroll")
	payroll["expression"] = "return {}"
	vaultApp := find(f.apps, "slug", "vault-agentic")
	for _, b := range f.bindings {
		if b["group"] == groupPK(t, f, "finance") {
			b["negate"] = true
		}
	}
	f.bindings = append(f.bindings, map[string]any{
		"pk": "binding-stray", "target": vaultApp["pbm_uuid"], "group": groupPK(t, f, "sales"),
		"policy": nil, "user": nil, "enabled": true, "negate": false, "order": 5,
	})
	alice := find(f.users, "username", "alice")
	find(f.tokens, "identifier", "hal-agentic-iam-finance-agent")["user"] = asInt(alice["pk"])

	second, err := EnsureAgenticIAMAuthentik("docker", f.client(), testAgenticIAMConfig)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}

	if len(f.providers) != 3 || find(f.providers, "name", "vault-agentic")["pk"] != vault["pk"] {
		t.Errorf("vault-agentic was recreated instead of updated: %v", f.providers)
	}
	if g := strs(vault["grant_types"]); !reflect.DeepEqual(g, []string{AuthentikGrantTokenExchange}) {
		t.Errorf("vault-agentic grant_types = %v after repair", g)
	}
	if fed := ints(vault["jwt_federation_providers"]); len(fed) != 2 {
		t.Errorf("vault-agentic jwt_federation_providers = %v after repair", fed)
	}
	if !strings.Contains(fmt.Sprint(payroll["expression"]), "agentic-db/creds/payroll") {
		t.Errorf("payroll expression = %v after repair", payroll["expression"])
	}
	var bound []string
	for _, b := range f.bindings {
		if b["negate"] != false {
			t.Errorf("negated binding %v kept", b)
		}
		bound = append(bound, fmt.Sprint(b["group"]))
	}
	slices.Sort(bound)
	want := []string{groupPK(t, f, "engineering"), groupPK(t, f, "finance")}
	slices.Sort(want)
	if !reflect.DeepEqual(bound, want) {
		t.Errorf("bound groups = %v after repair, want finance and engineering", bound)
	}
	actor := find(f.users, "username", "finance-agent")
	tok := find(f.tokens, "identifier", "hal-agentic-iam-finance-agent")
	if asInt(tok["user"]) != asInt(actor["pk"]) {
		t.Errorf("actor token belongs to user %v, want the actor %v", tok["user"], actor["pk"])
	}
	if second.ActorAppPassword == first.ActorAppPassword {
		t.Error("a recreated token kept its old key")
	}
	if second.VaultAgentic != first.VaultAgentic {
		t.Errorf("vault-agentic credentials changed on repair: %+v, was %+v", second.VaultAgentic, first.VaultAgentic)
	}
}

func TestRemoveAgenticIAMAuthentikKeepsSharedPersonas(t *testing.T) {
	f := newFakeAuthentik(t)
	seedVaultOIDC(f)
	if _, err := EnsureAgenticIAMAuthentik("podman", f.client(), testAgenticIAMConfig); err != nil {
		t.Fatalf("EnsureAgenticIAMAuthentik: %v", err)
	}

	if err := RemoveAgenticIAMAuthentik(f.client(), AgenticIAMRemoveOptions{}); err != nil {
		t.Fatalf("RemoveAgenticIAMAuthentik: %v", err)
	}

	var users []string
	for _, u := range f.users {
		users = append(users, fmt.Sprint(u["username"]))
	}
	if !reflect.DeepEqual(users, []string{"alice", "bob"}) {
		t.Errorf("users = %v, want alice and bob only", users)
	}
	if g := strs(find(f.users, "username", "alice")["groups"]); !reflect.DeepEqual(g, []string{"group-admin"}) {
		t.Errorf("alice groups = %v, want only hal vault oidc's admin", g)
	}
	if g := strs(find(f.users, "username", "bob")["groups"]); !reflect.DeepEqual(g, []string{"group-user-ro"}) {
		t.Errorf("bob groups = %v, want only hal vault oidc's user-ro", g)
	}
	assertOnly := func(kind string, items []map[string]any, key string, want ...string) {
		t.Helper()
		var got []string
		for _, it := range items {
			got = append(got, fmt.Sprint(it[key]))
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, want %v", kind, got, want)
		}
	}
	assertOnly("groups", f.groups, "name", "admin", "user-ro")
	assertOnly("providers", f.providers, "name", "vault-oidc-provider")
	assertOnly("applications", f.apps, "slug", "hashicorp-vault")
	assertOnly("custom scope mappings", slices.DeleteFunc(slices.Clone(f.mappings), func(m map[string]any) bool {
		return m["managed"] != nil
	}), "name", "hal: OIDC groups scope")
	if len(f.bindings) != 0 || len(f.tokens) != 0 {
		t.Errorf("bindings = %v, tokens = %v, want none", f.bindings, f.tokens)
	}

	// A second remove finds nothing to delete and still succeeds.
	if err := RemoveAgenticIAMAuthentik(f.client(), AgenticIAMRemoveOptions{}); err != nil {
		t.Errorf("second RemoveAgenticIAMAuthentik: %v", err)
	}
}

func TestRemoveAgenticIAMAuthentikDeletesSharedPersonasWhenAsked(t *testing.T) {
	f := newFakeAuthentik(t)
	seedVaultOIDC(f)
	if _, err := EnsureAgenticIAMAuthentik("podman", f.client(), testAgenticIAMConfig); err != nil {
		t.Fatalf("EnsureAgenticIAMAuthentik: %v", err)
	}
	if err := RemoveAgenticIAMAuthentik(f.client(), AgenticIAMRemoveOptions{DeleteSharedPersonas: true}); err != nil {
		t.Fatalf("RemoveAgenticIAMAuthentik: %v", err)
	}
	if len(f.users) != 0 {
		t.Errorf("users = %v, want none", f.users)
	}
}

func TestAgenticIAMSharedPersonasInUse(t *testing.T) {
	cases := []struct {
		consumers []string
		want      bool
	}{
		{nil, false},
		{[]string{}, false},
		{[]string{AgenticIAMAuthentikConsumer}, false},
		{[]string{"vault-oidc"}, true},
		{[]string{AgenticIAMAuthentikConsumer, "tfe-saml"}, true},
		{[]string{"tfe-bis-saml"}, true},
	}
	for _, tc := range cases {
		if got := AgenticIAMSharedPersonasInUse(tc.consumers); got != tc.want {
			t.Errorf("AgenticIAMSharedPersonasInUse(%v) = %v, want %v", tc.consumers, got, tc.want)
		}
	}
}

func TestEnsureAgenticIAMAuthentikRejectsIncompleteConfig(t *testing.T) {
	f := newFakeAuthentik(t)
	for name, mutate := range map[string]func(*AgenticIAMAuthentikConfig){
		"no public URL":       func(c *AgenticIAMAuthentikConfig) { c.ChatPublicURL = "" },
		"relative public URL": func(c *AgenticIAMAuthentikConfig) { c.ChatPublicURL = "localhost:8095" },
		"no mount":            func(c *AgenticIAMAuthentikConfig) { c.DBMount = "/" },
		"no payroll role":     func(c *AgenticIAMAuthentikConfig) { c.PayrollRole = "" },
	} {
		cfg := testAgenticIAMConfig
		mutate(&cfg)
		if _, err := EnsureAgenticIAMAuthentik("podman", f.client(), cfg); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("an incomplete config reached Authentik: %v", f.calls)
	}
}

func TestRARScopeExpression(t *testing.T) {
	got := rarScopeExpression("agentic-db/creds/forecasts")
	want := `return {"authorization_details":[{"type":"vault:path_access","path":"agentic-db/creds/forecasts","capabilities":["read"]}]}`
	if got != want {
		t.Errorf("rarScopeExpression = %s, want %s", got, want)
	}
}
