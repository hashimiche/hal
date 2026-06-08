package plus

// defaults.go complements the single source of truth for the HAL Plus package.
// Container names and image name/tag defaults are declared once in plus.go (every
// flag default references the same variable, so they cannot drift). This file
// holds the network endpoints and host values shared across more than one file.

const (
	// --- Public UI host (shared by create + status output) ---
	plusUIHostname = "hal.localhost"

	// --- Container-internal endpoints / ports ---
	plusAPIPort      = 9000
	plusMCPEnvURL    = "http://hal-mcp:8080/mcp"
	plusQdrantEnvURL = "http://hal-qdrant:6333"
)
