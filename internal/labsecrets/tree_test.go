package labsecrets

import (
	"bytes"
	"strings"
	"testing"
)

// keys returns the section keys of a tree level, so a test can state the shape
// it expects without walking the structs.
func keys(sections []Section) []string {
	out := make([]string, 0, len(sections))
	for _, s := range sections {
		out = append(out, s.Key)
	}
	return out
}

func nested(t *testing.T, tree Tree, key string) Section {
	t.Helper()
	section, ok := tree.Section(key)
	if !ok {
		t.Fatalf("tree has no %q section, got %v", key, keys(tree.Sections))
	}
	return section
}

func render(tree Tree) string {
	var buf bytes.Buffer
	Render(&buf, tree)
	return buf.String()
}

func TestNothingRunningHasNoSections(t *testing.T) {
	tree := BuildTree(Facts{}, Values{})
	if tree.Active() {
		t.Fatalf("expected an empty tree, got %v", keys(tree.Sections))
	}
}

func TestVaultAloneShowsOnlyTheDevToken(t *testing.T) {
	tree := BuildTree(Facts{VaultRunning: true}, Values{})

	if got := keys(tree.Sections); len(got) != 1 || got[0] != SectionVault {
		t.Fatalf("expected a lone Vault section, got %v", got)
	}
	vault := nested(t, tree, SectionVault)
	if len(vault.Sections) != 0 {
		t.Errorf("no scenario is enabled, but Vault has %v", keys(vault.Sections))
	}
	if got := vault.Entries[0].Secret; got != VaultDevRootToken {
		t.Errorf("root token = %q, want the dev token %q", got, VaultDevRootToken)
	}
}

func TestEnablingOIDCNestsItUnderVault(t *testing.T) {
	facts := Facts{
		VaultRunning:        true,
		VaultOIDCEnabled:    true,
		AuthentikRunning:    true,
		AuthentikUnderVault: true,
		AuthentikURL:        "http://authentik.localhost:9100/if/admin/",
	}
	tree := BuildTree(facts, Values{KeyAuthentikAdminPassword: "generated-pw"})

	if got := keys(tree.Sections); len(got) != 1 || got[0] != SectionVault {
		t.Fatalf("Authentik should sit under Vault, not beside it: %v", got)
	}
	vault := nested(t, tree, SectionVault)
	if got := keys(vault.Sections); len(got) != 2 || got[0] != "vault-oidc" || got[1] != SectionAuthentik {
		t.Fatalf("expected OIDC then Authentik under Vault, got %v", got)
	}
	if got := vault.Sections[1].Entries[0].Secret; got != "generated-pw" {
		t.Errorf("Authentik password = %q, want the generated one", got)
	}
}

func TestProdVaultShowsTheGeneratedToken(t *testing.T) {
	values := Values{
		KeyVaultRootToken: "hvs.generated",
		KeyVaultUnsealKey: "unseal-key",
		KeyVaultCACert:    "/home/lab/.hal/vault-prod/certs/cert.pem",
	}
	vault := nested(t, BuildTree(Facts{VaultRunning: true, VaultProd: true}, values), SectionVault)

	if got := vault.Entries[0].Secret; got != "hvs.generated" {
		t.Errorf("root token = %q, want the generated one, not the dev token", got)
	}
	if got := vault.Entries[1].Secret; got != "unseal-key" {
		t.Errorf("unseal key = %q, want the generated one", got)
	}
	out := render(Tree{Sections: []Section{vault}})
	if !strings.Contains(out, "VAULT_CACERT='/home/lab/.hal/vault-prod/certs/cert.pem'") {
		t.Errorf("prod Vault must export its CA cert, got:\n%s", out)
	}
}

func TestProdVaultWithoutAStoredTokenExplainsItself(t *testing.T) {
	vault := nested(t, BuildTree(Facts{VaultRunning: true, VaultProd: true}, Values{}), SectionVault)
	out := render(Tree{Sections: []Section{vault}})

	if strings.Contains(out, "Root token : \n") {
		t.Errorf("an absent token must not print as an empty field, got:\n%s", out)
	}
	if !strings.Contains(out, "generated at init") {
		t.Errorf("expected a note explaining the absent token, got:\n%s", out)
	}
}

func TestGitLabSitsUnderTheProductThatStartedIt(t *testing.T) {
	underVault := BuildTree(Facts{
		VaultRunning:     true,
		GitLabRunning:    true,
		GitLabUnderVault: true,
		GitLabURL:        "http://gitlab.localhost:8080",
	}, Values{})
	if got := keys(underVault.Sections); len(got) != 1 || got[0] != SectionVault {
		t.Fatalf("GitLab started by vault jwt belongs under Vault, got %v", got)
	}
	if got := keys(nested(t, underVault, SectionVault).Sections); len(got) != 1 || got[0] != SectionGitLab {
		t.Fatalf("expected GitLab under Vault, got %v", got)
	}

	underTFE := BuildTree(Facts{
		TFERunning:     true,
		GitLabRunning:  true,
		GitLabUnderTFE: true,
	}, Values{})
	if got := keys(nested(t, underTFE, SectionTFE).Sections); len(got) != 1 || got[0] != SectionGitLab {
		t.Fatalf("expected GitLab under Terraform, got %v", got)
	}
}

func TestGitLabIsPrintedUnderEveryProductUsingIt(t *testing.T) {
	tree := BuildTree(Facts{
		VaultRunning:       true,
		TFERunning:         true,
		TFETwinRunning:     true,
		GitLabRunning:      true,
		GitLabUnderVault:   true,
		GitLabUnderTFE:     true,
		GitLabUnderTFETwin: true,
	}, Values{})

	for _, product := range []string{SectionVault, SectionTFE, SectionTFETwin} {
		if got := keys(nested(t, tree, product).Sections); len(got) != 1 || got[0] != SectionGitLab {
			t.Errorf("%s should carry the GitLab login, got %v", product, got)
		}
	}
	if _, standalone := tree.Section(SectionGitLab); standalone {
		t.Error("GitLab has consumers, so it must not also stand on its own")
	}
}

func TestSharedServiceWithNoConsumerStandsOnItsOwn(t *testing.T) {
	tree := BuildTree(Facts{GitLabRunning: true, GitLabURL: "http://gitlab.localhost:8080"}, Values{})

	gitlab := nested(t, tree, SectionGitLab)
	if gitlab.Entries[0].Username != GitLabRootUsername || gitlab.Entries[0].Secret != GitLabRootPassword {
		t.Errorf("expected the catalog root login, got %+v", gitlab.Entries[0])
	}
}

func TestAnOverriddenPasswordWinsOverTheCatalog(t *testing.T) {
	values := Values{
		KeyGitLabRootPassword: "chosen-at-boot",
		KeyTFEAdminPassword:   "tfe-chosen-at-boot",
	}
	tree := BuildTree(Facts{TFERunning: true, GitLabRunning: true, GitLabUnderTFE: true}, values)

	tfe := nested(t, tree, SectionTFE)
	if got := tfe.Entries[0].Secret; got != "tfe-chosen-at-boot" {
		t.Errorf("TFE admin password = %q, want the overridden one", got)
	}
	if got := tfe.Sections[0].Entries[0].Secret; got != "chosen-at-boot" {
		t.Errorf("GitLab password = %q, want the overridden one", got)
	}
}

func TestPrimaryAndTwinAreSeparateSections(t *testing.T) {
	tree := BuildTree(Facts{
		TFERunning:     true,
		TFEURL:         "https://tfe.localhost",
		TFETwinRunning: true,
		TFETwinURL:     "https://tfe-bis.localhost",
	}, Values{KeyTFETwinAdminPassword: "twin-only"})

	if got := nested(t, tree, SectionTFE).Entries[0].Secret; got != TFEAdminPassword {
		t.Errorf("primary password = %q, want the catalog one", got)
	}
	if got := nested(t, tree, SectionTFETwin).Entries[0].Secret; got != "twin-only" {
		t.Errorf("twin password = %q, want the twin's own", got)
	}
}

func TestContainerOnlyPasswordsAreNeverShown(t *testing.T) {
	// Every lab is up, so anything printable is printed.
	facts := Facts{
		VaultRunning: true, VaultProd: true, VaultOIDCEnabled: true,
		VaultAgenticIAMRunning: true, VaultLDAPRunning: true, VaultUserPassEnabled: true,
		VaultDatabaseActive: true, VaultOSRunning: true, VaultAAPRunning: true,
		GitLabRunning: true, GitLabUnderVault: true,
		AuthentikRunning: true, AuthentikUnderVault: true,
		TFERunning: true, TFETwinRunning: true,
	}
	out := render(BuildTree(facts, Values{KeyVaultRootToken: "hvs.generated"}))

	for _, secret := range []string{
		TFEDatabasePassword,
		TFEEncryptionPassword,
		VaultMariaDBRootPassword,
		VaultMariaDBBrokerBootstrapPassword,
		AgenticIAMBrokerBootstrapPassword,
		// LDAPAdminSeedPassword is the bare word "admin", which legitimately
		// appears elsewhere; the LDAP admin line is asserted below instead.
		LDAPServiceAccountPassword,
		VaultOSDemoSeedPassword,
		VaultOSAppAdminSeedPassword,
	} {
		if strings.Contains(out, secret) {
			t.Errorf("%q is a container-only or rotated secret and must not be shown:\n%s", secret, out)
		}
	}
}

func TestRotatedSecretsAreShownAsTheCommandThatReadsThem(t *testing.T) {
	facts := Facts{VaultRunning: true, VaultLDAPRunning: true, VaultOSRunning: true}
	out := render(BuildTree(facts, Values{}))

	for _, want := range []string{
		"vault read ldap/static-cred/static-app",
		"vault read os/hosts/demo-vm/accounts/demouser/creds",
		"rotated by Vault at enable; not recoverable",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in the output, got:\n%s", want, out)
		}
	}
	if !strings.Contains(out, VaultOSMgmtPassword) {
		t.Errorf("mgmt-user survives enable, so its password must be shown:\n%s", out)
	}
	if strings.Contains(out, LDAPAdminDN+" : "+LDAPAdminSeedPassword) {
		t.Errorf("the rotated directory admin password must not be shown:\n%s", out)
	}
}

func TestNoLicenceIsEverRendered(t *testing.T) {
	facts := Facts{VaultRunning: true, VaultProd: true, TFERunning: true, TFETwinRunning: true}
	// A licence would only reach the tree through the runtime store, which
	// never holds one; prove the render carries no such field regardless.
	out := strings.ToLower(render(BuildTree(facts, Values{"VAULT_LICENSE": "leaked", "TFE_LICENSE": "leaked"})))

	for _, forbidden := range []string{"licen", "leaked"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("the credential tree must never carry a licence, found %q in:\n%s", forbidden, out)
		}
	}
}

func TestRenderSectionFiltersToOneProduct(t *testing.T) {
	tree := BuildTree(Facts{VaultRunning: true, TFERunning: true}, Values{})

	var buf bytes.Buffer
	if !RenderSection(&buf, tree, SectionVault) {
		t.Fatal("Vault is running, so its section must render")
	}
	if strings.Contains(buf.String(), "Terraform") {
		t.Errorf("hal vault status must not print Terraform credentials:\n%s", buf.String())
	}

	buf.Reset()
	if RenderSection(&buf, tree, SectionTFETwin) {
		t.Error("the twin is down, so nothing should render for it")
	}
}
