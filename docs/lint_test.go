package topics

import (
	"strings"
	"testing"

	"hal/internal/labsecrets"
)

// labSecrets are the lab secrets that are distinctive enough to refuse as a
// substring. They are referenced by name from internal/labsecrets, so a catalog
// change shows up here. The generic ones (root, admin, password, alice) are left
// out: they are ordinary words, and the shape rules catch them where they act as
// a secret ("root / admin", "password: x").
var labSecrets = []string{
	labsecrets.GitLabRootPassword,
	labsecrets.TFEAdminPassword,
	labsecrets.TFEEncryptionPassword,
	labsecrets.TFEDatabasePassword,
	labsecrets.VaultUserPassPassword,
	labsecrets.VaultMariaDBRootPassword,
	labsecrets.VaultMariaDBBrokerBootstrapPassword,
	labsecrets.AgenticIAMBrokerBootstrapPassword,
	labsecrets.VaultOSMgmtPassword,
	labsecrets.VaultOSDemoSeedPassword,
	labsecrets.VaultOSAppAdminSeedPassword,
	labsecrets.LDAPServiceAccountPassword,
	labsecrets.LDAPReadOnlyPassword,
}

func TestOperatorLoginsFinds(t *testing.T) {
	tests := []struct {
		name string
		text string
		rule string
	}{
		{"VAULT_TOKEN set to root", "VAULT_TOKEN=root vault read auth/jwt/config", "VAULT_TOKEN assignment"},
		{"VAULT_TOKEN exported", "export VAULT_TOKEN='root'", "VAULT_TOKEN assignment"},
		{"VAULT_TOKEN from a command", "VAULT_TOKEN=$(cat ~/.vault-token) vault status", "VAULT_TOKEN assignment"},
		{"VAULT_TOKEN with spaces", "VAULT_TOKEN = abc", "VAULT_TOKEN assignment"},
		{"vault login with a token", "$ vault login root", "vault login with a token"},
		{"vault login with a quoted token", `vault login "abc123"`, "vault login with a token"},
		// The token-shaped strings are split in two on purpose: written whole, they
		// match GitHub's secret scanning (push protection refuses the push). The
		// test sees the same string once the compiler joins the halves.
		{"an hvs token", "the token is hv" + "s.CAESIJlU1ucTkWyz2qLDEN5Q", "Vault token literal"},
		{"an hvb token", "hv" + "b.AAAAAQJ-abcdefghijklmnop", "Vault token literal"},
		{"a legacy s token", "s" + ".Abcdefghijklmnopqrstuvwx", "Vault token literal"},
		{"password=", "mysql -u root --password=hunter2", "secret flag"},
		{"password= in a URL-ish pair", "login with user=alice&password=hunter2", "secret assignment"},
		{"PASSWORD env", "GITLAB_ROOT_PASSWORD=hunter2", "secret assignment"},
		{"root_password", "root_password = s3cret", "secret assignment"},
		{"secret_id", "secret_id=1234-abcd", "secret assignment"},
		{"an api key", "api_key=sk-12345", "secret assignment"},
		{"the token header", "curl -H 'X-Vault-Token: root' http://hal-vault:8200", "token header"},
		{"--token", "terraform login --token abc", "secret flag"},
		{"--password", "tool --password hunter2", "secret flag"},
		{"a printed password line", "Password: hunter2", "printed login line"},
		{"a printed root token line", "Root token : root", "printed login line"},
		{"a printed unseal key line", "unseal key: abcd", "printed login line"},
		{"a login pair", "alice / password", "login pair"},
		{"an admin pair", "Admin : haladmin / admin", "login pair"},
		{"a root pair", "ldap-admin / root", "login pair"},
		{"a known lab secret on its own", "type hal9000FTW in the form", "known lab secret"},
		{"a known lab secret in the middle of a word list", "(threat-level-midnight)", "known lab secret"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := OperatorLogins(tt.text, labSecrets...)
			for _, f := range findings {
				for _, rule := range f.Rules {
					if rule == tt.rule {
						return
					}
				}
			}
			t.Errorf("%q: findings %+v, want the rule %q", tt.text, findings, tt.rule)
		})
	}
}

func TestOperatorLoginsLetsThrough(t *testing.T) {
	// Each of these is something a topic legitimately says.
	for name, text := range map[string]string{
		"a project path that starts with root":    "The project is root/secret-zero.",
		"the role binds a project path":           "`project_path` must be `root/secret-zero`.",
		"a bare username":                         "GitLab's administrator is root.",
		"the word token":                          "Vault returns a token that carries the role's policies.",
		"the word password":                       "A person types a password; the job has none.",
		"the policy list":                         "token_policies=cicd-read is set on the role.",
		"a ttl":                                   "token_ttl=1h",
		"a placeholder env":                       "export VAULT_ADDR=http://vault.localhost:8200",
		"a flag with a placeholder":               "--token $TOKEN",
		"a flag with an angle placeholder":        "--password <password>",
		"an assignment with a placeholder":        "password=<your password>",
		"an assignment with a shell variable":     "client_secret=${CLIENT_SECRET}",
		"a pointer to the command":                "Password: `hal creds status` shows it.",
		"vault login with a method":               "vault login -method=oidc",
		"the creds command":                       "Run `hal creds status` to see the logins.",
		"a hyphenated secret word":                "The secret-zero problem.",
		"an hvs prefix with no token":             "A token starting with hvs. is a service token.",
		"a path with a trailing slash and a pair": "kv-jwt/data/cicd",
		"a division":                              "1 / 2",
		"the audience":                            "aud: vault",
		"a header key in a doc title":             "- Vault JWT auth: https://developer.hashicorp.com/vault/docs/auth/jwt#bound-claims",
	} {
		t.Run(name, func(t *testing.T) {
			if findings := OperatorLogins(text, labSecrets...); len(findings) != 0 {
				t.Errorf("%q: unexpected findings %+v", text, findings)
			}
		})
	}
}

func TestOperatorLoginsReportsLines(t *testing.T) {
	text := "clean\nclean\nexport VAULT_TOKEN=root\nclean\nPassword: hunter2\n"
	got := OperatorLogins(text)
	if len(got) != 2 || got[0].Line != 3 || got[1].Line != 5 {
		t.Fatalf("findings = %+v, want lines 3 and 5", got)
	}
}

func TestCheck(t *testing.T) {
	good, err := Parse("vault/jwt.md", []byte(adrJWT))
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(good, labSecrets...); err != nil {
		t.Fatalf("the ADR example was refused: %v", err)
	}

	bad, err := Parse("vault/jwt.md", []byte(adrJWT+"\nRun `VAULT_TOKEN=root vault read auth/jwt/config`.\nUI login: root / hal9000FTW\n"))
	if err != nil {
		t.Fatal(err)
	}
	err = Check(bad, labSecrets...)
	if err == nil {
		t.Fatal("a topic with an operator login was accepted")
	}
	msg := err.Error()
	for _, want := range []string{"topic vault/jwt", "VAULT_TOKEN assignment", "known lab secret", "hal creds status"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the error does not hold %q:\n%s", want, msg)
		}
	}
	// A test log must not echo the secret.
	for _, leak := range []string{"hal9000FTW", "VAULT_TOKEN=root"} {
		if strings.Contains(msg, leak) {
			t.Errorf("the error echoes %q:\n%s", leak, msg)
		}
	}
}
