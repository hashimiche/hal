package vault

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

// The exact answer of a 2.1.1-ent dev Vault whose license lacks the Agentic
// IAM terms, captured with `curl -X LIST .../v1/sys/config/oauth-resource-server`.
const featureNotEnabledBody = `{"errors":["1 error occurred:\n\t* Feature Not Enabled\n\n"]}`

const (
	healthKey = "GET /v1/sys/health"
	mountsKey = "GET /v1/sys/mounts"
	probeKey  = "LIST /v1/sys/config/oauth-resource-server"
)

func healthBody(version string) fakeVaultResponse {
	return ok(`{"initialized":true,"sealed":false,"standby":false,"version":"` + version + `"}`)
}

var mountsWithRegistry = ok(`{"data":{"agent-registry/":{"type":"agent_registry","accessor":"agent-registry_1"},"sys/":{"type":"system"}}}`)

func TestParseVaultVersion(t *testing.T) {
	cases := map[string][3]int{
		"2.1.1+ent":     {2, 1, 1},
		"2.1.1-ent":     {2, 1, 1},
		"v2.0.4":        {2, 0, 4},
		"2.1.0-rc1+ent": {2, 1, 0},
		"2.1":           {2, 1, 0},
	}
	for in, want := range cases {
		got, ok := parseVaultVersion(in)
		if !ok || got != want {
			t.Errorf("parseVaultVersion(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "ent", "2", "2.x.1", "1.2.3.4"} {
		if _, ok := parseVaultVersion(in); ok {
			t.Errorf("parseVaultVersion(%q) accepted an invalid version", in)
		}
	}
}

func TestVaultVersionAtLeast(t *testing.T) {
	cases := []struct {
		v    string
		want bool
	}{
		{"2.1.0+ent", true},
		{"2.1.1+ent", true},
		{"2.2.0+ent", true},
		{"3.0.0+ent", true},
		{"2.0.4+ent", false},
		{"1.21.3+ent", false},
		{"garbage", false},
	}
	for _, tc := range cases {
		if got := vaultVersionAtLeast(tc.v, "2.1.0"); got != tc.want {
			t.Errorf("vaultVersionAtLeast(%q, 2.1.0) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

func TestCheckAgenticIAMVaultVersion(t *testing.T) {
	cases := []struct {
		version string
		want    error
	}{
		{"2.1.1", errAgenticIAMNotEnterprise},
		{"2.0.4", errAgenticIAMNotEnterprise},
		{"2.0.4+ent", errAgenticIAMVaultTooOld},
		{"2.1.0+ent", nil},
		{"2.1.1+ent", nil},
	}
	for _, tc := range cases {
		err := checkAgenticIAMVaultVersion(tc.version, false)
		if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
			t.Errorf("checkAgenticIAMVaultVersion(%q) = %v, want %v", tc.version, err, tc.want)
		}
	}
}

func TestAgenticIAMRedeployHints(t *testing.T) {
	if got := agenticIAMSuggestedEntTag("2.1.2+ent"); got != "2.1.2-ent" {
		t.Errorf("a licensed-version fix must keep the running tag, got %q", got)
	}
	for _, v := range []string{"2.0.4", "2.0.4+ent"} {
		if got := agenticIAMSuggestedEntTag(v); !vaultVersionAtLeast(got, agenticIAMMinVaultVersion) || !strings.HasSuffix(got, "-ent") {
			t.Errorf("agenticIAMSuggestedEntTag(%q) = %q, want an Enterprise tag >= %s", v, got, agenticIAMMinVaultVersion)
		}
	}
	if got := agenticIAMRedeployCommand("2.1.1+ent", false); got != "hal vault update --edition ent --vault-tag 2.1.1-ent" {
		t.Errorf("dev redeploy = %q", got)
	}
	if got := agenticIAMRedeployCommand("2.1.1+ent", true); got != "hal vault update --mode prod --vault-tag 2.1.1-ent" {
		t.Errorf("prod redeploy = %q", got)
	}
}

func TestProbeOAuthResourceServerFeature(t *testing.T) {
	cases := []struct {
		name         string
		resp         fakeVaultResponse
		wantErr      bool
		wantLicensed bool // false: the license error
	}{
		{"no profile yet", fakeVaultResponse{Status: http.StatusNotFound, Body: `{"errors":[]}`}, false, true},
		{"empty 404", fakeVaultResponse{Status: http.StatusNotFound}, false, true},
		{"profiles listed", ok(`{"data":{"keys":["authentik"]}}`), false, true},
		{"feature not enabled", fakeVaultResponse{Status: http.StatusUnauthorized, Body: featureNotEnabledBody}, true, false},
		{"other 401", fakeVaultResponse{Status: http.StatusUnauthorized, Body: `{"errors":["token expired"]}`}, true, true},
		{"permission denied", fakeVaultResponse{Status: http.StatusForbidden, Body: `{"errors":["permission denied"]}`}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, client := newFakeVault(t, map[string]fakeVaultResponse{probeKey: tc.resp})
			err := probeOAuthResourceServerFeature(client, "2.1.1+ent", false)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if got := errors.Is(err, errAgenticIAMNotLicensed); got == tc.wantLicensed {
				t.Errorf("errors.Is(err, errAgenticIAMNotLicensed) = %v for %v", got, err)
			}
		})
	}
}

func TestLicenseErrorIsActionable(t *testing.T) {
	_, client := newFakeVault(t, map[string]fakeVaultResponse{
		probeKey: {Status: http.StatusUnauthorized, Body: featureNotEnabledBody},
	})
	msg := probeOAuthResourceServerFeature(client, "2.1.1+ent", false).Error()
	for _, want := range []string{"Agentic IAM", "Feature Not Enabled", "VAULT_LICENSE_PATH", "unset VAULT_LICENSE", "hal vault update --edition ent --vault-tag 2.1.1-ent", "hal vault agentic-iam enable"} {
		if !strings.Contains(msg, want) {
			t.Errorf("license error misses %q:\n%s", want, msg)
		}
	}
}

func TestAgenticIAMPrerequisites(t *testing.T) {
	cases := []struct {
		name   string
		routes map[string]fakeVaultResponse
		want   error // nil: passes
	}{
		{"community edition", map[string]fakeVaultResponse{healthKey: healthBody("2.1.1")}, errAgenticIAMNotEnterprise},
		{"enterprise too old", map[string]fakeVaultResponse{healthKey: healthBody("2.0.4+ent")}, errAgenticIAMVaultTooOld},
		{"no agent registry", map[string]fakeVaultResponse{
			healthKey: healthBody("2.1.1+ent"),
			mountsKey: ok(`{"data":{"sys/":{"type":"system"}}}`),
		}, errAgentRegistryMissing},
		{"not licensed", map[string]fakeVaultResponse{
			healthKey: healthBody("2.1.1+ent"),
			mountsKey: mountsWithRegistry,
			probeKey:  {Status: http.StatusUnauthorized, Body: featureNotEnabledBody},
		}, errAgenticIAMNotLicensed},
		{"ready", map[string]fakeVaultResponse{
			healthKey: healthBody("2.1.1+ent"),
			mountsKey: mountsWithRegistry,
		}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fv, client := newFakeVault(t, tc.routes)
			err := checkAgenticIAMPrerequisites(client, false)
			if tc.want == nil && err != nil {
				t.Fatalf("want success, got %v", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
			for _, k := range fv.keys() {
				if !strings.HasPrefix(k, "GET ") && !strings.HasPrefix(k, "LIST ") {
					t.Errorf("the probe must only read, but sent %s", k)
				}
			}
		})
	}
}
