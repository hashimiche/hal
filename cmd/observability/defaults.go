package observability

// defaults.go is the single source of truth for values shared across the
// observability (PLG stack) command package. Container names, public UI URLs and
// image/tag flag defaults are declared once here so create, status and delete can
// never drift apart. Cross-product values (the Docker network name) live in
// internal/global and are referenced, never redeclared.

const (
	// --- Container names (shared by create, status, delete) ---
	obsPrometheusContainer = "hal-prometheus"
	obsLokiContainer       = "hal-loki"
	obsPromtailContainer   = "hal-promtail"
	obsGrafanaContainer    = "hal-grafana"

	// --- Public UI URLs (shared by create + status output) ---
	obsGrafanaURL    = "http://grafana.localhost:3000"
	obsPrometheusURL = "http://prometheus.localhost:9090"
	obsLokiReadyURL  = "http://loki.localhost:3100/ready"

	// --- Image / tag flag defaults ---
	defaultLokiImage     = "grafana/loki"
	defaultLokiTag       = "3.8"
	defaultGrafanaImage  = "grafana/grafana"
	defaultGrafanaTag    = "main"
	defaultPromImage     = "prom/prometheus"
	defaultPromTag       = "main"
	defaultPromtailImage = "grafana/promtail"
	defaultPromtailTag   = "3.6"
)
