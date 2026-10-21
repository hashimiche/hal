package labsecrets

import "fmt"

// The active tree is what the credential surfaces show: a section per running
// product, and a subsection per scenario enabled on it. Nothing that is down
// has a section, so `hal vault create` shows one Vault block and each
// `hal vault <scenario> enable` adds a block underneath it.
//
// BuildTree is pure: Facts say what is running, Values carry the secrets a lab
// generated, and everything else comes from the catalog. The engine lookups
// that fill Facts live in internal/labsecrets/live.

// Section keys. A product status command filters the tree by one of these.
const (
	SectionVault     = "vault"
	SectionTFE       = "tfe"
	SectionTFETwin   = "tfe-twin"
	SectionGitLab    = "gitlab"
	SectionAuthentik = "authentik"
)

// Entry is one credential: a login pair, a single labelled value, or a note
// pointing at the command that reads a secret HAL cannot show.
type Entry struct {
	// Name labels a single value, as in "Root token" or "API token". Empty for
	// a demo login, which prints as "user / secret".
	Name string `json:"name,omitempty"`
	// Username and Secret are the login pair.
	Username string `json:"username,omitempty"`
	Secret   string `json:"secret,omitempty"`
	// Note explains the entry, or stands in for a secret HAL cannot show.
	Note string `json:"note,omitempty"`
}

// Section is a running product, shared service, or scenario.
type Section struct {
	Key      string   `json:"service"`
	Icon     string   `json:"-"`
	Label    string   `json:"label"`
	URL      string   `json:"url,omitempty"`
	Entries  []Entry  `json:"entries,omitempty"`
	Commands []string `json:"commands,omitempty"`
	// Sections are the scenarios enabled on this product.
	Sections []Section `json:"scenarios,omitempty"`
}

// Tree is the whole active credential surface.
type Tree struct {
	Sections []Section
}

// Active reports whether anything is running.
func (t Tree) Active() bool { return len(t.Sections) > 0 }

// Section returns the top-level section with the given key.
func (t Tree) Section(key string) (Section, bool) {
	for _, s := range t.Sections {
		if s.Key == key {
			return s, true
		}
	}
	return Section{}, false
}

// Facts is what the engine says is running. Every field is a plain observation
// so the tree can be built and tested without an engine.
type Facts struct {
	VaultRunning bool
	// VaultProd is a `--mode prod` Vault: it generated its own root token and
	// unseal key instead of using the dev token.
	VaultProd bool

	VaultOIDCEnabled       bool
	VaultAgenticIAMRunning bool
	VaultAgenticIAMURL     string
	VaultLDAPRunning       bool
	VaultLDAPAdminURL      string
	VaultUserPassEnabled   bool
	VaultDatabaseActive    bool
	VaultOSRunning         bool
	VaultAAPRunning        bool
	VaultAAPURL            string

	// GitLab and Authentik are shared: each sits under the products that
	// registered as consumers, and stands alone when it runs for no one.
	GitLabRunning      bool
	GitLabURL          string
	GitLabUnderVault   bool
	GitLabUnderTFE     bool
	GitLabUnderTFETwin bool

	AuthentikRunning      bool
	AuthentikURL          string
	AuthentikUnderVault   bool
	AuthentikUnderTFE     bool
	AuthentikUnderTFETwin bool

	TFERunning     bool
	TFEURL         string
	TFETwinRunning bool
	TFETwinURL     string
}

// BuildTree renders the active tree from what is running and what each lab
// generated. Values win over the catalog, so a flag-overridden password or a
// generated token is what the operator is shown.
func BuildTree(f Facts, v Values) Tree {
	var tree Tree

	if f.VaultRunning {
		tree.Sections = append(tree.Sections, vaultSection(f, v))
	}
	if f.TFERunning {
		tree.Sections = append(tree.Sections, tfeSection(f, v, false))
	}
	if f.TFETwinRunning {
		tree.Sections = append(tree.Sections, tfeSection(f, v, true))
	}

	// A shared service nobody registered for still has a login to show.
	if f.GitLabRunning && !f.GitLabUnderVault && !f.GitLabUnderTFE && !f.GitLabUnderTFETwin {
		tree.Sections = append(tree.Sections, gitLabSection(f, v))
	}
	if f.AuthentikRunning && !f.AuthentikUnderVault && !f.AuthentikUnderTFE && !f.AuthentikUnderTFETwin {
		tree.Sections = append(tree.Sections, authentikSection(f, v))
	}

	return tree
}

func vaultSection(f Facts, v Values) Section {
	vault := Section{Key: SectionVault, Icon: "🔐", Label: "Vault", URL: "http://vault.localhost:8200"}

	if f.VaultProd {
		vault.Label = "Vault (Enterprise, prod)"
		vault.URL = "https://vault.localhost:8200"
		rootToken := v.Get(KeyVaultRootToken)
		vault.Entries = append(vault.Entries, Entry{Name: "Root token", Secret: rootToken, Note: missingNote(rootToken, "generated at init")})
		if unseal := v.Get(KeyVaultUnsealKey); unseal != "" {
			vault.Entries = append(vault.Entries, Entry{Name: "Unseal key", Secret: unseal})
		}
		vault.Commands = []string{
			"export VAULT_ADDR='https://vault.localhost:8200'",
			fmt.Sprintf("export VAULT_TOKEN='%s'", rootToken),
		}
		if caCert := v.Get(KeyVaultCACert); caCert != "" {
			vault.Commands = append(vault.Commands, fmt.Sprintf("export VAULT_CACERT='%s'", caCert))
		}
	} else {
		vault.Entries = append(vault.Entries, Entry{Name: "Root token", Secret: VaultDevRootToken})
		vault.Commands = []string{
			"export VAULT_ADDR='http://vault.localhost:8200'",
			fmt.Sprintf("export VAULT_TOKEN='%s'", VaultDevRootToken),
		}
	}

	if f.VaultOIDCEnabled {
		vault.Sections = append(vault.Sections, Section{
			Key: "vault-oidc", Icon: "🔑", Label: "OIDC — demo users",
			Entries: []Entry{
				{Username: VaultOIDCUserAlice, Secret: AuthentikPersonaPassword, Note: "Vault policy: admin (all paths)"},
				{Username: VaultOIDCUserBob, Secret: AuthentikPersonaPassword, Note: "Vault policy: user-ro (kv-oidc/team1 read)"},
			},
			Commands: []string{"vault login -method=oidc"},
		})
	}

	if f.AuthentikRunning && f.AuthentikUnderVault {
		vault.Sections = append(vault.Sections, authentikSection(f, v))
	}

	if f.VaultAgenticIAMRunning {
		vault.Sections = append(vault.Sections, Section{
			Key: "vault-agentic-iam", Icon: "🤖", Label: "Agentic IAM lab — personas (log in to the chat)",
			URL: f.VaultAgenticIAMURL,
			Entries: []Entry{
				{Username: "alice", Secret: AuthentikPersonaPassword, Note: "finance: may delegate; reads results, forecasts and payroll"},
				{Username: "bob", Secret: AuthentikPersonaPassword, Note: "engineering: may delegate; no rights on the lab's data"},
				{Username: "charlie", Secret: AuthentikPersonaPassword, Note: "sales: may not delegate to the demo agent"},
			},
		})
	}

	if f.VaultLDAPRunning {
		// Enable ends with ldap/rotate-root and hands the service accounts to
		// Vault, so only the two demo users stay typeable.
		vault.Sections = append(vault.Sections, Section{
			Key: "vault-ldap", Icon: "📂", Label: "LDAP — demo users", URL: f.VaultLDAPAdminURL,
			Entries: []Entry{
				{Username: LDAPUserAlice, Secret: LDAPUserPassword, Note: "group: admin"},
				{Username: LDAPUserBob, Secret: LDAPUserPassword, Note: "group: readers"},
				{Name: LDAPAdminDN, Note: "rotated by Vault at enable; not recoverable"},
				{Name: "app-service", Note: "read it: vault read ldap/static-cred/static-app"},
				{Name: "dev-pool", Note: "check one out: vault write -f ldap/library/dev-pool/check-out"},
			},
			Commands: []string{
				fmt.Sprintf("vault login -method=ldap username=%s password=%s", LDAPUserBob, LDAPUserPassword),
			},
		})
	}

	if f.VaultUserPassEnabled {
		vault.Sections = append(vault.Sections, Section{
			Key: "vault-userpass", Icon: "👤", Label: "UserPass — demo user",
			Entries: []Entry{{Username: VaultUserPassUsername, Secret: VaultUserPassPassword}},
			Commands: []string{
				fmt.Sprintf("vault login -method=userpass username=%s password=%s", VaultUserPassUsername, VaultUserPassPassword),
			},
		})
	}

	if f.VaultDatabaseActive {
		vault.Sections = append(vault.Sections, Section{
			Key: "vault-database", Icon: "🗄️", Label: "Database — dynamic credentials",
			Commands: []string{"vault read database/creds/dba-role"},
		})
	}

	if f.VaultOSRunning {
		// Vault rotates demouser and appadmin during enable; mgmt-user is the
		// parent account it logs in with, so that password survives.
		vault.Sections = append(vault.Sections, Section{
			Key: "vault-os", Icon: "🖥️", Label: "OS — demo VM accounts",
			Entries: []Entry{
				{Username: VaultOSMgmtUser, Secret: VaultOSMgmtPassword, Note: "parent account Vault rotates the others with"},
				{Name: VaultOSDemoUser, Note: "read it: vault read os/hosts/demo-vm/accounts/demouser/creds"},
				{Name: VaultOSAppAdmin, Note: "read it: vault read os/hosts/demo-vm/accounts/appadmin/creds"},
			},
		})
	}

	if f.VaultAAPRunning {
		vault.Sections = append(vault.Sections, Section{
			Key: "vault-aap", Icon: "🅰️", Label: "AAP controller", URL: f.VaultAAPURL,
			Entries: []Entry{{Username: AAPUsername, Secret: v.Or(KeyAAPPassword, AAPPassword)}},
		})
	}

	if f.GitLabRunning && f.GitLabUnderVault {
		vault.Sections = append(vault.Sections, gitLabSection(f, v))
	}

	return vault
}

func tfeSection(f Facts, v Values, twin bool) Section {
	tfe := Section{Key: SectionTFE, Icon: "🏗️", Label: "Terraform Enterprise", URL: f.TFEURL}
	passwordKey, gitLabHere, authentikHere := KeyTFEAdminPassword, f.GitLabUnderTFE, f.AuthentikUnderTFE
	if twin {
		tfe.Key, tfe.Label, tfe.URL = SectionTFETwin, "Terraform Enterprise (twin)", f.TFETwinURL
		passwordKey, gitLabHere, authentikHere = KeyTFETwinAdminPassword, f.GitLabUnderTFETwin, f.AuthentikUnderTFETwin
	}

	tfe.Entries = append(tfe.Entries, Entry{
		Name:     "Admin",
		Username: TFEAdminUsername,
		Secret:   v.Or(passwordKey, TFEAdminPassword),
	})

	// The twin shares the primary's API token cache; there is one token.
	token := v.Get(KeyTFEAPIToken)
	tfe.Entries = append(tfe.Entries, Entry{
		Name:   "API token",
		Secret: token,
		Note:   missingNote(token, "run `hal terraform status` to retrieve"),
	})

	if f.GitLabRunning && gitLabHere {
		tfe.Sections = append(tfe.Sections, gitLabSection(f, v))
	}
	if f.AuthentikRunning && authentikHere {
		tfe.Sections = append(tfe.Sections, authentikSection(f, v))
	}

	return tfe
}

func gitLabSection(f Facts, v Values) Section {
	return Section{
		Key: SectionGitLab, Icon: "🦊", Label: "GitLab", URL: f.GitLabURL,
		Entries: []Entry{{
			Name:     "Root",
			Username: GitLabRootUsername,
			Secret:   v.Or(KeyGitLabRootPassword, GitLabRootPassword),
		}},
	}
}

func authentikSection(f Facts, v Values) Section {
	password := v.Get(KeyAuthentikAdminPassword)
	return Section{
		Key: SectionAuthentik, Icon: "🌐", Label: "Authentik IdP", URL: f.AuthentikURL,
		Entries: []Entry{{
			Name:     "Admin",
			Username: AuthentikAdminUsername,
			Secret:   password,
			Note:     missingNote(password, "generated at enable"),
		}},
	}
}

// missingNote explains an absent secret instead of printing an empty field.
func missingNote(secret, note string) string {
	if secret != "" {
		return ""
	}
	return note
}
