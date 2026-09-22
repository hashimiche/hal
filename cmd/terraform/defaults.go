package terraform

// defaults.go is the single source of truth for shared Terraform Enterprise
// deployment values.
//
// Rule: any value used by more than one file in this package — and every
// identity/credential/URL default exposed through a CLI flag — MUST be defined
// here and referenced by name. Never re-declare these as inline literals in a
// command file. This prevents the kind of silent drift that let the bootstrap
// org default ("hal") diverge from a downstream flag default.
//
// Values that are genuinely local to a single feature file (e.g. agent pool
// names, SAML service keys, CLI seed-file paths) intentionally stay with that
// feature; they are already defined in exactly one place.
//
// The shared Docker network name lives in internal/global (global.HalNetName);
// reference that directly rather than copying it here.

const (
	// --- Primary TFE container + shared backend service names ---
	// Used across create, delete, status, obs, saml, foundation, twin.
	tfeCoreContainer  = "hal-tfe"
	tfeProxyContainer = "hal-tfe-proxy"
	tfeDBContainer    = "hal-tfe-db"
	tfeRedisContainer = "hal-tfe-redis"
	tfeS3Container    = "hal-tfe-s3"

	// --- Named volumes backing the shared services (create, delete) ---
	tfeDBVolume    = "hal-tfe-db-data"
	tfeRedisVolume = "hal-tfe-redis-data"
	tfeS3Volume    = "hal-tfe-s3-data"
	tfeCacheVolume = "hal-tfe-cache"

	// --- Primary hostname, ports, and base URL (shared widely) ---
	tfePrimaryHostname     = "tfe.localhost"
	defaultTFETwinHostname = "tfe-bis.localhost"
	tfeHTTPPort            = 8080
	tfeHTTPSPort           = 8443
	tfeAdminHTTPSPort      = 8444
	tfeMetricsHTTPPort     = 9090
	tfeMetricsHTTPSPort    = 9091
	tfePrimaryBaseURL      = "https://tfe.localhost:8443"

	// --- Ingress proxy static-IP host number on hal-net ---
	// A single shared proxy serves every TFE target (one vhost each), so there is
	// exactly one host number. The IP is derived dynamically from the live hal-net
	// subnet via global.HalNetStaticIP(engine, hostNum) so it works on any
	// host/engine. Shared by create (primary), twin, and agent.
	tfeProxyHostNum = 250

	// --- Identity / credential defaults ---
	// Flag defaults and !Changed() fallbacks across create, api-workflow,
	// agent, vcs-workflow, saml.
	defaultTFEOrg                = "hal"
	defaultTFEProject            = "Dave"
	defaultTFETwinContainer      = "hal-tfe-bis"
	defaultTFEAdminUsername      = "haladmin"
	defaultTFEAdminEmail         = "haladmin@localhost"
	defaultTFEAdminPassword      = "hal9000FTW"
	defaultTFEEncryptionPassword = "hal-secret-encryption-password"

	// --- Shared backend service credentials / object storage config ---
	// Used by create and twin.
	tfeDBUser     = "tfe"
	tfeDBPassword = "tfe_password"
	tfeDBName     = "tfe"
	// hal9000FTW is the shared lab password — the same value backs the TFE admin
	// user (defaultTFEAdminPassword) and the GitLab root account. One password to
	// remember for the whole lab; do not invent a new secret here.
	tfeS3AccessKey = "haladmin"
	tfeS3SecretKey = "hal9000FTW"
	tfeS3Bucket    = "tfe-data"
	tfeS3Region    = "us-east-1"

	// --- Default backend images + tags (create flag defaults) ---
	// The TFE core image is also reused by the twin lifecycle.
	defaultTFEImage      = "images.releases.hashicorp.com/hashicorp/terraform-enterprise"
	defaultTFETag        = "2.0.5"
	defaultTFEPGImage    = "postgres"
	defaultTFEPGTag      = "17-alpine"
	defaultTFERedisImage = "redis"
	defaultTFERedisTag   = "8-alpine"
	defaultTFES3Image    = "ghcr.io/versity/versitygw"
	defaultTFES3Tag      = "v1.8.0"
	defaultTFEProxyImage = "nginx"
	defaultTFEProxyTag   = "alpine"

	// --- Host port mappings (create flag defaults) ---
	defaultTFES3APIHostPort = 19000

	// --- ~/.hal filesystem layout (create, delete, api-workflow, agent) ---
	halStateDirName = ".hal"
	// tfeCertsDirName holds the single TLS cert/key shared by every TFE target.
	tfeCertsDirName = "tfe-certs"
	// tfeProxyDirName holds the shared proxy's nginx.conf plus one vhost file per
	// deployed target under tfeProxyVhostsDirName.
	tfeProxyDirName       = "tfe-proxy"
	tfeProxyVhostsDirName = "vhosts"
	tfeAPITokenFileName   = "tfe-app-api-token"
)
