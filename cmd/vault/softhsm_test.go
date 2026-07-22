package vault

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	vault "github.com/hashicorp/vault/api"
)

func TestIsEnterpriseEdition(t *testing.T) {
	tests := map[string]bool{
		"ce":         false,
		"ent":        true,
		"enterprise": true,
		"ent-hsm":    true,
		"unknown":    false,
	}
	for edition, want := range tests {
		t.Run(edition, func(t *testing.T) {
			if got := isEnterpriseEdition(edition); got != want {
				t.Fatalf("isEnterpriseEdition(%q) = %v, want %v", edition, got, want)
			}
		})
	}
}

func TestIsHSMTag(t *testing.T) {
	tests := map[string]bool{
		"2.0.3-ent.hsm": true,
		"2.0.3-ent":     false,
		"2.0.3":         false,
		"":              false,
	}
	for tag, want := range tests {
		t.Run(tag, func(t *testing.T) {
			if got := isHSMTag(tag); got != want {
				t.Fatalf("isHSMTag(%q) = %v, want %v", tag, got, want)
			}
		})
	}
}

func TestIsVaultHSMBuild(t *testing.T) {
	for _, tc := range []struct {
		version string
		want    bool
	}{
		{version: "2.0.3+ent.hsm", want: true},
		{version: "2.0.3+ent", want: false},
		{version: "2.0.3", want: false},
	} {
		t.Run(tc.version, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"initialized":true,"sealed":false,"version":%q}`, tc.version)
			}))
			defer server.Close()

			config := vault.DefaultConfig()
			config.Address = server.URL
			client, err := vault.NewClient(config)
			if err != nil {
				t.Fatalf("vault.NewClient: %v", err)
			}
			if got := isVaultHSMBuild(client); got != tc.want {
				t.Fatalf("isVaultHSMBuild(%q) = %v, want %v", tc.version, got, tc.want)
			}
		})
	}
}

func TestVaultHSMProdConfigHCL(t *testing.T) {
	config := vaultHSMProdConfigHCL()
	for _, want := range []string{
		`storage "raft"`,
		`kms_library "pkcs11"`,
		`name    = "softhsm"`,
		`library = "` + softHSMLibPath + `"`,
	} {
		if !strings.Contains(config, want) {
			t.Errorf("HSM prod config missing %q", want)
		}
	}
}
