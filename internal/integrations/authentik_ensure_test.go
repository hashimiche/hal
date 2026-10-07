package integrations

import (
	"errors"
	"strings"
	"testing"
)

func TestEnsureUserCreatesAnAbsentUser(t *testing.T) {
	f := newFakeAuthentik(t)
	c := f.client()
	gpk, err := c.EnsureGroup("sales")
	if err != nil {
		t.Fatalf("EnsureGroup: %v", err)
	}

	pk, err := c.EnsureUser("charlie", "Charlie", "charlie@hal.local", "password", []string{gpk})
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	u := find(f.users, "pk", pk)
	if u == nil || u["username"] != "charlie" || u["password"] != "password" {
		t.Fatalf("user = %v, want charlie with password %q", u, "password")
	}
	if g := strs(u["groups"]); len(g) != 1 || g[0] != gpk {
		t.Errorf("charlie groups = %v, want [%s]", g, gpk)
	}
}

func TestEnsureUserOnlyAddsMembershipToAnExistingUser(t *testing.T) {
	f := newFakeAuthentik(t)
	f.groups = append(f.groups, map[string]any{"pk": "group-admin", "name": "admin"})
	f.users = append(f.users, map[string]any{
		"pk": 7, "username": "alice", "name": "Alice Admin", "groups": []string{"group-admin"}, "password": "kept",
	})
	c := f.client()
	gpk, _ := c.EnsureGroup("finance")

	for range 2 {
		pk, err := c.EnsureUser("alice", "Alice", "alice@hal.local", "password", []string{gpk})
		if err != nil {
			t.Fatalf("EnsureUser: %v", err)
		}
		if pk != 7 {
			t.Errorf("pk = %d, want the existing 7", pk)
		}
	}
	u := find(f.users, "pk", 7)
	if g := strs(u["groups"]); len(g) != 2 || g[0] != "group-admin" || g[1] != gpk {
		t.Errorf("alice groups = %v, want admin then finance", g)
	}
	if u["password"] != "kept" || u["name"] != "Alice Admin" {
		t.Errorf("alice = %v, want password and name untouched", u)
	}
	if n := f.count("POST /api/v3/core/groups/" + gpk + "/add_user/"); n != 1 {
		t.Errorf("add_user called %d times, want 1", n)
	}
	if n := f.count("POST /api/v3/core/users/"); n != 0 {
		t.Errorf("%d user writes, want none", n)
	}
}

func TestEnsureGroupFindsAnExistingGroup(t *testing.T) {
	f := newFakeAuthentik(t)
	f.groups = append(f.groups, map[string]any{"pk": "group-finance", "name": "finance"})
	pk, err := f.client().EnsureGroup("finance")
	if err != nil || pk != "group-finance" {
		t.Errorf("EnsureGroup = %q, %v, want the existing group-finance", pk, err)
	}
	if len(f.groups) != 1 {
		t.Errorf("groups = %v, want no duplicate", f.groups)
	}
}

func TestGetManagedScopeMappingPKsIgnoresCustomMappings(t *testing.T) {
	f := newFakeAuthentik(t)
	f.mappings = append(f.mappings, map[string]any{"pk": "custom-openid", "name": "my openid", "scope_name": "openid"})
	c := f.client()

	pks, err := c.GetManagedScopeMappingPKs([]string{"openid", "profile", "email"})
	if err != nil {
		t.Fatalf("GetManagedScopeMappingPKs: %v", err)
	}
	if strings.Join(pks, ",") != "managed-openid,managed-profile,managed-email" {
		t.Errorf("pks = %v, want the three managed mappings in order", pks)
	}

	if _, err := c.GetManagedScopeMappingPKs([]string{"openid", "nope"}); err == nil {
		t.Error("a missing built-in mapping is not an error")
	}
}

func TestEnsureAppPasswordKeepsAMatchingToken(t *testing.T) {
	f := newFakeAuthentik(t)
	f.users = append(f.users, map[string]any{"pk": 3, "username": "finance-agent"})
	c := f.client()

	first, err := c.EnsureAppPassword("hal-test-token", 3, "test")
	if err != nil {
		t.Fatalf("EnsureAppPassword: %v", err)
	}
	second, err := c.EnsureAppPassword("hal-test-token", 3, "test")
	if err != nil {
		t.Fatalf("EnsureAppPassword again: %v", err)
	}
	if first == "" || first != second {
		t.Errorf("keys = %q then %q, want one stable key", first, second)
	}
	if len(f.tokens) != 1 || f.count("POST /api/v3/core/tokens/") != 1 {
		t.Errorf("tokens = %v after %d creates, want one", f.tokens, f.count("POST /api/v3/core/tokens/"))
	}
}

func TestEnsureAppPasswordRecreatesADriftedToken(t *testing.T) {
	f := newFakeAuthentik(t)
	f.users = append(f.users, map[string]any{"pk": 3, "username": "finance-agent"})
	f.tokens = append(f.tokens, map[string]any{
		"identifier": "hal-test-token", "intent": "app_password", "user": 3, "expiring": true, "key": "old",
	})

	key, err := f.client().EnsureAppPassword("hal-test-token", 3, "test")
	if err != nil {
		t.Fatalf("EnsureAppPassword: %v", err)
	}
	tok := find(f.tokens, "identifier", "hal-test-token")
	if key == "old" || tok["expiring"] != false || len(f.tokens) != 1 {
		t.Errorf("token = %v with key %q, want one recreated non-expiring token", tok, key)
	}
}

func TestEnsureAuthentikActor(t *testing.T) {
	var gotEngine, gotScript string
	reply := func(out string, err error) {
		authentikShell = func(engine, script string) (string, error) {
			gotEngine, gotScript = engine, script
			return out, err
		}
	}
	prev := authentikShell
	t.Cleanup(func() { authentikShell = prev })

	reply("### authentik shell (2026.8.3)\n{\"event\": \"noise\"}\nHAL_ACTOR_PK=42\n", nil)
	pk, err := EnsureAuthentikActor("docker", "finance-agent", `Finance "demo" agent`)
	if err != nil || pk != 42 {
		t.Fatalf("EnsureAuthentikActor = %d, %v, want 42", pk, err)
	}
	if gotEngine != "docker" {
		t.Errorf("engine = %q, want docker", gotEngine)
	}
	for _, want := range []string{
		`username = "finance-agent"`,
		`name = "Finance \"demo\" agent"`,
		`"parent": None`,
		`"policy_behavior": "none"`,
		`"type": UserTypes.SERVICE_ACCOUNT`,
		`"expiring": False`,
		`actor.set_unusable_password()`,
	} {
		if !strings.Contains(gotScript, want) {
			t.Errorf("script lacks %s:\n%s", want, gotScript)
		}
	}

	reply("HAL_ACTOR_ERROR=a user that is not an actor already has this username\n", nil)
	if _, err := EnsureAuthentikActor("podman", "finance-agent", "x"); err == nil || !strings.Contains(err.Error(), "not an actor") {
		t.Errorf("error marker: err = %v", err)
	}

	reply("### authentik shell\n", nil)
	if _, err := EnsureAuthentikActor("podman", "finance-agent", "x"); err == nil {
		t.Error("no pk printed: no error")
	}

	reply("", errors.New("exit status 1"))
	if _, err := EnsureAuthentikActor("podman", "finance-agent", "x"); err == nil {
		t.Error("ak shell failure: no error")
	}

	gotScript = ""
	if _, err := EnsureAuthentikActor("podman", `x"; import os`, "x"); err == nil || gotScript != "" {
		t.Errorf("invalid username: err = %v, script ran = %v", err, gotScript != "")
	}
}

func TestSetApplicationGroupBindingsWithNoGroupsDeletesAll(t *testing.T) {
	f := newFakeAuthentik(t)
	f.apps = append(f.apps, map[string]any{"slug": "vault-agentic", "pbm_uuid": "pbm-1"}, map[string]any{"slug": "other", "pbm_uuid": "pbm-2"})
	f.bindings = append(f.bindings,
		map[string]any{"pk": "b1", "target": "pbm-1", "group": "g1", "policy": nil, "user": nil, "enabled": true, "negate": false},
		map[string]any{"pk": "b2", "target": "pbm-2", "group": "g1", "policy": nil, "user": nil, "enabled": true, "negate": false},
	)
	if err := f.client().SetApplicationGroupBindings("pbm-1", nil); err != nil {
		t.Fatalf("SetApplicationGroupBindings: %v", err)
	}
	if len(f.bindings) != 1 || f.bindings[0]["pk"] != "b2" {
		t.Errorf("bindings = %v, want only the other application's", f.bindings)
	}
}

func TestApplicationExists(t *testing.T) {
	f := newFakeAuthentik(t)
	f.apps = append(f.apps, map[string]any{"slug": "hal-chat", "pbm_uuid": "pbm-1"})
	c := f.client()
	if ok, err := c.ApplicationExists("hal-chat"); err != nil || !ok {
		t.Errorf("ApplicationExists(hal-chat) = %v, %v, want true", ok, err)
	}
	if ok, err := c.ApplicationExists("vault-agentic"); err != nil || ok {
		t.Errorf("ApplicationExists(vault-agentic) = %v, %v, want false, nil", ok, err)
	}
	for _, call := range f.calls {
		if !strings.HasPrefix(call, "GET ") {
			t.Errorf("ApplicationExists made a write: %s", call)
		}
	}
}
