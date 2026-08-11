package nomad

// defaults.go is the single source of truth for values shared across the Nomad
// command package. Any value used by more than one file in this package, plus
// every identity / endpoint / URL / image flag default, MUST be defined here so
// the commands can never drift apart.

const (
	// --- Multipass instance / host / port ---
	nomadInstance = "hal-nomad"
	nomadHostname = "nomad.localhost"
	nomadHTTPPort = 4646

	// --- Flag defaults ---
	defaultNomadVersion     = "2.0.3"
	defaultNomadUbuntuImage = "24.04"
	defaultNomadCPUs        = "2"
	defaultNomadMem         = "2G"
)
