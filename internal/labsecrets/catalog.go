// Package labsecrets is the catalog of every fixed lab secret HAL uses, the
// store for the ones it generates at runtime, and the tree of credentials the
// `status` surfaces print.
//
// Rule: a user, password, token or any other lab secret is declared here once
// and referenced by name — never written as a literal in a command file. The
// products boot from these names, and `hal creds status` reads the same names,
// so the lab can never show a credential that differs from the one it booted
// with. Values that a lab generates at runtime (a prod Vault root token, the
// Authentik admin password, the TFE API token) live in the runtime store
// instead; see store.go.
//
// Passwords deliberately differ between labs: GitLab and TFE both refuse a weak
// one, while the Vault demo users are meant to be typed from memory. Each value
// below stands on its own — do not collapse them into a single shared constant.
// See docs/adr/0005-lab-secret-catalog.md.
//
// A HashiCorp product license is not a lab secret: it is never stored here and
// never printed by any surface.
package labsecrets

// --- Vault core -------------------------------------------------------------

const (
	// VaultDevRootToken is the well-known token of a `-dev` Vault. A prod Vault
	// generates its own; that one lands in the runtime store under KeyVaultRootToken.
	VaultDevRootToken = "root"
)

// --- Vault OIDC (Authentik demo users) --------------------------------------

const (
	VaultOIDCUserAlice = "alice"
	VaultOIDCUserBob   = "bob"
	// AuthentikPersonaPassword is the password of every demo user and persona
	// HAL creates in Authentik, for OIDC and for the Agentic IAM lab alike.
	AuthentikPersonaPassword = "password"
)

// --- Vault LDAP (OpenLDAP seed) ---------------------------------------------

const (
	// LDAPAdminDN and LDAPAdminSeedPassword bootstrap the directory. `hal vault
	// ldap enable` ends with ldap/rotate-root, so the seed password stops being
	// valid the moment Vault owns the directory: it is a boot value, not an
	// operator login.
	LDAPAdminDN           = "cn=admin,dc=hal,dc=local"
	LDAPAdminSeedPassword = "admin"

	LDAPUserAlice    = "alice"
	LDAPUserBob      = "bob"
	LDAPUserPassword = "password"

	// Service accounts Vault manages after enable: the static role rotates
	// app-service, and the library checks out the lib-service pair.
	LDAPServiceAccountPassword = "initialpass"

	// Container-only: the directory's own read-only bind user.
	LDAPReadOnlyUser     = "vault-auth"
	LDAPReadOnlyPassword = "authpass"
)

// --- Vault UserPass ---------------------------------------------------------

const (
	VaultUserPassUsername = "michaelScott"
	VaultUserPassPassword = "threat-level-midnight"
)

// --- Vault Database (MariaDB backend) ---------------------------------------

const (
	// Container-only: MariaDB boots with this root password and keeps it for the
	// life of the container. It is never an operator login.
	VaultMariaDBRootPassword = "vaultroot"

	// Broker of `hal vault database`. Vault rotates the password right after the
	// connection is written; the bootstrap value is only used before that.
	VaultMariaDBBrokerUser              = "vaultadmin"
	VaultMariaDBBrokerBootstrapPassword = "temp-vault-pass"

	// Broker of the Agentic IAM lab, reset on every enable and rotated by Vault.
	AgenticIAMBrokerUser              = "agentic-iam-broker"
	AgenticIAMBrokerBootstrapPassword = "agentic-iam-temp-pass"
)

// --- Vault OS (Multipass demo VM) -------------------------------------------

const (
	// VaultOSMgmtUser is the privileged parent account: Vault SSHes in as this
	// user to rotate the others, so its password survives enable.
	VaultOSMgmtUser     = "mgmt-user"
	VaultOSMgmtPassword = "mgmt-password-789"

	// Target accounts. Vault rotates both during enable, so these seeds stop
	// being valid; read the live ones from os/hosts/demo-vm/accounts/<name>/creds.
	VaultOSDemoUser             = "demouser"
	VaultOSDemoSeedPassword     = "initial-password-123"
	VaultOSAppAdmin             = "appadmin"
	VaultOSAppAdminSeedPassword = "admin-password-456"
)

// --- Vault AAP --------------------------------------------------------------

const (
	AAPUsername = "admin"
	AAPPassword = "admin"
)

// --- Terraform Enterprise ---------------------------------------------------

const (
	TFEAdminUsername = "haladmin"
	TFEAdminEmail    = "haladmin@localhost"
	// TFE enforces a strong admin password. It reads like the GitLab one below
	// by coincidence of that shared constraint, not because one lab password is
	// reused: each may be changed without touching the other.
	TFEAdminPassword = "hal9000FTW"

	// Container-only credentials of the TFE backend services.
	TFEEncryptionPassword = "hal-secret-encryption-password"
	TFEDatabaseUser       = "tfe"
	TFEDatabasePassword   = "tfe_password"
	TFES3AccessKey        = "haladmin"
	TFES3SecretKey        = "hal9000FTW"
)

// --- GitLab (shared service) ------------------------------------------------

const (
	GitLabRootUsername = "root"
	// GitLab refuses a weak root password at boot.
	GitLabRootPassword = "hal9000FTW"
)

// --- Authentik (shared service) ---------------------------------------------

const (
	AuthentikAdminUsername = "akadmin"
)
