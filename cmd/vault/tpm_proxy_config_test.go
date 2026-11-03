package vault

import (
	"strings"
	"testing"
)

func TestVaultSupportsTPMAuth(t *testing.T) {
	cases := []struct {
		version string
		want    bool
	}{
		{version: "2.2.0-rc1+ent", want: true},
		{version: "2.2.0+ent", want: true},
		{version: "2.3.1+ent", want: true},
		{version: "2.1.2-ent", want: false},
		{version: "2.2.0", want: false},
		{version: "1.19.5+ent", want: false},
		{version: "", want: false},
	}
	for _, tc := range cases {
		if got := vaultSupportsTPMAuth(tc.version); got != tc.want {
			t.Errorf("vaultSupportsTPMAuth(%q) = %v, want %v", tc.version, got, tc.want)
		}
	}
}

func TestVaultTPMProxyConfig(t *testing.T) {
	cfg := vaultTPMProxyConfig(
		"demo",
		"https://127.0.0.1:8200",
		"/var/lib/vault-agent/demo/tls/vault-ca.pem",
		"/run/swtpm/swtpm.sock",
		"hal-vault-tpm",
	)
	for _, want := range []string{
		`address = "https://127.0.0.1:8200"`,
		`ca_cert = "/var/lib/vault-agent/demo/tls/vault-ca.pem"`,
		`tpm_cert "host_identity"`,
		`mount_path = "auth/tpm"`,
		`role_name  = "demo-app"`,
		`device_path = "/run/swtpm/swtpm.sock"`,
		`CN = "hal-vault-tpm"`,
		`path  = "/var/lib/vault-agent/demo/tpm"`,
		`method "tpm"`,
		`tpm_cert  = "host_identity"`,
		`use_auto_auth_token = "force"`,
		`address      = "/run/vault-proxy/demo.sock"`,
		`socket_user  = "demo"`,
		`tls_disable  = true`,
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config missing %q\n%s", want, cfg)
		}
	}
}

func TestTPMProxyGuestSetupScript(t *testing.T) {
	script := tpmProxyGuestSetupScript("demo", "2.2.0-rc1+ent")
	for _, want := range []string{
		"User=demo",
		"path=/run/swtpm/swtpm.sock",
		"ver=\"2.2.0-rc1+ent\"",
		"/var/lib/vault-cli/demo/tpm",
		"/etc/apparmor.d/disable/usr.bin.swtpm",
		"TCP:${gw}:8200",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("setup script missing %q", want)
		}
	}
	if strings.Contains(script, "%[") {
		t.Fatal("setup script still has an unsubstituted format verb")
	}
}

func TestValidateVaultTPMApp(t *testing.T) {
	if err := validateVaultTPMApp("demo"); err != nil {
		t.Fatal(err)
	}
	if err := validateVaultTPMApp("Demo"); err == nil {
		t.Fatal("expected mixed-case app to be rejected")
	}
	if err := validateVaultTPMApp(""); err == nil {
		t.Fatal("expected empty app to be rejected")
	}
}
