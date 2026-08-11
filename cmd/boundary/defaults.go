package boundary

// defaults.go is the single source of truth for values shared across the Boundary
// command package. Container names, ports, endpoint URLs, the dev login and every
// image/tag flag default are declared once here so create, status, delete, obs,
// mariadb and ssh can never drift apart. Cross-product values (the Docker network
// name) live in internal/global and are referenced, never redeclared.

const (
	// --- Containers / instances ---
	boundaryContainer        = "hal-boundary"
	boundaryBackendContainer = "hal-boundary-backend"
	boundaryMariaDBContainer = "hal-boundary-target-mariadb"
	boundarySSHInstance      = "hal-boundary-ssh" // Multipass VM
	// vaultMariaDBContainer mirrors the Vault package's MariaDB container name; it
	// is referenced (not owned) here when --with-vault attaches to that database.
	vaultMariaDBContainer = "hal-vault-mariadb"

	// --- Ports ---
	boundaryAPIPort     = 9200
	boundaryClusterPort = 9201
	boundaryProxyPort   = 9202
	boundaryMariaDBPort = 3306

	// --- Endpoints / dev login (shared by create, ssh, obs) ---
	boundaryUIURL         = "http://boundary.localhost:9200"
	boundaryLocalAPIURL   = "http://127.0.0.1:9200"
	boundaryAdminUsername = "admin"
	boundaryAdminPassword = "password"
	boundaryObsTarget     = boundaryContainer + ":9200"

	// --- Backend Postgres credentials ---
	boundaryDBUser     = "boundary"
	boundaryDBPassword = "boundary"
	boundaryDBName     = "boundary"

	// --- Image / tag flag defaults ---
	defaultBoundaryImage          = "hashicorp/boundary"
	defaultBoundaryTag            = "0.21.3"
	defaultBoundaryPGImage        = "postgres"
	defaultBoundaryPGTag          = "17-alpine"
	defaultBoundaryMariaDBImage   = "mariadb"
	defaultBoundaryMariaDBTag     = "11.8"
	defaultBoundarySSHUbuntuImage = "24.04"
	defaultBoundarySSHCPUs        = "1"
	defaultBoundarySSHMem         = "512M"
)
