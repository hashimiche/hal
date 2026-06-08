package consul

// defaults.go is the single source of truth for values shared across the Consul
// command package. Any value used by more than one file in this package, plus
// every identity / endpoint / URL / image flag default, MUST be defined here so
// the commands can never drift apart. Cross-product values (the Docker network
// name) live in internal/global and are referenced, never redeclared.

const (
	// --- Container ---
	consulContainer = "hal-consul"

	// --- Host / ports / URL ---
	consulHostname  = "consul.localhost"
	consulHTTPPort  = 8500
	consulBaseURL   = "http://consul.localhost:8500"
	consulObsTarget = consulContainer + ":8500"

	// --- Image / tag flag defaults ---
	defaultConsulImage = "hashicorp/consul"
	defaultConsulTag   = "1.15.0"
)
