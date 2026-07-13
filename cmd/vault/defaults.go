package vault

// defaults.go is the single source of truth for values shared across the Vault
// command package. Container/volume names, the dev endpoint+token, ports and every
// image/tag flag default are declared once here so create, status, delete, audit,
// database, os, ldap, oidc and k8s can never drift apart. Cross-product values
// (the Docker network name) live in internal/global and are referenced, never
// redeclared.

const (
	// --- Containers / instances ---
	vaultContainer        = "hal-vault"
	vaultMariaDBContainer = "hal-vault-mariadb"
	vaultOSInstance       = "hal-vault-os" // Multipass VM
	openLDAPContainer     = "hal-openldap"
	phpLDAPAdminContainer = "hal-phpldapadmin"
	// Feature/demo containers spun up by individual auth-method commands
	// (referenced by status, delete, jwt, oidc and pki — kept here so they cannot drift).
	gitlabContainer       = "hal-gitlab"
	gitlabRunnerContainer = "hal-gitlab-runner"
	acmeDNSContainer      = "hal-acme-dns"

	// --- Volumes ---
	vaultLogsVolume    = "hal-vault-logs"
	vaultPluginsVolume = "hal-vault-plugins"
	vaultDataVolume    = "hal-vault-data"

	// --- Endpoints / token / port ---
	vaultHTTPPort    = 8200
	vaultRootToken   = "root"
	vaultLocalAPIURL = "http://127.0.0.1:8200"
	vaultPublicURL   = "http://vault.localhost:8200"
	vaultHealthURL   = "http://vault.localhost:8200/v1/sys/health"

	// --- Production mode (--mode prod): single-node Raft Vault Enterprise ---
	// Dev mode boots `server -dev` (plaintext HTTP, in-memory, throwaway root
	// token). Prod mode boots a real `server -config` with integrated Raft
	// storage on a persistent volume, TLS on localhost, and auto init+unseal.
	// The generated unseal key + root token are cached via internal/global
	// (VaultInitCachePath) at ~/.hal/vault-prod/init.json (mode 0600).
	defaultVaultMode         = "dev"
	defaultVaultKeyShares    = 1
	defaultVaultKeyThreshold = 1
	defaultVaultProdNodeID   = "hal-vault-node-1"

	// Prod HTTPS endpoints (self-signed cert forged into ~/.hal/vault-prod/certs).
	vaultProdPublicURL   = "https://vault.localhost:8200"
	vaultProdLocalAPIURL = "https://127.0.0.1:8200"
	vaultProdHealthURL   = "https://vault.localhost:8200/v1/sys/health"

	// Prod on-disk layout (host side, under internal/global.VaultProdStateDir()).
	vaultProdConfigFileName = "vault.hcl"
	vaultProdCertsDirName   = "certs"
	vaultProdCertFileName   = "cert.pem"
	vaultProdKeyFileName    = "key.pem"

	// Prod in-container mount targets + cluster port.
	// NOTE: the official Vault image entrypoint auto-injects `-config=/vault/config`
	// whenever the command starts with `server`. We therefore mount our config
	// OUTSIDE /vault/config (which stays empty) and pass a single explicit
	// -config, so the listener is never loaded twice (a double load makes the
	// second bind of :8200 fail with "address already in use").
	vaultProdConfigMount = "/vault/hal/vault.hcl"
	vaultProdTLSMount    = "/vault/tls"
	vaultProdRaftMount   = "/vault/data"
	vaultProdClusterPort = 8201

	// --- Backend DB (MariaDB) ---
	vaultMariaDBHostAlias    = "mariadb.localhost"
	vaultMariaDBPort         = 3306
	vaultMariaDBRootPassword = "vaultroot"

	// --- Image / tag flag defaults ---
	defaultVaultImageCE     = "hashicorp/vault"
	defaultVaultImageEnt    = "hashicorp/vault-enterprise"
	defaultVaultTag         = "2.0.1"
	defaultVaultEntTag      = "2.0.1-ent"
	defaultVaultEdition     = "ce"
	defaultVaultHelperImage = "alpine"
	defaultVaultHelperTag   = "3.22"

	defaultVaultMariaDBImage = "mariadb"
	defaultVaultMariaDBTag   = "11.4"

	defaultOpenLDAPTag     = "1.5.0"
	defaultPHPLDAPAdminTag = "0.9.0"

	defaultVaultOSUbuntuImage = "22.04"
	defaultVaultOSVMCPUs      = "1"
	defaultVaultOSVMMem       = "1G"

	// --- Oracle Database ---
	vaultOracleContainer = "hal-vault-oracle-db"
	vaultOracleHostAlias = "oracle.localhost"
	vaultOraclePort      = 1521
	vaultOracleSysPass   = "OracleXE1!"
	vaultOracleVaultPass = "vaultpasswd"
	vaultOraclePDB       = "FREEPDB1"

	// Custom vault+oracle runtime image built locally by hal vault oracle enable.
	// Debian-slim base with vault binary + Oracle Instant Client (needs glibc).
	// The official hashicorp/vault image is Alpine-based (musl libc) and cannot
	// run the oracle plugin, which requires glibc via Oracle Instant Client.
	vaultOracleRuntimeImage = "hal-vault-oracle-runtime"
	vaultOracleRuntimeTag   = "latest"

	// gvenzl/oracle-free: Oracle Database 23ai Free, public (no Oracle account
	// needed), multi-arch (amd64 + arm64 from v23.5+).
	defaultOracleFreeImage  = "gvenzl/oracle-free"
	defaultOracleFreeTag    = "slim"
	defaultOraclePluginVer  = "0.14.1+ent"
	defaultInstantClientVer = "19.26.0.0.0"
	defaultInstantClientDir = "instantclient_19_26"

	defaultInstantClientVerARM64 = "23.26.2.0.0"
	defaultInstantClientDirARM64 = "instantclient_23_26"
)
