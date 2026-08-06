package integrations

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetTFEOIDCIssuer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"issuer": serverURLForRequest(r) + "/"})
	}))
	defer server.Close()

	issuer, err := GetTFEOIDCIssuer(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if issuer != server.URL {
		t.Fatalf("issuer = %q, want %q", issuer, server.URL)
	}
}

func TestGetTFEOIDCIssuerFallsBackToLoopbackForLocalhost(t *testing.T) {
	const wantIssuer = "https://tfe.localhost:8443"
	var wantHost string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != wantHost {
			t.Fatalf("Host = %q, want %q", r.Host, wantHost)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"issuer": wantIssuer})
	}))
	defer server.Close()

	wantHost = strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	wantHost = "tfe.localhost:" + wantHost
	baseURL := "http://" + wantHost
	issuer, err := GetTFEOIDCIssuer(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if issuer != wantIssuer {
		t.Fatalf("issuer = %q, want %q", issuer, wantIssuer)
	}
}

func TestTFELoopbackDialAddress(t *testing.T) {
	tests := map[string]string{
		"tfe.localhost:8443":     "127.0.0.1:8443",
		"TFE-BIS.LOCALHOST:9443": "127.0.0.1:9443",
		"localhost:8443":         "localhost:8443",
		"example.com:443":        "example.com:443",
		"not-a-host-port":        "not-a-host-port",
	}

	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			if got := tfeLoopbackDialAddress(input); got != want {
				t.Fatalf("tfeLoopbackDialAddress(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func TestTFEVarSetIDByNameFollowsPagination(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Query().Get("page[number]") == "2" {
			_, _ = fmt.Fprint(w, `{"data":[{"id":"varset-target","attributes":{"name":"hal-vault"}}],"links":{"next":null}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":[{"id":"varset-other","attributes":{"name":"other"}}],"links":{"next":"?page%5Bnumber%5D=2"}}`)
	}))
	defer server.Close()

	id, err := tfeVarSetIDByName(server.URL, "token", "hal", "hal-vault")
	if err != nil {
		t.Fatal(err)
	}
	if id != "varset-target" {
		t.Fatalf("id = %q", id)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestListTFEVarSetVarsFollowsPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page[number]") == "2" {
			_, _ = fmt.Fprint(w, `{"data":[{"id":"var-role","attributes":{"key":"TFC_VAULT_RUN_ROLE"}}],"links":{"next":null}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":[{"id":"var-addr","attributes":{"key":"TFC_VAULT_ADDR"}}],"links":{"next":"?page%5Bnumber%5D=2"}}`)
	}))
	defer server.Close()

	vars, err := listTFEVarSetVars(server.URL, "token", "varset-1")
	if err != nil {
		t.Fatal(err)
	}
	if vars["TFC_VAULT_ADDR"] != "var-addr" || vars["TFC_VAULT_RUN_ROLE"] != "var-role" {
		t.Fatalf("unexpected variables: %#v", vars)
	}
}

func TestEnsureTFEVariableSetReconcilesExistingAndMissingVariables(t *testing.T) {
	patched := map[string]string{}
	created := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/organizations/hal/varsets"):
			_, _ = fmt.Fprint(w, `{"data":[{"id":"varset-1","attributes":{"name":"hal-vault"}}],"links":{"next":null}}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/varsets/varset-1/relationships/vars"):
			_, _ = fmt.Fprint(w, `{"data":[{"id":"var-addr","attributes":{"key":"TFC_VAULT_ADDR"}}],"links":{"next":null}}`)
		case r.Method == http.MethodPatch:
			key, value := decodeTFEVariablePayload(t, r)
			patched[key] = value
			_, _ = fmt.Fprint(w, `{"data":{"id":"var-addr"}}`)
		case r.Method == http.MethodPost:
			key, value := decodeTFEVariablePayload(t, r)
			created[key] = value
			_, _ = fmt.Fprint(w, `{"data":{"id":"var-new"}}`)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()

	err := EnsureTFEVariableSet(server.URL, "token", "hal", "hal-vault", map[string]string{
		"TFC_VAULT_ADDR":     "http://hal-vault:8200",
		"TFC_VAULT_RUN_ROLE": "tfe-workspace-role",
	})
	if err != nil {
		t.Fatal(err)
	}
	if patched["TFC_VAULT_ADDR"] != "http://hal-vault:8200" {
		t.Fatalf("patched variables = %#v", patched)
	}
	if created["TFC_VAULT_RUN_ROLE"] != "tfe-workspace-role" {
		t.Fatalf("created variables = %#v", created)
	}
}

func TestTFERequestIncludesErrorDetail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = fmt.Fprint(w, `{"errors":[{"detail":"conflicting global variable"}]}`)
	}))
	defer server.Close()

	_, _, err := TFERequest(http.MethodPost, server.URL, "token", map[string]string{"test": "value"})
	if err == nil || !strings.Contains(err.Error(), "conflicting global variable") {
		t.Fatalf("error = %v", err)
	}
}

func TestDeleteTFEVariableSetVariableRemovesLegacyKey(t *testing.T) {
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/organizations/hal/varsets"):
			_, _ = fmt.Fprint(w, `{"data":[{"id":"varset-1","attributes":{"name":"hal-vault"}}],"links":{"next":null}}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/varsets/varset-1/relationships/vars"):
			_, _ = fmt.Fprint(w, `{"data":[{"id":"var-legacy","attributes":{"key":"VAULT_ADDR"}}],"links":{"next":null}}`)
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/varsets/varset-1/relationships/vars/var-legacy"):
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()

	if err := DeleteTFEVariableSetVariable(server.URL, "token", "hal", "hal-vault", "VAULT_ADDR"); err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("legacy VAULT_ADDR variable was not deleted")
	}
}

func decodeTFEVariablePayload(t *testing.T, r *http.Request) (string, string) {
	t.Helper()
	var payload struct {
		Data struct {
			Attributes struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return payload.Data.Attributes.Key, payload.Data.Attributes.Value
}

func serverURLForRequest(r *http.Request) string {
	return "http://" + r.Host
}
