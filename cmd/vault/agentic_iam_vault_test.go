package vault

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	vault "github.com/hashicorp/vault/api"
)

// ─── fake Vault ──────────────────────────────────────────────────────────────

type fakeVaultResponse struct {
	Status int
	Body   string
}

type fakeVaultCall struct {
	Key  string // "METHOD /v1/path", with LIST for GET ?list=true
	Body map[string]any
}

// fakeVault answers from a route table keyed by "METHOD /v1/path". Unknown
// reads answer 404, unknown writes and deletes 204, like Vault for most paths.
type fakeVault struct {
	routes map[string]fakeVaultResponse
	// seq answers a route with successive responses, before routes; the last
	// one repeats.
	seq   map[string][]fakeVaultResponse
	calls []fakeVaultCall
}

func newFakeVault(t *testing.T, routes map[string]fakeVaultResponse) (*fakeVault, *vault.Client) {
	t.Helper()
	fv := &fakeVault{routes: routes, seq: map[string][]fakeVaultResponse{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.Method
		if method == http.MethodGet && r.URL.Query().Get("list") == "true" {
			method = "LIST"
		}
		key := method + " " + r.URL.Path
		call := fakeVaultCall{Key: key}
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			_ = json.Unmarshal(raw, &call.Body)
		}
		fv.calls = append(fv.calls, call)

		resp, ok := fv.routes[key]
		if q := fv.seq[key]; len(q) > 0 {
			resp, ok = q[0], true
			if len(q) > 1 {
				fv.seq[key] = q[1:]
			}
		}
		if !ok {
			resp = fakeVaultResponse{Status: http.StatusNoContent}
			if method == http.MethodGet || method == "LIST" {
				resp = fakeVaultResponse{Status: http.StatusNotFound, Body: `{"errors":[]}`}
			}
		}
		if resp.Body != "" {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(resp.Status)
		_, _ = io.WriteString(w, resp.Body)
	}))
	t.Cleanup(srv.Close)

	client, err := vault.NewClient(&vault.Config{Address: srv.URL})
	if err != nil {
		t.Fatalf("vault client: %v", err)
	}
	client.SetToken("test")
	return fv, client
}

func (fv *fakeVault) keys() []string {
	out := make([]string, 0, len(fv.calls))
	for _, c := range fv.calls {
		out = append(out, c.Key)
	}
	return out
}

func (fv *fakeVault) call(t *testing.T, key string) fakeVaultCall {
	t.Helper()
	for _, c := range fv.calls {
		if c.Key == key {
			return c
		}
	}
	t.Fatalf("no call %q; calls: %v", key, fv.keys())
	return fakeVaultCall{}
}

func ok(body string) fakeVaultResponse { return fakeVaultResponse{Status: http.StatusOK, Body: body} }

const testIssuer = "http://authentik.localhost:9100/application/o/vault-agentic/"

// ─── issuer and profile ──────────────────────────────────────────────────────

func TestNormalizeIssuer(t *testing.T) {
	cases := map[string]string{
		testIssuer: "http://authentik.localhost:9100/application/o/vault-agentic",
		"http://authentik.localhost:9100/application/o/vault-agentic":     "http://authentik.localhost:9100/application/o/vault-agentic",
		" HTTP://Authentik.localhost:9100/application/o/Vault-Agentic// ": "http://authentik.localhost:9100/application/o/vault-agentic",
	}
	for in, want := range cases {
		if got := normalizeIssuer(in); got != want {
			t.Errorf("normalizeIssuer(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestJWKSURIForIssuer(t *testing.T) {
	want := "http://authentik.localhost:9100/application/o/vault-agentic/jwks/"
	for _, in := range []string{testIssuer, strings.TrimSuffix(testIssuer, "/")} {
		if got := jwksURIForIssuer(in); got != want {
			t.Errorf("jwksURIForIssuer(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOAuthResourceServerPayload(t *testing.T) {
	create := oauthResourceServerPayload(testIssuer, "client-123", true)
	if create["issuer_id"] != "http://authentik.localhost:9100/application/o/vault-agentic" {
		t.Errorf("issuer_id = %v, want the normalised issuer", create["issuer_id"])
	}
	if create["use_jwks"] != true || create["jwks_uri"] != "http://authentik.localhost:9100/application/o/vault-agentic/jwks/" {
		t.Errorf("JWKS fields = %v / %v", create["use_jwks"], create["jwks_uri"])
	}
	if !slices.Equal(create["audiences"].([]string), []string{"client-123"}) {
		t.Errorf("audiences = %v", create["audiences"])
	}
	if create["user_claim"] != "sub" || create["actor_claim"] != "act.sub" {
		t.Errorf("claims = %v / %v", create["user_claim"], create["actor_claim"])
	}
	if !slices.Equal(create["supported_algorithms"].([]string), []string{"RS256"}) {
		t.Errorf("supported_algorithms = %v", create["supported_algorithms"])
	}
	if create["optional_authorization_details"] != false {
		t.Error("RAR must stay mandatory: it carries the task scope")
	}
	if _, set := create["unique_id_claim"]; set {
		t.Error("unique_id_claim must keep Vault's default (jti)")
	}

	update := oauthResourceServerPayload(testIssuer, "client-123", false)
	if _, set := update["issuer_id"]; set {
		t.Error("issuer_id is immutable and must not be sent on update")
	}
}

func TestOAuthProfileNeedsRecreate(t *testing.T) {
	norm := normalizeIssuer(testIssuer)
	cases := []struct {
		name     string
		existing map[string]any
		want     bool
	}{
		{"same issuer", map[string]any{"issuer_id": norm, "unique_id_claim": "jti"}, false},
		{"same issuer, trailing slash", map[string]any{"issuer_id": testIssuer}, false},
		{"other issuer", map[string]any{"issuer_id": "http://authentik.localhost:9100/application/o/other"}, true},
		{"other unique_id_claim", map[string]any{"issuer_id": norm, "unique_id_claim": "sid"}, true},
	}
	for _, tc := range cases {
		if got := oauthProfileNeedsRecreate(tc.existing, testIssuer); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestEnsureOAuthResourceServerProfile(t *testing.T) {
	const path = "/v1/sys/config/oauth-resource-server/authentik"
	readBack := ok(`{"data":{"config_id":"cfg-2","issuer_id":"http://authentik.localhost:9100/application/o/vault-agentic"}}`)

	t.Run("absent: create with issuer_id", func(t *testing.T) {
		fv, client := newFakeVault(t, map[string]fakeVaultResponse{})
		fv.seq["GET "+path] = []fakeVaultResponse{{Status: http.StatusNotFound, Body: `{"errors":[]}`}, readBack}
		configID, err := ensureOAuthResourceServerProfile(client, testIssuer, "client-123")
		if err != nil {
			t.Fatalf("ensure: %v", err)
		}
		if configID != "cfg-2" {
			t.Errorf("config_id = %q, want cfg-2", configID)
		}
		if slices.Contains(fv.keys(), "DELETE "+path) {
			t.Error("an absent profile needs no delete")
		}
		if body := fv.call(t, "PUT "+path).Body; body["issuer_id"] != normalizeIssuer(testIssuer) {
			t.Errorf("create must send the normalised issuer_id: %v", body)
		}
	})

	t.Run("same issuer: update in place", func(t *testing.T) {
		fv, client := newFakeVault(t, map[string]fakeVaultResponse{"GET " + path: readBack})
		configID, err := ensureOAuthResourceServerProfile(client, testIssuer, "client-123")
		if err != nil {
			t.Fatalf("ensure: %v", err)
		}
		if configID != "cfg-2" {
			t.Errorf("config_id = %q", configID)
		}
		if slices.Contains(fv.keys(), "DELETE "+path) {
			t.Error("an unchanged issuer must not delete the profile")
		}
		if body := fv.call(t, "PUT "+path).Body; body["issuer_id"] != nil {
			t.Errorf("update must not send issuer_id: %v", body)
		}
	})

	t.Run("issuer changed: delete and recreate", func(t *testing.T) {
		fv, client := newFakeVault(t, map[string]fakeVaultResponse{
			"GET " + path: ok(`{"data":{"config_id":"cfg-1","issuer_id":"http://old.example/application/o/vault-agentic"}}`),
		})
		if _, err := ensureOAuthResourceServerProfile(client, testIssuer, "client-123"); err != nil {
			t.Fatalf("ensure: %v", err)
		}
		keys := fv.keys()
		del, put := slices.Index(keys, "DELETE "+path), slices.Index(keys, "PUT "+path)
		if del < 0 || put < del {
			t.Fatalf("want DELETE then PUT, got %v", keys)
		}
		if body := fv.call(t, "PUT "+path).Body; body["issuer_id"] != normalizeIssuer(testIssuer) {
			t.Errorf("recreate must send the new issuer_id: %v", body)
		}
	})
}

// ─── names ───────────────────────────────────────────────────────────────────

// SCIM pushes Authentik's alice, bob, finance and engineering into Vault, so
// every lab identity object a persona or group maps to must be prefixed.
func TestAgenticIAMNamesAvoidSCIMCollisions(t *testing.T) {
	scimNames := []string{"alice", "bob", "charlie", "finance", "engineering", "sales"}
	var names []string
	for _, p := range agenticIAMPersonas {
		names = append(names, p.EntityName())
	}
	for _, g := range agenticIAMGroups {
		names = append(names, g.Name)
	}
	names = append(names, agenticIAMFinancePolicy, agenticIAMCeilingPolicy)
	for _, n := range names {
		if !strings.HasPrefix(n, agenticIAMPrefix) || slices.Contains(scimNames, n) {
			t.Errorf("%q must carry the %q prefix", n, agenticIAMPrefix)
		}
	}
	want := []string{"agentic-iam-alice", "agentic-iam-bob", "agentic-iam-finance", "agentic-iam-engineering"}
	if !slices.Equal(names[:4], want) {
		t.Errorf("names = %v, want %v", names[:4], want)
	}
}

// ─── entities, aliases, groups ───────────────────────────────────────────────

func TestSplitLabAliases(t *testing.T) {
	spec := oauthAliasSpec{Name: "agentic-iam-alice", Issuer: normalizeIssuer(testIssuer), ExternalID: "alice"}
	aliases := []any{
		map[string]any{"id": "a-current", "name": "agentic-iam-alice", "mount_accessor": "oauth-resource-server_root_cfg-2", "issuer": testIssuer, "external_id": "alice"},
		map[string]any{"id": "a-old-profile", "name": "agentic-iam-alice", "mount_accessor": "oauth-resource-server_root_cfg-1"},
		map[string]any{"id": "a-other", "name": "something-else", "mount_accessor": "oauth-resource-server_root_cfg-2"},
	}
	current, stale := splitLabAliases(aliases, spec, "cfg-2")
	if current != "a-current" {
		t.Errorf("current = %q, want a-current", current)
	}
	if !slices.Equal(stale, []string{"a-old-profile"}) {
		t.Errorf("stale = %v, want [a-old-profile] (aliases with another name are not the lab's)", stale)
	}

	wrongSub := []any{map[string]any{"id": "a-bob", "name": "agentic-iam-alice", "mount_accessor": "x_cfg-2", "external_id": "bob"}}
	if current, stale := splitLabAliases(wrongSub, spec, "cfg-2"); current != "" || len(stale) != 1 {
		t.Errorf("an alias with another external_id must be stale: current=%q stale=%v", current, stale)
	}
}

func TestOAuthAliasPayloadCarriesIssuer(t *testing.T) {
	p := oauthAliasPayload("ent-1", oauthAliasSpec{Name: "finance-agent", Issuer: "iss", ExternalID: "finance-agent"})
	if p["issuer"] != "iss" || p["external_id"] != "finance-agent" || p["canonical_id"] != "ent-1" {
		t.Errorf("alias payload = %v", p)
	}
	if _, set := p["mount_accessor"]; set {
		t.Error("mount_accessor must be left to Vault, which derives it from issuer")
	}
}

func TestEnsureLabEntity(t *testing.T) {
	t.Run("refuses an entity the lab does not own", func(t *testing.T) {
		fv, client := newFakeVault(t, map[string]fakeVaultResponse{
			"GET /v1/identity/entity/name/agentic-iam-alice": ok(`{"data":{"id":"e-1","name":"agentic-iam-alice","metadata":null}}`),
		})
		if _, err := ensureLabEntity(client, "agentic-iam-alice"); err == nil {
			t.Fatal("want an error for an unmarked entity")
		}
		for _, k := range fv.keys() {
			if !strings.HasPrefix(k, "GET ") {
				t.Errorf("must not write to a foreign entity, got %s", k)
			}
		}
	})
	t.Run("reuses a lab entity", func(t *testing.T) {
		_, client := newFakeVault(t, map[string]fakeVaultResponse{
			"GET /v1/identity/entity/name/finance-agent": ok(`{"data":{"id":"e-2","metadata":{"hal_lab":"agentic-iam"}}}`),
		})
		if id, err := ensureLabEntity(client, "finance-agent"); err != nil || id != "e-2" {
			t.Fatalf("got %q, %v; want e-2", id, err)
		}
	})
	t.Run("creates a missing entity with the marker", func(t *testing.T) {
		fv, client := newFakeVault(t, map[string]fakeVaultResponse{
			"PUT /v1/identity/entity": ok(`{"data":{"id":"e-3"}}`),
		})
		if id, err := ensureLabEntity(client, "agentic-iam-bob"); err != nil || id != "e-3" {
			t.Fatalf("got %q, %v; want e-3", id, err)
		}
		body := fv.call(t, "PUT /v1/identity/entity").Body
		if md, _ := body["metadata"].(map[string]any); md["hal_lab"] != "agentic-iam" {
			t.Errorf("entity created without the ownership marker: %v", body)
		}
	})
}

func TestLabGroupPayload(t *testing.T) {
	create := labGroupPayload(agenticIAMEngineeringGroup, nil, true)
	if create["type"] != "internal" {
		t.Errorf("type = %v, want internal", create["type"])
	}
	if p := create["policies"].([]string); p == nil || len(p) != 0 {
		t.Errorf("engineering must have an empty (not nil) policy list, got %#v", p)
	}
	if m := create["member_entity_ids"].([]string); m == nil {
		t.Error("member_entity_ids must be an empty list, so an update clears stale members")
	}
	update := labGroupPayload(agenticIAMFinanceGroup, []string{"e-1"}, false)
	if _, set := update["type"]; set {
		t.Error("type must only be sent on create")
	}
	if !slices.Equal(update["policies"].([]string), []string{"agentic-iam-finance"}) {
		t.Errorf("finance policies = %v", update["policies"])
	}
}

// ─── Agent Registry ──────────────────────────────────────────────────────────

const (
	regByName   = "/v1/agent-registry/registration/display-name/finance-agent"
	regByEntity = "/v1/agent-registry/registration/entity-id/e-agent"
	regRegister = "/v1/agent-registry/register"
	regNotFound = `{"errors":["agent with provided display_name does not exist"]}`
)

func TestEnsureAgentRegistration(t *testing.T) {
	t.Run("absent: register", func(t *testing.T) {
		fv, client := newFakeVault(t, map[string]fakeVaultResponse{
			"GET " + regByName:   {Status: http.StatusBadRequest, Body: regNotFound},
			"GET " + regByEntity: {Status: http.StatusBadRequest, Body: `{"errors":["agent with provided entity_id does not exist"]}`},
			"PUT " + regRegister: ok(`{"data":{"id":"reg-1","display_name":"finance-agent"}}`),
		})
		id, err := ensureAgentRegistration(client, "e-agent")
		if err != nil || id != "reg-1" {
			t.Fatalf("got %q, %v; want reg-1", id, err)
		}
		body := fv.call(t, "PUT "+regRegister).Body
		if body["id"] != nil {
			t.Errorf("a new registration must not send id: %v", body)
		}
		if body["entity_id"] != "e-agent" || body["display_name"] != "finance-agent" {
			t.Errorf("register body = %v", body)
		}
		if got := body["ceiling_policies"].([]any); len(got) != 1 || got[0] != agenticIAMCeilingPolicy {
			t.Errorf("ceiling_policies = %v", got)
		}
	})
	t.Run("present and owned: update by id", func(t *testing.T) {
		fv, client := newFakeVault(t, map[string]fakeVaultResponse{
			"GET " + regByName:   ok(`{"data":{"id":"reg-1","owner":"hal-agentic-iam-lab","entity_id":"e-old"}}`),
			"PUT " + regRegister: ok(`{"data":{"id":"reg-1"}}`),
		})
		if _, err := ensureAgentRegistration(client, "e-agent"); err != nil {
			t.Fatal(err)
		}
		if body := fv.call(t, "PUT "+regRegister).Body; body["id"] != "reg-1" || body["entity_id"] != "e-agent" {
			t.Errorf("update body = %v", body)
		}
	})
	t.Run("present and foreign: refuse", func(t *testing.T) {
		_, client := newFakeVault(t, map[string]fakeVaultResponse{
			"GET " + regByName: ok(`{"data":{"id":"reg-9","owner":"someone-else"}}`),
		})
		if _, err := ensureAgentRegistration(client, "e-agent"); err == nil {
			t.Fatal("want an error for a registration the lab does not own")
		}
	})
}

// ─── database mount ──────────────────────────────────────────────────────────

func TestAgenticIAMPolicies(t *testing.T) {
	finance := agenticIAMReadPolicy(agenticDBRoles)
	ceiling := agenticIAMReadPolicy(agenticIAMCeilingRoles)
	for _, p := range []string{"agentic-db/creds/quarterly-results", "agentic-db/creds/forecasts", "agentic-db/creds/payroll"} {
		if !strings.Contains(finance, `path "`+p+`"`) {
			t.Errorf("finance policy misses %s:\n%s", p, finance)
		}
	}
	if !strings.Contains(ceiling, "agentic-db/creds/quarterly-results") || !strings.Contains(ceiling, "agentic-db/creds/forecasts") {
		t.Errorf("ceiling must allow reports and forecasts:\n%s", ceiling)
	}
	if strings.Contains(ceiling, "payroll") {
		t.Errorf("ceiling must not allow payroll:\n%s", ceiling)
	}
	if strings.Contains(finance+ceiling, "update") || strings.Contains(finance+ceiling, "*") {
		t.Error("lab policies grant read on exact paths only")
	}
}

func TestAgenticDBRolePayload(t *testing.T) {
	for _, r := range agenticDBRoles {
		p := agenticDBRolePayload(r)
		stmts := strings.Join(p["creation_statements"].([]string), " ")
		if strings.Count(stmts, "GRANT") != 1 || !strings.Contains(stmts, "GRANT SELECT ON acme."+r.Table+" TO '{{name}}'@'%';") {
			t.Errorf("role %s must grant SELECT on acme.%s only: %s", r.Name, r.Table, stmts)
		}
		if p["db_name"] != "acme" || p["default_ttl"] != "5m" || p["max_ttl"] != "15m" {
			t.Errorf("role %s payload = %v", r.Name, p)
		}
		// Ephemeral users are obo-<role>-<8 random>: the role must fit the
		// template's truncation so it stays readable in the transparency panel.
		if len(r.Name) > 18 {
			t.Errorf("role name %q is longer than the username template keeps", r.Name)
		}
	}
	if got := len("obo-") + 18 + len("-") + 8; got > 32 {
		t.Errorf("username template can produce %d characters, over mysql-database-plugin's 32", got)
	}
}

func TestAgenticDBConnectionPayload(t *testing.T) {
	p := agenticDBConnectionPayload("agentic-iam-broker", "agentic-iam-temp-pass")
	if p["plugin_name"] != "mysql-database-plugin" {
		t.Errorf("plugin_name = %v (database.go uses mysql-database-plugin for MariaDB)", p["plugin_name"])
	}
	if p["connection_url"] != "{{username}}:{{password}}@tcp(hal-vault-mariadb:3306)/" {
		t.Errorf("connection_url = %v", p["connection_url"])
	}
	if !slices.Equal(p["allowed_roles"].([]string), []string{"quarterly-results", "forecasts", "payroll"}) {
		t.Errorf("allowed_roles = %v", p["allowed_roles"])
	}
	if p["username"] != "agentic-iam-broker" {
		t.Errorf("username = %v", p["username"])
	}
}

func TestAgenticIAMVaultConfigValidate(t *testing.T) {
	good := agenticIAMVaultConfig{Issuer: testIssuer, ClientID: "c", BrokerUser: agenticIAMDefaultBrokerUser, BrokerPassword: agenticIAMDefaultBrokerPassword}
	if err := good.validate(); err != nil {
		t.Fatalf("valid config refused: %v", err)
	}
	bad := []agenticIAMVaultConfig{
		{Issuer: "authentik.localhost/application/o/vault-agentic", ClientID: "c", BrokerUser: "u", BrokerPassword: "password1"},
		{Issuer: testIssuer, ClientID: " ", BrokerUser: "u", BrokerPassword: "password1"},
		{Issuer: testIssuer, ClientID: "c", BrokerUser: "vaultadmin", BrokerPassword: "password1"},
	}
	for i, c := range bad {
		if err := c.validate(); err == nil {
			t.Errorf("case %d: invalid config accepted: %+v", i, c)
		}
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func TestIsVaultNotFound(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{&vault.ResponseError{StatusCode: 404}, true},
		{&vault.ResponseError{StatusCode: 400, Errors: []string{"agent with provided display_name does not exist"}}, true},
		{&vault.ResponseError{StatusCode: 400, Errors: []string{"invalid request"}}, false},
		{&vault.ResponseError{StatusCode: 500, Errors: []string{"not found"}}, false},
		{errors.New("not found"), false},
	}
	for i, tc := range cases {
		if got := isVaultNotFound(tc.err); got != tc.want {
			t.Errorf("case %d: isVaultNotFound(%v) = %v, want %v", i, tc.err, got, tc.want)
		}
	}
}

func TestHasAgenticIAMMarker(t *testing.T) {
	if !hasAgenticIAMMarker(map[string]any{"metadata": map[string]any{"hal_lab": "agentic-iam"}}) {
		t.Error("marked object not recognised")
	}
	for _, data := range []map[string]any{{}, {"metadata": nil}, {"metadata": map[string]any{"hal_lab": "other"}}} {
		if hasAgenticIAMMarker(data) {
			t.Errorf("unmarked object recognised as the lab's: %v", data)
		}
	}
}

// ─── configure and teardown, end to end ──────────────────────────────────────

func (fv *fakeVault) callsTo(key string) []fakeVaultCall {
	var out []fakeVaultCall
	for _, c := range fv.calls {
		if c.Key == key {
			out = append(out, c)
		}
	}
	return out
}

func (fv *fakeVault) before(t *testing.T, first, second string) {
	t.Helper()
	keys := fv.keys()
	i, j := slices.Index(keys, first), slices.Index(keys, second)
	if i < 0 || j < 0 || i > j {
		t.Errorf("want %s before %s; calls: %v", first, second, keys)
	}
}

func toStrings(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, it := range items {
		s, _ := it.(string)
		out = append(out, s)
	}
	return out
}

func TestConfigureAgenticIAMVault(t *testing.T) {
	const profile = "/v1/sys/config/oauth-resource-server/authentik"
	fv, client := newFakeVault(t, map[string]fakeVaultResponse{
		"GET /v1/identity/entity/id/e-alice": ok(`{"data":{"id":"e-alice","aliases":[]}}`),
		"GET /v1/identity/entity/id/e-bob":   ok(`{"data":{"id":"e-bob","aliases":[]}}`),
		"GET /v1/identity/entity/id/e-agent": ok(`{"data":{"id":"e-agent","aliases":[]}}`),
		"GET " + regByName:                   {Status: http.StatusBadRequest, Body: regNotFound},
		"GET " + regByEntity:                 {Status: http.StatusBadRequest, Body: regNotFound},
		"PUT " + regRegister:                 ok(`{"data":{"id":"reg-1"}}`),
		"GET /v1/sys/mounts":                 ok(`{"data":{"sys/":{"type":"system"}}}`),
	})
	fv.seq["GET "+profile] = []fakeVaultResponse{
		{Status: http.StatusNotFound, Body: `{"errors":[]}`},
		ok(`{"data":{"config_id":"cfg-1"}}`),
	}
	fv.seq["PUT /v1/identity/entity"] = []fakeVaultResponse{
		ok(`{"data":{"id":"e-alice"}}`), ok(`{"data":{"id":"e-bob"}}`), ok(`{"data":{"id":"e-agent"}}`),
	}

	state, err := configureAgenticIAMVault(client, agenticIAMVaultConfig{
		Issuer: testIssuer, ClientID: "client-123",
		BrokerUser: agenticIAMDefaultBrokerUser, BrokerPassword: agenticIAMDefaultBrokerPassword,
	})
	if err != nil {
		t.Fatalf("configure: %v\ncalls: %v", err, fv.keys())
	}
	if state.ConfigID != "cfg-1" || state.AgentEntityID != "e-agent" || state.RegistrationID != "reg-1" ||
		state.PersonaEntityIDs["alice"] != "e-alice" || state.PersonaEntityIDs["bob"] != "e-bob" {
		t.Errorf("state = %+v", state)
	}
	if _, has := state.PersonaEntityIDs["charlie"]; has {
		t.Error("charlie must get no entity: the IdP stops him")
	}

	// One alias per entity, bound by issuer and the claim value.
	wantAliases := map[string]string{"agentic-iam-alice": "alice", "agentic-iam-bob": "bob", "finance-agent": "finance-agent"}
	aliases := fv.callsTo("PUT /v1/identity/entity-alias")
	if len(aliases) != len(wantAliases) {
		t.Fatalf("got %d aliases, want %d", len(aliases), len(wantAliases))
	}
	for _, a := range aliases {
		name, _ := a.Body["name"].(string)
		if a.Body["external_id"] != wantAliases[name] || a.Body["issuer"] != normalizeIssuer(testIssuer) {
			t.Errorf("alias %s = %v", name, a.Body)
		}
	}

	finance := fv.call(t, "PUT /v1/identity/group/name/agentic-iam-finance").Body
	if !slices.Equal(toStrings(finance["member_entity_ids"]), []string{"e-alice"}) || !slices.Equal(toStrings(finance["policies"]), []string{"agentic-iam-finance"}) {
		t.Errorf("finance group = %v", finance)
	}
	engineering := fv.call(t, "PUT /v1/identity/group/name/agentic-iam-engineering").Body
	if !slices.Equal(toStrings(engineering["member_entity_ids"]), []string{"e-bob"}) || len(toStrings(engineering["policies"])) != 0 {
		t.Errorf("engineering group = %v", engineering)
	}

	// Dependency order.
	fv.before(t, "PUT /v1/sys/policies/acl/agentic-iam-finance-agent-ceiling", "PUT "+regRegister)
	fv.before(t, "PUT "+profile, "PUT /v1/identity/entity-alias")
	fv.before(t, "POST /v1/sys/mounts/agentic-db", "PUT /v1/agentic-db/config/acme")
	fv.before(t, "PUT /v1/agentic-db/config/acme", "PUT /v1/agentic-db/rotate-root/acme")
	for _, r := range agenticDBRoles {
		fv.call(t, "PUT /v1/agentic-db/roles/"+r.Name)
	}
}

func TestTeardownAgenticIAMVault(t *testing.T) {
	fv, client := newFakeVault(t, map[string]fakeVaultResponse{
		"GET /v1/sys/mounts": ok(`{"data":{"agentic-db/":{"type":"database"},"database/":{"type":"database"}}}`),
		"GET " + regByName:   ok(`{"data":{"id":"reg-1","owner":"hal-agentic-iam-lab"}}`),
		"GET /v1/identity/entity/name/agentic-iam-alice": ok(`{"data":{"id":"e-alice","metadata":{"hal_lab":"agentic-iam"},
			"aliases":[{"id":"al-1","name":"agentic-iam-alice"},{"id":"al-x","name":"not-the-lab"}]}}`),
		"GET /v1/identity/entity/name/agentic-iam-bob":        ok(`{"data":{"id":"e-foreign","metadata":null}}`),
		"GET /v1/identity/group/name/agentic-iam-finance":     ok(`{"data":{"id":"g-1","metadata":{"hal_lab":"agentic-iam"}}}`),
		"GET /v1/identity/group/name/agentic-iam-engineering": ok(`{"data":{"id":"g-2","metadata":{"owner":"someone"}}}`),
	})

	if err := teardownAgenticIAMVault(client); err != nil {
		t.Fatalf("teardown: %v", err)
	}
	keys := fv.keys()
	for _, want := range []string{
		"PUT /v1/sys/leases/revoke-force/agentic-db",
		"DELETE /v1/sys/mounts/agentic-db",
		"DELETE " + regByName,
		"DELETE /v1/sys/config/oauth-resource-server/authentik",
		"DELETE /v1/identity/entity-alias/id/al-1",
		"DELETE /v1/identity/entity/name/agentic-iam-alice",
		"DELETE /v1/identity/group/name/agentic-iam-finance",
		"DELETE /v1/sys/policies/acl/agentic-iam-finance",
		"DELETE /v1/sys/policies/acl/agentic-iam-finance-agent-ceiling",
	} {
		if !slices.Contains(keys, want) {
			t.Errorf("teardown misses %s", want)
		}
	}
	for _, never := range []string{
		"DELETE /v1/sys/mounts/database",                         // the database/ feature
		"DELETE /v1/identity/entity-alias/id/al-x",               // not the lab's alias
		"DELETE /v1/identity/entity/name/agentic-iam-bob",        // no marker
		"DELETE /v1/identity/entity/name/finance-agent",          // absent
		"DELETE /v1/identity/group/name/agentic-iam-engineering", // no marker
	} {
		if slices.Contains(keys, never) {
			t.Errorf("teardown must not send %s", never)
		}
	}
	fv.before(t, "PUT /v1/sys/leases/revoke-force/agentic-db", "DELETE /v1/sys/mounts/agentic-db")
	fv.before(t, "DELETE /v1/sys/mounts/agentic-db", "DELETE "+regByName)
	fv.before(t, "DELETE "+regByName, "DELETE /v1/identity/entity/name/agentic-iam-alice")
}

func TestTeardownAgenticIAMVaultWithoutLicense(t *testing.T) {
	_, client := newFakeVault(t, map[string]fakeVaultResponse{
		"GET /v1/sys/mounts": ok(`{"data":{"sys/":{"type":"system"}}}`),
		"DELETE /v1/sys/config/oauth-resource-server/authentik": {Status: http.StatusUnauthorized, Body: featureNotEnabledBody},
	})
	if err := teardownAgenticIAMVault(client); err != nil {
		t.Errorf("teardown of a never-enabled lab must succeed, got %v", err)
	}
}
