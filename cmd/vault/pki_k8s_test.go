package vault

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPKIK8sVaultHTTPS(t *testing.T) {
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	ca := []byte("test CA PEM")
	if err := os.WriteFile(caPath, ca, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VAULT_CACERT", caPath)
	server, name, bundle, err := pkiK8sVaultConnection("https://localhost:8200", "10.89.0.2")
	if err != nil {
		t.Fatal(err)
	}
	if server != "https://10.89.0.2:8200" || name != vaultContainer || bundle != base64.StdEncoding.EncodeToString(ca) {
		t.Fatalf("unexpected connection: %q %q %q", server, name, bundle)
	}
	manifest := buildPKIK8sCRDManifests(server, "pki-int", name, bundle)
	for _, want := range []string{`server: "https://10.89.0.2:8200"`, `serverName: "hal-vault"`, `caBundle: "` + bundle + `"`} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("missing %s", want)
		}
	}
}

func TestPKIK8sVaultHTTPAndRemote(t *testing.T) {
	for _, tc := range []struct{ address, server, name string }{
		{"http://127.0.0.1:8200", "http://10.89.0.2:8200", vaultContainer},
		{"http://vault.example:8200", "http://vault.example:8200", "vault.example"},
	} {
		server, name, bundle, err := pkiK8sVaultConnection(tc.address, "10.89.0.2")
		if err != nil || server != tc.server || name != tc.name || bundle != "" {
			t.Fatalf("%s: %q %q %q %v", tc.address, server, name, bundle, err)
		}
	}
}

func TestPKIK8sVaultMissingCA(t *testing.T) {
	t.Setenv("VAULT_CACERT", filepath.Join(t.TempDir(), "missing"))
	if _, _, _, err := pkiK8sVaultConnection("https://localhost:8200", "10.89.0.2"); err == nil {
		t.Fatal("expected missing CA error")
	}
}
