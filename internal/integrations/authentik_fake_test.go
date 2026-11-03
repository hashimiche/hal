package integrations

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// fakeAuthentik is an in-memory Authentik API, just deep enough for the Ensure*
// helpers: objects are JSON maps, and unique keys are enforced like Authentik
// does (400 on a duplicate name, slug, username or identifier).
type fakeAuthentik struct {
	t      *testing.T
	srv    *httptest.Server
	nextID int

	groups    []map[string]any
	users     []map[string]any
	flows     []map[string]any
	keys      []map[string]any
	mappings  []map[string]any
	providers []map[string]any
	apps      []map[string]any
	bindings  []map[string]any
	tokens    []map[string]any

	// calls records "METHOD /path" for every request, without the query.
	calls []string
	// shellScripts records every script sent to the fake ak shell.
	shellScripts []string
}

func newFakeAuthentik(t *testing.T) *fakeAuthentik {
	t.Helper()
	f := &fakeAuthentik{t: t}
	f.flows = []map[string]any{
		{"pk": "flow-authz-explicit", "slug": "default-provider-authorization-explicit-consent", "designation": "authorization"},
		{"pk": "flow-authz-implicit", "slug": "default-provider-authorization-implicit-consent", "designation": "authorization"},
		{"pk": "flow-inval", "slug": "default-invalidation-flow", "designation": "invalidation"},
		{"pk": "flow-inval-provider", "slug": "default-provider-invalidation-flow", "designation": "invalidation"},
	}
	f.keys = []map[string]any{{"pk": "key-self-signed", "name": "authentik Self-signed Certificate"}}
	for _, s := range []string{"openid", "profile", "email", "offline_access"} {
		f.mappings = append(f.mappings, map[string]any{
			"pk": "managed-" + s, "name": "authentik default OAuth Mapping: " + s,
			"scope_name": s, "managed": "goauthentik.io/providers/oauth2/scope-" + s,
		})
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)

	prev := authentikShell
	authentikShell = f.shell
	t.Cleanup(func() { authentikShell = prev })
	return f
}

func (f *fakeAuthentik) client() *AuthentikClient {
	return &AuthentikClient{baseURL: f.srv.URL, token: "test-token", http: f.srv.Client()}
}

func (f *fakeAuthentik) id(prefix string) string {
	f.nextID++
	return fmt.Sprintf("%s-%d", prefix, f.nextID)
}

func (f *fakeAuthentik) intID() int {
	f.nextID++
	return f.nextID
}

// asInt turns a JSON number (float64 once decoded) or an int into an int.
func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	}
	return -1
}

// strs turns a decoded JSON list of strings into a []string.
func strs(v any) []string {
	var out []string
	switch l := v.(type) {
	case []string:
		return l
	case []any:
		for _, s := range l {
			out = append(out, fmt.Sprint(s))
		}
	}
	return out
}

// ints turns a decoded JSON list of numbers into a []int.
func ints(v any) []int {
	out := []int{}
	if l, ok := v.([]any); ok {
		for _, n := range l {
			out = append(out, asInt(n))
		}
	}
	return out
}

func find(items []map[string]any, key string, value any) map[string]any {
	for _, it := range items {
		if fmt.Sprint(it[key]) == fmt.Sprint(value) {
			return it
		}
	}
	return nil
}

func without(items []map[string]any, key string, value any) []map[string]any {
	out := items[:0]
	for _, it := range items {
		if fmt.Sprint(it[key]) != fmt.Sprint(value) {
			out = append(out, it)
		}
	}
	return out
}

func (f *fakeAuthentik) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// list answers a list request: every query parameter but the pagination ones
// filters on equality, "managed" may repeat, and "search" matches the name.
func (f *fakeAuthentik) list(w http.ResponseWriter, r *http.Request, items []map[string]any) {
	results := []map[string]any{}
	q := r.URL.Query()
	for _, it := range items {
		ok := true
		for key, values := range q {
			switch key {
			case "page_size", "ordering", "has_key":
			case "managed":
				ok = ok && slices.Contains(values, fmt.Sprint(it["managed"]))
			case "search":
				ok = ok && strings.Contains(fmt.Sprint(it["name"]), values[0])
			default:
				ok = ok && fmt.Sprint(it[key]) == values[0]
			}
		}
		if ok {
			results = append(results, it)
		}
	}
	f.writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (f *fakeAuthentik) body(r *http.Request) map[string]any {
	var b map[string]any
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		f.t.Errorf("%s %s: bad JSON body: %v", r.Method, r.URL.Path, err)
	}
	return b
}

func merge(dst, src map[string]any) {
	for k, v := range src {
		dst[k] = v
	}
}

func (f *fakeAuthentik) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer test-token" {
		f.writeJSON(w, http.StatusForbidden, nil)
		return
	}
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	path := strings.TrimPrefix(r.URL.Path, "/api/v3/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	notFound := func() { f.writeJSON(w, http.StatusNotFound, map[string]any{"detail": "Not found."}) }
	duplicate := func(field string) {
		f.writeJSON(w, http.StatusBadRequest, map[string]any{field: []string{"already exists"}})
	}

	route := r.Method + " " + strings.Join(parts[:min(len(parts), 2)], "/")
	switch {
	case route == "GET flows/instances":
		f.list(w, r, f.flows)
	case route == "GET crypto/certificatekeypairs":
		f.list(w, r, f.keys)

	// ── groups ──
	case route == "GET core/groups" && len(parts) == 2:
		f.list(w, r, f.groups)
	case route == "POST core/groups" && len(parts) == 2:
		b := f.body(r)
		if find(f.groups, "name", b["name"]) != nil {
			duplicate("name")
			return
		}
		b["pk"] = f.id("group")
		f.groups = append(f.groups, b)
		f.writeJSON(w, http.StatusCreated, b)
	case route == "POST core/groups" && len(parts) == 4 && parts[3] == "add_user":
		u := find(f.users, "pk", asInt(f.body(r)["pk"]))
		if find(f.groups, "pk", parts[2]) == nil || u == nil {
			notFound()
			return
		}
		if gs := strs(u["groups"]); !slices.Contains(gs, parts[2]) {
			u["groups"] = append(gs, parts[2])
		}
		f.writeJSON(w, http.StatusNoContent, nil)
	case route == "DELETE core/groups" && len(parts) == 3:
		if find(f.groups, "pk", parts[2]) == nil {
			notFound()
			return
		}
		f.groups = without(f.groups, "pk", parts[2])
		for _, u := range f.users {
			u["groups"] = slices.DeleteFunc(strs(u["groups"]), func(g string) bool { return g == parts[2] })
		}
		f.writeJSON(w, http.StatusNoContent, nil)

	// ── users ──
	case route == "GET core/users" && len(parts) == 2:
		f.list(w, r, f.users)
	case route == "POST core/users" && len(parts) == 2:
		b := f.body(r)
		if find(f.users, "username", b["username"]) != nil {
			duplicate("username")
			return
		}
		b["pk"] = f.intID()
		b["groups"] = strs(b["groups"])
		f.users = append(f.users, b)
		f.writeJSON(w, http.StatusCreated, b)
	case route == "POST core/users" && len(parts) == 4 && parts[3] == "set_password":
		u := find(f.users, "pk", parts[2])
		if u == nil {
			notFound()
			return
		}
		u["password"] = f.body(r)["password"]
		f.writeJSON(w, http.StatusNoContent, nil)
	case route == "DELETE core/users" && len(parts) == 3:
		if find(f.users, "pk", parts[2]) == nil {
			notFound()
			return
		}
		f.users = without(f.users, "pk", parts[2])
		f.tokens = without(f.tokens, "user", parts[2])
		f.writeJSON(w, http.StatusNoContent, nil)

	// ── scope mappings ──
	case strings.HasPrefix(path, "propertymappings/provider/scope/"):
		pk := strings.TrimPrefix(strings.Trim(path, "/"), "propertymappings/provider/scope")
		pk = strings.Trim(pk, "/")
		switch {
		case r.Method == "GET" && pk == "":
			f.list(w, r, f.mappings)
		case r.Method == "POST" && pk == "":
			b := f.body(r)
			if find(f.mappings, "name", b["name"]) != nil {
				duplicate("name")
				return
			}
			b["pk"] = f.id("mapping")
			f.mappings = append(f.mappings, b)
			f.writeJSON(w, http.StatusCreated, b)
		case r.Method == "PATCH":
			m := find(f.mappings, "pk", pk)
			if m == nil {
				notFound()
				return
			}
			merge(m, f.body(r))
			f.writeJSON(w, http.StatusOK, m)
		case r.Method == "DELETE":
			if find(f.mappings, "pk", pk) == nil {
				notFound()
				return
			}
			f.mappings = without(f.mappings, "pk", pk)
			f.writeJSON(w, http.StatusNoContent, nil)
		default:
			f.unexpected(w, r)
		}

	// ── OAuth2 providers ──
	case route == "GET providers/oauth2" && len(parts) == 2:
		f.list(w, r, f.providers)
	case route == "POST providers/oauth2" && len(parts) == 2:
		b := f.body(r)
		if find(f.providers, "name", b["name"]) != nil {
			duplicate("name")
			return
		}
		n := f.intID()
		b["pk"] = n
		b["client_id"] = fmt.Sprintf("client-%d", n)
		b["client_secret"] = fmt.Sprintf("secret-%d", n)
		f.providers = append(f.providers, b)
		f.writeJSON(w, http.StatusCreated, b)
	case route == "PATCH providers/oauth2" && len(parts) == 3:
		p := find(f.providers, "pk", parts[2])
		if p == nil {
			notFound()
			return
		}
		merge(p, f.body(r))
		f.writeJSON(w, http.StatusOK, p)
	case route == "DELETE providers/oauth2" && len(parts) == 3:
		if find(f.providers, "pk", parts[2]) == nil {
			notFound()
			return
		}
		f.providers = without(f.providers, "pk", parts[2])
		f.writeJSON(w, http.StatusNoContent, nil)

	// ── applications ──
	case route == "POST core/applications" && len(parts) == 2:
		b := f.body(r)
		if find(f.apps, "slug", b["slug"]) != nil {
			duplicate("slug")
			return
		}
		b["pbm_uuid"] = f.id("pbm")
		f.apps = append(f.apps, b)
		f.writeJSON(w, http.StatusCreated, b)
	case route == "GET core/applications" && len(parts) == 3:
		if a := find(f.apps, "slug", parts[2]); a != nil {
			f.writeJSON(w, http.StatusOK, a)
			return
		}
		notFound()
	case route == "PATCH core/applications" && len(parts) == 3:
		a := find(f.apps, "slug", parts[2])
		if a == nil {
			notFound()
			return
		}
		merge(a, f.body(r))
		f.writeJSON(w, http.StatusOK, a)
	case route == "DELETE core/applications" && len(parts) == 3:
		a := find(f.apps, "slug", parts[2])
		if a == nil {
			notFound()
			return
		}
		f.apps = without(f.apps, "slug", parts[2])
		f.bindings = without(f.bindings, "target", a["pbm_uuid"])
		f.writeJSON(w, http.StatusNoContent, nil)

	// ── policy bindings ──
	case route == "GET policies/bindings" && len(parts) == 2:
		f.list(w, r, f.bindings)
	case route == "POST policies/bindings" && len(parts) == 2:
		b := f.body(r)
		if find(f.apps, "pbm_uuid", b["target"]) == nil {
			f.writeJSON(w, http.StatusBadRequest, map[string]any{"target": []string{"does not exist"}})
			return
		}
		b["pk"] = f.id("binding")
		if _, ok := b["policy"]; !ok {
			b["policy"] = nil
		}
		if _, ok := b["user"]; !ok {
			b["user"] = nil
		}
		f.bindings = append(f.bindings, b)
		f.writeJSON(w, http.StatusCreated, b)
	case route == "DELETE policies/bindings" && len(parts) == 3:
		if find(f.bindings, "pk", parts[2]) == nil {
			notFound()
			return
		}
		f.bindings = without(f.bindings, "pk", parts[2])
		f.writeJSON(w, http.StatusNoContent, nil)

	// ── tokens ──
	case route == "GET core/tokens" && len(parts) == 2:
		f.list(w, r, f.tokens)
	case route == "POST core/tokens" && len(parts) == 2:
		b := f.body(r)
		if find(f.tokens, "identifier", b["identifier"]) != nil {
			duplicate("identifier")
			return
		}
		if find(f.users, "pk", asInt(b["user"])) == nil {
			f.writeJSON(w, http.StatusBadRequest, map[string]any{"user": []string{"does not exist"}})
			return
		}
		b["user"] = asInt(b["user"])
		b["key"] = f.id("key")
		f.tokens = append(f.tokens, b)
		f.writeJSON(w, http.StatusCreated, b)
	case route == "GET core/tokens" && len(parts) == 4 && parts[3] == "view_key":
		t := find(f.tokens, "identifier", parts[2])
		if t == nil {
			notFound()
			return
		}
		f.writeJSON(w, http.StatusOK, map[string]any{"key": t["key"]})
	case route == "DELETE core/tokens" && len(parts) == 3:
		if find(f.tokens, "identifier", parts[2]) == nil {
			notFound()
			return
		}
		f.tokens = without(f.tokens, "identifier", parts[2])
		f.writeJSON(w, http.StatusNoContent, nil)

	default:
		f.unexpected(w, r)
	}
}

func (f *fakeAuthentik) unexpected(w http.ResponseWriter, r *http.Request) {
	f.t.Errorf("unexpected request %s %s", r.Method, r.URL)
	f.writeJSON(w, http.StatusInternalServerError, nil)
}

var fakeShellUsernameRE = regexp.MustCompile(`(?m)^username = "([^"]+)"$`)

// shell fakes `ak shell` running authentikActorScript: get_or_create of a core
// Actor, which is a service-account user.
func (f *fakeAuthentik) shell(engine, script string) (string, error) {
	f.shellScripts = append(f.shellScripts, script)
	m := fakeShellUsernameRE.FindStringSubmatch(script)
	if m == nil {
		return "", fmt.Errorf("fake ak shell: no username in script")
	}
	actor := find(f.users, "username", m[1])
	if actor == nil {
		actor = map[string]any{"pk": f.intID(), "username": m[1], "type": "service_account", "groups": []string{}}
		f.users = append(f.users, actor)
	}
	return fmt.Sprintf("### authentik shell (2026.8.3)\n### Node test | Arch arm64\nHAL_ACTOR_PK=%d\n", asInt(actor["pk"])), nil
}

// count returns how many recorded calls start with prefix, e.g. "POST /api/v3/core/users/".
func (f *fakeAuthentik) count(prefix string) int {
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}
