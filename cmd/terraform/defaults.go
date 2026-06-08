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
	tfeMinioContainer = "hal-tfe-minio"

	// --- Named volumes backing the shared services (create, delete) ---
	tfeDBVolume    = "hal-tfe-db-data"
	tfeRedisVolume = "hal-tfe-redis-data"
	tfeMinioVolume = "hal-tfe-minio-data"
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

	// --- Ingress proxy static-IP host numbers on hal-net ---
	// The proxy IP is derived dynamically from the live hal-net subnet via
	// global.HalNetStaticIP(engine, hostNum) so it works on any host/engine.
	// These host numbers are shared by create (primary), twin, and agent.
	tfePrimaryProxyHostNum = 250
	tfeTwinProxyHostNum    = 249

	// --- Identity / credential defaults ---
	// Flag defaults and !Changed() fallbacks across create, api-workflow,
	// agent, vcs-workflow, saml.
	defaultTFEOrg                = "hal"
	defaultTFEProject            = "Dave"
	defaultTFEAdminUsername      = "haladmin"
	defaultTFEAdminEmail         = "haladmin@localhost"
	defaultTFEAdminPassword      = "hal9000FTW"
	defaultTFEEncryptionPassword = "hal-secret-encryption-password"

	// --- Shared backend service credentials / object storage config ---
	// Used by create and twin.
	tfeDBUser        = "tfe"
	tfeDBPassword    = "tfe_password"
	tfeDBName        = "tfe"
	tfeMinioRootUser = "minioadmin"
	tfeMinioRootPass = "minioadmin"
	tfeS3Bucket      = "tfe-data"
	tfeS3Region      = "us-east-1"

	// --- Default backend images + tags (create flag defaults) ---
	// The TFE core image is also reused by the twin lifecycle.
	defaultTFEImage      = "images.releases.hashicorp.com/hashicorp/terraform-enterprise"
	defaultTFETag        = "2.0.2"
	defaultTFEPGImage    = "postgres"
	defaultTFEPGTag      = "16-alpine"
	defaultTFERedisImage = "redis"
	defaultTFERedisTag   = "7-alpine"
	defaultTFEMinioImage = "minio/minio"
	defaultTFEMinioTag   = "latest"
	defaultTFEProxyImage = "nginx"
	defaultTFEProxyTag   = "alpine"

	// --- Host port mappings (create flag defaults) ---
	defaultMinioAPIHostPort     = 19000
	defaultMinioConsoleHostPort = 19001

	// --- ~/.hal filesystem layout (create, delete, api-workflow, agent) ---
	halStateDirName     = ".hal"
	tfeCertsDirName     = "tfe-certs"
	tfeProxyConfName    = "tfe-proxy.conf"
	tfeAPITokenFileName = "tfe-app-api-token"
)
