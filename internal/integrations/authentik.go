package integrations

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hal/internal/global"
)

const (
	AuthentikPGContainer     = "hal-authentik-pg"
	AuthentikServerContainer = "hal-authentik-server"
	AuthentikWorkerContainer = "hal-authentik-worker"

	AuthentikDefaultImage = "ghcr.io/goauthentik/server"
	AuthentikDefaultTag   = "2026.2.3"

	// Host ports — chosen to avoid all existing HAL port usage.
	// 9000: hal-plus / MinIO internal
	// 9001: hal-health / MinIO console internal
	// 9443: TFE twin HTTPS
	AuthentikHTTPPort  = "9100"
	AuthentikHTTPSPort = "9143"

	// Shared-services key used in ~/.hal/shared-services.json
	AuthentikSharedServiceKey = "authentik-idp"
)

// AuthentikSecrets holds the secrets loaded from / generated into ~/.hal/authentik/env.
type AuthentikSecrets struct {
	PGPass         string
	SecretKey      string
	BootstrapToken string
	AdminPassword  string
}

// AuthentikAdminURL returns the canonical base URL for the Authentik UI/API.
// Uses authentik.localhost so the URL is identical from the host browser
// (macOS resolves *.localhost → 127.0.0.1) and from containers on hal-net
// (via --network-alias authentik.localhost). This guarantees a consistent
// OIDC issuer in tokens so Vault validation never fails.
func AuthentikAdminURL() string {
	return fmt.Sprintf("http://authentik.localhost:%s", AuthentikHTTPPort)
}

// AuthentikOIDCIssuer returns the OIDC issuer URL for a given application slug.
// Vault must use this URL for both oidc_discovery_url and callback validation.
func AuthentikOIDCIssuer(slug string) string {
	return fmt.Sprintf("http://authentik.localhost:%s/application/o/%s/", AuthentikHTTPPort, slug)
}

// authentikDir returns ~/.hal/authentik.
func authentikDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".hal", "authentik")
}

// AuthentikEnvPath returns the path to the persistent secrets file.
func AuthentikEnvPath() string {
	return filepath.Join(authentikDir(), "env")
}

// AuthentikDataDir returns the bind-mount path for /data in server/worker containers.
func AuthentikDataDir() string {
	return filepath.Join(authentikDir(), "data")
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// LoadOrCreateAuthentikSecrets reads ~/.hal/authentik/env or generates fresh values.
// The file is created with mode 0600.
func LoadOrCreateAuthentikSecrets() (*AuthentikSecrets, error) {
	envPath := AuthentikEnvPath()
	s := &AuthentikSecrets{}

	data, err := os.ReadFile(envPath)
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
			if len(parts) != 2 || parts[0] == "" {
				continue
			}
			switch parts[0] {
			case "PG_PASS":
				s.PGPass = parts[1]
			case "AUTHENTIK_SECRET_KEY":
				s.SecretKey = parts[1]
			case "AUTHENTIK_BOOTSTRAP_TOKEN":
				s.BootstrapToken = parts[1]
			case "AUTHENTIK_BOOTSTRAP_PASSWORD":
				s.AdminPassword = parts[1]
			}
		}
		if s.PGPass != "" && s.SecretKey != "" && s.BootstrapToken != "" && s.AdminPassword != "" {
			return s, nil
		}
	}

	// Generate any missing values.
	if s.PGPass == "" {
		s.PGPass = randomHex(16) // 32-char hex; stays well under pg 99-char limit
	}
	if s.SecretKey == "" {
		s.SecretKey = randomHex(32) // 64-char hex
	}
	if s.BootstrapToken == "" {
		s.BootstrapToken = randomHex(20) // 40-char hex, used as static API token
	}
	if s.AdminPassword == "" {
		s.AdminPassword = randomHex(12) // 24-char hex, printed once to user
	}

	if err := os.MkdirAll(filepath.Dir(envPath), 0o700); err != nil {
		return nil, fmt.Errorf("failed to create authentik config dir: %w", err)
	}

	content := fmt.Sprintf(
		"PG_PASS=%s\nAUTHENTIK_SECRET_KEY=%s\nAUTHENTIK_BOOTSTRAP_TOKEN=%s\nAUTHENTIK_BOOTSTRAP_PASSWORD=%s\n",
		s.PGPass, s.SecretKey, s.BootstrapToken, s.AdminPassword,
	)
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		return nil, fmt.Errorf("failed to write authentik env: %w", err)
	}

	return s, nil
}

// IsAuthentikRunning returns true if hal-authentik-server container is up.
func IsAuthentikRunning(engine string) bool {
	return global.CheckContainer(engine, AuthentikServerContainer)
}

// StartAuthentikStack brings up hal-authentik-pg → hal-authentik-server → hal-authentik-worker.
// image and tag control the Authentik image (defaults: AuthentikDefaultImage / AuthentikDefaultTag).
func StartAuthentikStack(engine, image, tag string, secrets *AuthentikSecrets) error {
	if image == "" {
		image = AuthentikDefaultImage
	}
	if tag == "" {
		tag = AuthentikDefaultTag
	}
	imgRef := fmt.Sprintf("%s:%s", image, tag)

	dataDir := AuthentikDataDir()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("failed to create authentik data dir: %w", err)
	}

	global.EnsureNetwork(engine)

	// ── 1. PostgreSQL ────────────────────────────────────────────────────────────
	fmt.Println("  ⏳ Starting Authentik PostgreSQL...")
	pgArgs := []string{
		"run", "-d",
		"--name", AuthentikPGContainer,
		"--network", global.HalNetName,
		"--restart", "unless-stopped",
		"-e", "POSTGRES_DB=authentik",
		"-e", "POSTGRES_USER=authentik",
		"-e", fmt.Sprintf("POSTGRES_PASSWORD=%s", secrets.PGPass),
		"-v", "hal-authentik-db:/var/lib/postgresql/data",
		"docker.io/library/postgres:16-alpine",
	}
	if out, err := exec.Command(engine, pgArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start authentik pg: %w\n%s", err, string(out))
	}

	// ── 2. Wait for pg ───────────────────────────────────────────────────────────
	fmt.Print("  ⏳ Waiting for PostgreSQL")
	deadline := time.Now().Add(30 * time.Second)
	pgReady := false
	for time.Now().Before(deadline) {
		if exec.Command(engine, "exec", AuthentikPGContainer,
			"pg_isready", "-d", "authentik", "-U", "authentik").Run() == nil {
			pgReady = true
			break
		}
		fmt.Print(".")
		time.Sleep(2 * time.Second)
	}
	fmt.Println()
	if !pgReady {
		return fmt.Errorf("postgresql did not become ready within 30s")
	}
	fmt.Println("  ✅ PostgreSQL ready")

	// ── 3. Server ────────────────────────────────────────────────────────────────
	fmt.Println("  ⏳ Starting Authentik server...")
	serverArgs := []string{
		"run", "-d",
		"--name", AuthentikServerContainer,
		"--network", global.HalNetName,
		"--restart", "unless-stopped",
		// Use the same port inside and outside the container so the OIDC issuer
		// URL (derived from the Host header) is consistent across browser and
		// container-to-container traffic.
		"-p", fmt.Sprintf("%s:%s", AuthentikHTTPPort, AuthentikHTTPPort),
		"-p", fmt.Sprintf("%s:%s", AuthentikHTTPSPort, AuthentikHTTPSPort),
		"--network-alias", "authentik.localhost",
		"--shm-size", "512mb",
		"-e", fmt.Sprintf("AUTHENTIK_POSTGRESQL__HOST=%s", AuthentikPGContainer),
		"-e", "AUTHENTIK_POSTGRESQL__NAME=authentik",
		"-e", "AUTHENTIK_POSTGRESQL__USER=authentik",
		"-e", fmt.Sprintf("AUTHENTIK_POSTGRESQL__PASSWORD=%s", secrets.PGPass),
		"-e", fmt.Sprintf("AUTHENTIK_SECRET_KEY=%s", secrets.SecretKey),
		"-e", fmt.Sprintf("AUTHENTIK_BOOTSTRAP_TOKEN=%s", secrets.BootstrapToken),
		"-e", fmt.Sprintf("AUTHENTIK_BOOTSTRAP_PASSWORD=%s", secrets.AdminPassword),
		"-e", fmt.Sprintf("AUTHENTIK_LISTEN__HTTP=0.0.0.0:%s", AuthentikHTTPPort),
		"-e", fmt.Sprintf("AUTHENTIK_LISTEN__HTTPS=0.0.0.0:%s", AuthentikHTTPSPort),
		"-e", "AUTHENTIK_OUTPOSTS__DISCOVER=false",
		"-e", "AUTHENTIK_STORAGE__MEDIA__BACKEND=file",
		"-e", "AUTHENTIK_ERROR_REPORTING__ENABLED=false",
		"-e", "AUTHENTIK_DISABLE_UPDATE_CHECK=true",
		"-v", fmt.Sprintf("%s:/data", dataDir),
		imgRef,
		"server",
	}
	if out, err := exec.Command(engine, serverArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start authentik server: %w\n%s", err, string(out))
	}

	// ── 4. Worker (no docker socket) ─────────────────────────────────────────────
	fmt.Println("  ⏳ Starting Authentik worker...")
	workerArgs := []string{
		"run", "-d",
		"--name", AuthentikWorkerContainer,
		"--network", global.HalNetName,
		"--restart", "unless-stopped",
		"--shm-size", "512mb",
	}
	// Docker (non-rootless) requires root user for the worker; podman rootless does not need it.
	if !strings.Contains(engine, "podman") {
		workerArgs = append(workerArgs, "--user", "root")
	}
	workerArgs = append(workerArgs,
		"-e", fmt.Sprintf("AUTHENTIK_POSTGRESQL__HOST=%s", AuthentikPGContainer),
		"-e", "AUTHENTIK_POSTGRESQL__NAME=authentik",
		"-e", "AUTHENTIK_POSTGRESQL__USER=authentik",
		"-e", fmt.Sprintf("AUTHENTIK_POSTGRESQL__PASSWORD=%s", secrets.PGPass),
		"-e", fmt.Sprintf("AUTHENTIK_SECRET_KEY=%s", secrets.SecretKey),
		// Bootstrap vars must also be on the worker — it runs Celery tasks that
		// create the initial API token. Without them the token is never created.
		"-e", fmt.Sprintf("AUTHENTIK_BOOTSTRAP_TOKEN=%s", secrets.BootstrapToken),
		"-e", fmt.Sprintf("AUTHENTIK_BOOTSTRAP_PASSWORD=%s", secrets.AdminPassword),
		"-e", "AUTHENTIK_OUTPOSTS__DISCOVER=false",
		"-e", "AUTHENTIK_STORAGE__MEDIA__BACKEND=file",
		"-e", "AUTHENTIK_ERROR_REPORTING__ENABLED=false",
		"-e", "AUTHENTIK_DISABLE_UPDATE_CHECK=true",
		"-v", fmt.Sprintf("%s:/data", dataDir),
		imgRef,
		"worker",
	)
	if out, err := exec.Command(engine, workerArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start authentik worker: %w\n%s", err, string(out))
	}

	return nil
}

// WaitAuthentikHealthy polls GET /api/v3/root/config/ until Authentik responds 200.
// Timeout: 90 seconds (Authentik runs migrations on first boot which takes time).
func WaitAuthentikHealthy() error {
	url := fmt.Sprintf("http://localhost:%s/api/v3/root/config/", AuthentikHTTPPort)
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(90 * time.Second)

	fmt.Print("  ⏳ Waiting for Authentik API")
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				fmt.Println(" ✅")
				return nil
			}
		}
		fmt.Print(".")
		time.Sleep(3 * time.Second)
	}
	fmt.Println()
	return fmt.Errorf("authentik did not become ready within 90s — check: docker logs %s", AuthentikServerContainer)
}

// WaitAuthentikTokenReady polls GET /api/v3/core/groups/ with the bootstrap token
// until the endpoint returns 200. This verifies the token has admin-level API access,
// not just basic authentication — the worker creates it asynchronously after migrations.
// Timeout: 60 seconds.
func WaitAuthentikTokenReady(token string) error {
	// Use an admin-only endpoint (requires is_superuser) so we confirm the token
	// has the same class of permissions that CreateGroup/CreateUser need.
	url := fmt.Sprintf("http://localhost:%s/api/v3/core/groups/", AuthentikHTTPPort)
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(60 * time.Second)

	fmt.Print("  ⏳ Waiting for Authentik bootstrap token")
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				fmt.Println(" ✅")
				return nil
			}
		}
		fmt.Print(".")
		time.Sleep(3 * time.Second)
	}
	fmt.Println()
	return fmt.Errorf("authentik bootstrap token not ready within 60s — check: docker logs %s", AuthentikWorkerContainer)
}

// WaitAuthentikScopesReady polls until the standard openid/profile/email scope
// property mappings exist. Authentik seeds these asynchronously after the first
// migration run, so they may not be present immediately after the bootstrap token
// becomes usable. Timeout: 60 seconds.
func WaitAuthentikScopesReady(token string) error {
	url := fmt.Sprintf("http://localhost:%s/api/v3/propertymappings/provider/scope/?page_size=100", AuthentikHTTPPort)
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(60 * time.Second)
	required := map[string]bool{"openid": false, "profile": false, "email": false}

	fmt.Print("  ⏳ Waiting for Authentik scope mappings")
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			var data map[string]interface{}
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if json.Unmarshal(raw, &data) == nil {
				found := 0
				results, _ := data["results"].([]interface{})
				for _, r := range results {
					item, _ := r.(map[string]interface{})
					if sn, _ := item["scope_name"].(string); required[sn] {
						found++
					}
				}
				if found == len(required) {
					fmt.Println(" ✅")
					return nil
				}
			}
		} else if resp != nil {
			resp.Body.Close()
		}
		fmt.Print(".")
		time.Sleep(3 * time.Second)
	}
	fmt.Println()
	return fmt.Errorf("authentik default scope mappings not ready within 60s — check: docker logs %s", AuthentikWorkerContainer)
}

// StopAuthentikStack stops and removes all Authentik containers.
// If removeVolumes is true, the named volume hal-authentik-db is also removed.
func StopAuthentikStack(engine string, removeVolumes bool) error {
	for _, name := range []string{AuthentikWorkerContainer, AuthentikServerContainer, AuthentikPGContainer} {
		_ = exec.Command(engine, "rm", "-f", name).Run()
	}
	if removeVolumes {
		_ = exec.Command(engine, "volume", "rm", "-f", "hal-authentik-db").Run()
	}
	return nil
}

// PrintAuthentikStatus prints a one-line health summary for each Authentik container.
func PrintAuthentikStatus(engine string) {
	pg := global.CheckContainer(engine, AuthentikPGContainer)
	server := global.CheckContainer(engine, AuthentikServerContainer)
	worker := global.CheckContainer(engine, AuthentikWorkerContainer)

	icon := func(up bool) string {
		if up {
			return "✅"
		}
		return "❌"
	}

	fmt.Printf("  %s Authentik PostgreSQL  : %s\n", icon(pg), AuthentikPGContainer)
	fmt.Printf("  %s Authentik Server      : %s  (http://localhost:%s)\n", icon(server), AuthentikServerContainer, AuthentikHTTPPort)
	fmt.Printf("  %s Authentik Worker      : %s\n", icon(worker), AuthentikWorkerContainer)

	if server {
		// Quick API probe
		client := &http.Client{Timeout: 3 * time.Second}
		url := fmt.Sprintf("http://localhost:%s/api/v3/root/config/", AuthentikHTTPPort)
		resp, err := client.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			fmt.Printf("  ✅ Authentik API        : reachable\n")
		} else {
			if resp != nil {
				resp.Body.Close()
			}
			fmt.Printf("  ⚠️  Authentik API        : server running but API not responding yet\n")
		}
	}
}

// WaitVaultVisibleAuthentik verifies that the hal-vault container can reach
// Authentik's OIDC discovery URL. This is required before configuring Vault OIDC
// because Vault fetches the discovery document at write time.
func WaitVaultVisibleAuthentik(engine, issuer string) error {
	discoveryURL := strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration"
	for i := 0; i < 30; i++ {
		cmd := exec.Command(engine, "exec", "hal-vault", "sh", "-lc",
			fmt.Sprintf(
				"command -v curl >/dev/null 2>&1 && curl -fsS %q >/dev/null || wget -qO- %q >/dev/null",
				discoveryURL, discoveryURL,
			),
		)
		if cmd.Run() == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout: Vault container cannot reach %s\n  Check that hal-vault is running and on hal-net", discoveryURL)
}

// ─── Authentik REST API client ────────────────────────────────────────────────

// AuthentikClient makes authenticated calls to the Authentik REST API using
// the bootstrap token created by AUTHENTIK_BOOTSTRAP_TOKEN on first boot.
type AuthentikClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewAuthentikClient returns a client pointed at the host-accessible Authentik URL.
func NewAuthentikClient(token string) *AuthentikClient {
	return &AuthentikClient{
		baseURL: fmt.Sprintf("http://authentik.localhost:%s", AuthentikHTTPPort),
		token:   token,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// BaseURL returns the host-accessible Authentik base URL (e.g. http://authentik.localhost:9100).
func (c *AuthentikClient) BaseURL() string { return c.baseURL }

// Token returns the API token used by this client (the Authentik bootstrap token).
func (c *AuthentikClient) Token() string { return c.token }

// do executes a JSON API call, decodes the response and returns it as a generic map.
// body may be nil for GET requests. Returns (nil, statusCode, nil) on 204 No Content.
func (c *AuthentikClient) do(method, path string, body interface{}) (map[string]interface{}, int, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, fmt.Errorf("authentik %s %s → %d: %s", method, path, resp.StatusCode, string(raw))
	}
	if len(raw) == 0 {
		return nil, resp.StatusCode, nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode response: %w", err)
	}
	return result, resp.StatusCode, nil
}

// firstResult returns the first item from a paginated list response.
func firstResult(data map[string]interface{}) (map[string]interface{}, error) {
	results, ok := data["results"].([]interface{})
	if !ok || len(results) == 0 {
		return nil, fmt.Errorf("no results returned")
	}
	item, ok := results[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected result type")
	}
	return item, nil
}

// CreateGroup creates an Authentik group and returns its UUID pk.
// Returns the existing group pk if the name already exists.
func (c *AuthentikClient) CreateGroup(name string) (string, error) {
	data, _, err := c.do("POST", "/api/v3/core/groups/", map[string]interface{}{
		"name":         name,
		"is_superuser": false,
	})
	if err != nil {
		// Already exists → look it up
		if strings.Contains(err.Error(), "400") {
			data, _, err2 := c.do("GET", "/api/v3/core/groups/?name="+name, nil)
			if err2 != nil {
				return "", err2
			}
			item, err2 := firstResult(data)
			if err2 != nil {
				return "", fmt.Errorf("group %q: %w", name, err)
			}
			return item["pk"].(string), nil
		}
		return "", err
	}
	return data["pk"].(string), nil
}

// CreateUser creates an Authentik user and sets their password. Idempotent.
func (c *AuthentikClient) CreateUser(username, displayName, email, password string, groupPKs []string) error {
	data, _, err := c.do("POST", "/api/v3/core/users/", map[string]interface{}{
		"username":  username,
		"name":      displayName,
		"email":     email,
		"type":      "internal",
		"is_active": true,
		"groups":    groupPKs,
	})
	if err != nil {
		if strings.Contains(err.Error(), "400") {
			return nil // already exists — skip silently
		}
		return err
	}

	// Extract integer pk
	pkFloat, ok := data["pk"].(float64)
	if !ok {
		return fmt.Errorf("unexpected user pk type")
	}
	pk := int(pkFloat)

	// Set password
	_, _, err = c.do("POST", fmt.Sprintf("/api/v3/core/users/%d/set_password/", pk), map[string]interface{}{
		"password": password,
	})
	return err
}

// GetDefaultInvalidationFlowPK returns the PK of the default provider invalidation flow.
// Required by CreateOAuth2Provider since Authentik 2024.x.
func (c *AuthentikClient) GetDefaultInvalidationFlowPK() (string, error) {
	data, _, err := c.do("GET", "/api/v3/flows/instances/?designation=invalidation&ordering=slug", nil)
	if err != nil {
		return "", err
	}
	// Prefer the provider-specific invalidation flow if present.
	results, _ := data["results"].([]interface{})
	for _, r := range results {
		item, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		if slug, _ := item["slug"].(string); strings.Contains(slug, "provider") {
			return item["pk"].(string), nil
		}
	}
	// Fall back to first available invalidation flow.
	item, err := firstResult(data)
	if err != nil {
		return "", fmt.Errorf("invalidation flow: %w", err)
	}
	return item["pk"].(string), nil
}

// GetDefaultAuthorizationFlowPK returns the PK of the implicit-consent authorization flow.
// Implicit consent auto-approves without showing a user-facing consent page, which
// keeps the OIDC popup clean and prevents duplicate callback races that corrupt OAuth state.
func (c *AuthentikClient) GetDefaultAuthorizationFlowPK() (string, error) {
	data, _, err := c.do("GET", "/api/v3/flows/instances/?designation=authorization&ordering=slug", nil)
	if err != nil {
		return "", err
	}
	// Prefer the implicit-consent flow so the OIDC popup redirects directly back to the
	// RP (Vault) without showing an intermediate consent page that the user might
	// navigate away from (which would orphan the OAuth state on the next attempt).
	results, _ := data["results"].([]interface{})
	for _, r := range results {
		item, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		if slug, _ := item["slug"].(string); strings.Contains(slug, "implicit") {
			return item["pk"].(string), nil
		}
	}
	// Fall back to first available authorization flow.
	item, err := firstResult(data)
	if err != nil {
		return "", fmt.Errorf("authorization flow: %w", err)
	}
	return item["pk"].(string), nil
}

// GetFirstSigningKeyPK returns the PK of the first available certificate key pair.
func (c *AuthentikClient) GetFirstSigningKeyPK() (string, error) {
	data, _, err := c.do("GET", "/api/v3/crypto/certificatekeypairs/?has_key=true&ordering=name", nil)
	if err != nil {
		return "", err
	}
	item, err := firstResult(data)
	if err != nil {
		return "", fmt.Errorf("signing key: %w", err)
	}
	return item["pk"].(string), nil
}

// GetGroupsScopeMappingPK returns the PK of the OIDC groups scope property mapping,
// creating it if it doesn't exist. The built-in groups mapping was removed from
// Authentik defaults in 2024.x — this ensures it's always present.
// Path changed in Authentik 2024.4: /propertymappings/scope/ → /propertymappings/provider/scope/
func (c *AuthentikClient) GetGroupsScopeMappingPK() (string, error) {
	data, _, err := c.do("GET", "/api/v3/propertymappings/provider/scope/?search=groups", nil)
	if err != nil {
		return "", err
	}
	results, _ := data["results"].([]interface{})
	for _, r := range results {
		item, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		scopeName, _ := item["scope_name"].(string)
		managed, _ := item["managed"].(string)
		if strings.Contains(scopeName, "group") || strings.Contains(managed, "group") {
			return item["pk"].(string), nil
		}
	}

	// Not found — create a custom groups scope mapping.
	// The expression returns the list of group names for the authenticated user.
	created, _, err := c.do("POST", "/api/v3/propertymappings/provider/scope/", map[string]interface{}{
		"name":        "hal: OIDC groups scope",
		"scope_name":  "groups",
		"description": "Adds a 'groups' claim containing the user's group names. Created by hal.",
		"expression":  `return list(request.user.ak_groups.values_list("name", flat=True))`,
	})
	if err != nil {
		// Already exists with same name — look it up by scope_name
		if strings.Contains(err.Error(), "400") {
			data2, _, err2 := c.do("GET", "/api/v3/propertymappings/provider/scope/?scope_name=groups", nil)
			if err2 != nil {
				return "", fmt.Errorf("groups scope mapping: %w", err2)
			}
			item, err2 := firstResult(data2)
			if err2 != nil {
				return "", fmt.Errorf("groups scope mapping not found and could not be created")
			}
			return item["pk"].(string), nil
		}
		return "", fmt.Errorf("create groups scope mapping: %w", err)
	}
	return created["pk"].(string), nil
}

// AuthentikRedirectURI pairs a matching mode with a URL for OAuth2 redirect_uris.
type AuthentikRedirectURI struct {
	MatchingMode string `json:"matching_mode"`
	URL          string `json:"url"`
}

// CreateOAuth2Provider creates a confidential OAuth2/OIDC provider.
// Returns (providerPK, clientID, clientSecret, error).
func (c *AuthentikClient) CreateOAuth2Provider(
	name, flowPK, invalidationFlowPK, signingKeyPK, groupsScopePK string,
	redirectURIs []AuthentikRedirectURI,
) (int, string, string, error) {
	// Collect the standard scope PKs (openid, profile, email) so the JWT contains
	// preferred_username, name, email, etc. alongside our custom groups claim.
	standardPKs, err := c.GetScopeMappingPKsByName([]string{"openid", "profile", "email"})
	if err != nil {
		return 0, "", "", fmt.Errorf("get standard scope mappings: %w", err)
	}
	if len(standardPKs) < 3 {
		return 0, "", "", fmt.Errorf("expected 3 standard scope mappings (openid/profile/email), got %d — JWT will be missing claims", len(standardPKs))
	}
	mappings := append(standardPKs, groupsScopePK)

	body := map[string]interface{}{
		"name":                       name,
		"authorization_flow":         flowPK,
		"invalidation_flow":          invalidationFlowPK,
		"client_type":                "confidential",
		"sub_mode":                   "user_username",
		"include_claims_in_id_token": true,
		"signing_key":                signingKeyPK,
		"property_mappings":          mappings,
		"redirect_uris":              redirectURIs,
	}

	data, _, err := c.do("POST", "/api/v3/providers/oauth2/", body)
	if err != nil {
		if !strings.Contains(err.Error(), "400") {
			return 0, "", "", err
		}
		// Provider already exists — look it up by name and PATCH it so redirect URIs
		// and mappings stay current, then return its credentials.
		existing, _, err2 := c.do("GET", "/api/v3/providers/oauth2/?search="+name, nil)
		if err2 != nil {
			return 0, "", "", fmt.Errorf("provider %q already exists and lookup failed: %w", name, err2)
		}
		results, _ := existing["results"].([]interface{})
		for _, r := range results {
			item, ok := r.(map[string]interface{})
			if !ok {
				continue
			}
			if item["name"] != name {
				continue
			}
			pkFloat, _ := item["pk"].(float64)
			pk := int(pkFloat)
			// PATCH to refresh redirect_uris and property_mappings.
			updated, _, _ := c.do("PATCH", fmt.Sprintf("/api/v3/providers/oauth2/%d/", pk), map[string]interface{}{
				"redirect_uris":     redirectURIs,
				"property_mappings": mappings,
			})
			if updated != nil {
				item = updated
			}
			clientID, _ := item["client_id"].(string)
			clientSecret, _ := item["client_secret"].(string)
			return pk, clientID, clientSecret, nil
		}
		return 0, "", "", fmt.Errorf("provider %q already exists but could not be found in search results", name)
	}

	pkFloat, _ := data["pk"].(float64)
	clientID, _ := data["client_id"].(string)
	clientSecret, _ := data["client_secret"].(string)
	return int(pkFloat), clientID, clientSecret, nil
}

// GetScopeMappingPKsByName returns the PKs of the scope property mappings with the given scope_names.
// Missing scopes are silently skipped (best-effort).
func (c *AuthentikClient) GetScopeMappingPKsByName(scopeNames []string) ([]string, error) {
	// page_size=100 to avoid pagination silently dropping standard built-in mappings.
	data, _, err := c.do("GET", "/api/v3/propertymappings/provider/scope/?page_size=100", nil)
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(scopeNames))
	for _, s := range scopeNames {
		wanted[s] = true
	}
	var pks []string
	results, _ := data["results"].([]interface{})
	for _, r := range results {
		item, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		scopeName, _ := item["scope_name"].(string)
		if wanted[scopeName] {
			pks = append(pks, item["pk"].(string))
		}
	}
	return pks, nil
}

// CreateApplication creates an Authentik application linked to a provider. Idempotent.
// launchURL is set as meta_launch_url so the application tile in the Authentik portal
// points to the SP's login page (e.g. the Vault UI OIDC page) rather than the
// auto-computed URL which may use an internal container hostname.
func (c *AuthentikClient) CreateApplication(name, slug string, providerPK int, launchURL string) error {
	body := map[string]interface{}{
		"name":            name,
		"slug":            slug,
		"provider":        providerPK,
		"meta_launch_url": launchURL,
	}
	_, _, err := c.do("POST", "/api/v3/core/applications/", body)
	if err != nil && strings.Contains(err.Error(), "400") {
		// Already exists — update provider binding and launch URL.
		_, _, err = c.do("PATCH", "/api/v3/core/applications/"+slug+"/", map[string]interface{}{
			"provider":        providerPK,
			"meta_launch_url": launchURL,
		})
	}
	return err
}

// DeleteApplicationBySlug deletes an Authentik application by its slug. No-op if not found.
func (c *AuthentikClient) DeleteApplicationBySlug(slug string) error {
	_, _, err := c.do("DELETE", "/api/v3/core/applications/"+slug+"/", nil)
	if err != nil && strings.Contains(err.Error(), "404") {
		return nil
	}
	return err
}

// DeleteOAuth2ProviderByName deletes an OAuth2 provider by name. No-op if not found.
func (c *AuthentikClient) DeleteOAuth2ProviderByName(name string) error {
	data, _, err := c.do("GET", "/api/v3/providers/oauth2/?search="+name, nil)
	if err != nil {
		return err
	}
	results, _ := data["results"].([]interface{})
	for _, r := range results {
		item, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		if item["name"] == name {
			pkFloat, _ := item["pk"].(float64)
			pk := int(pkFloat)
			_, _, err = c.do("DELETE", fmt.Sprintf("/api/v3/providers/oauth2/%d/", pk), nil)
			if err != nil && strings.Contains(err.Error(), "404") {
				return nil
			}
			return err
		}
	}
	return nil // not found — nothing to delete
}

// ─── SCIM provider management ─────────────────────────────────────────────────

// GetDefaultSCIMPropertyMappings returns Authentik's built-in SCIM property mapping PKs
// partitioned into user mappings and group mappings.
// Authentik ships defaults with managed fields under goauthentik.io/providers/scim/user*
// and goauthentik.io/providers/scim/group*.
func (c *AuthentikClient) GetDefaultSCIMPropertyMappings() (userPKs, groupPKs []string, err error) {
	data, _, err := c.do("GET", "/api/v3/propertymappings/provider/scim/", nil)
	if err != nil {
		return nil, nil, err
	}
	results, _ := data["results"].([]interface{})
	for _, r := range results {
		item, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		pk, _ := item["pk"].(string)
		managed, _ := item["managed"].(string)
		name, _ := item["name"].(string)

		isGroup := strings.Contains(managed, "/group") || strings.Contains(strings.ToLower(name), "group")
		if isGroup {
			groupPKs = append(groupPKs, pk)
		} else {
			userPKs = append(userPKs, pk)
		}
	}
	return userPKs, groupPKs, nil
}

// CreateSCIMProvider creates an Authentik outbound SCIM provider pointing at url.
// Returns the provider integer PK.
func (c *AuthentikClient) CreateSCIMProvider(name, baseURL, token string, userMappingPKs, groupMappingPKs []string) (int, error) {
	body := map[string]interface{}{
		"name":                    name,
		"url":                     baseURL,
		"token":                   token,
		"property_mappings":       userMappingPKs,
		"property_mappings_group": groupMappingPKs,
		// AWS compatibility mode avoids including "schemas" inside PATCH Operation
		// objects. Vault SCIM rejects that attribute with invalidValue/400, which
		// prevents group membership from ever being pushed (members list stays empty).
		"compatibility_mode": "aws",
	}
	data, _, err := c.do("POST", "/api/v3/providers/scim/", body)
	if err != nil {
		if strings.Contains(err.Error(), "400") {
			// Already exists — fetch existing pk
			existing, _, err2 := c.do("GET", "/api/v3/providers/scim/?search="+name, nil)
			if err2 != nil {
				return 0, err2
			}
			item, err2 := firstResult(existing)
			if err2 != nil {
				return 0, fmt.Errorf("scim provider %q: %w", name, err)
			}
			pkFloat, _ := item["pk"].(float64)
			return int(pkFloat), nil
		}
		return 0, err
	}
	pkFloat, _ := data["pk"].(float64)
	return int(pkFloat), nil
}

// GetSCIMSyncStatus returns the sync status of an Authentik SCIM provider.
// Authentik 2026.x does not expose a full-sync trigger endpoint — sync is event-driven.
func (c *AuthentikClient) GetSCIMSyncStatus(providerPK int) (map[string]interface{}, error) {
	data, _, err := c.do("GET", fmt.Sprintf("/api/v3/providers/scim/%d/sync/status/", providerPK), nil)
	return data, err
}

// SetApplicationBackchannelProviders adds providerPKs to the backchannel_providers list
// of an existing application. This is required for Authentik outbound SCIM to function —
// without a backchannel assignment, the SCIM provider never syncs.
func (c *AuthentikClient) SetApplicationBackchannelProviders(appSlug string, providerPKs []int) error {
	// Fetch current backchannel list so we don't overwrite existing entries.
	existing, _, err := c.do("GET", "/api/v3/core/applications/"+appSlug+"/", nil)
	if err != nil {
		return fmt.Errorf("get application %q: %w", appSlug, err)
	}
	// Merge existing backchannel_providers with new ones.
	pkSet := map[int]bool{}
	if current, ok := existing["backchannel_providers"].([]interface{}); ok {
		for _, v := range current {
			if f, ok := v.(float64); ok {
				pkSet[int(f)] = true
			}
		}
	}
	for _, pk := range providerPKs {
		pkSet[pk] = true
	}
	merged := make([]int, 0, len(pkSet))
	for pk := range pkSet {
		merged = append(merged, pk)
	}
	// Need provider PK for the PATCH — fetch it.
	providerPK, _ := existing["provider"].(float64)
	_, _, err = c.do("PATCH", "/api/v3/core/applications/"+appSlug+"/", map[string]interface{}{
		"name":                  existing["name"],
		"slug":                  appSlug,
		"provider":              int(providerPK),
		"backchannel_providers": merged,
	})
	return err
}

// DeleteSCIMProviderByName deletes an Authentik SCIM provider by name. No-op if not found.
func (c *AuthentikClient) DeleteSCIMProviderByName(name string) error {
	data, _, err := c.do("GET", "/api/v3/providers/scim/?search="+name, nil)
	if err != nil {
		return err
	}
	results, _ := data["results"].([]interface{})
	for _, r := range results {
		item, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		if item["name"] == name {
			pkFloat, _ := item["pk"].(float64)
			pk := int(pkFloat)
			_, _, err = c.do("DELETE", fmt.Sprintf("/api/v3/providers/scim/%d/", pk), nil)
			if err != nil && strings.Contains(err.Error(), "404") {
				return nil
			}
			return err
		}
	}
	return nil
}

// GetSCIMProviderByName returns the PK and current token of an existing SCIM provider,
// or (0, "", nil) if not found.
func (c *AuthentikClient) GetSCIMProviderByName(name string) (pk int, currentToken string, err error) {
	data, _, err := c.do("GET", "/api/v3/providers/scim/?search="+name, nil)
	if err != nil {
		return 0, "", err
	}
	results, _ := data["results"].([]interface{})
	for _, r := range results {
		item, ok := r.(map[string]interface{})
		if !ok || item["name"] != name {
			continue
		}
		pkFloat, _ := item["pk"].(float64)
		token, _ := item["token"].(string)
		return int(pkFloat), token, nil
	}
	return 0, "", nil
}

// UpdateSCIMProvider patches an existing Authentik SCIM provider with a new bearer token.
func (c *AuthentikClient) UpdateSCIMProvider(pk int, name, baseURL, token string, userMappingPKs, groupMappingPKs []string) error {
	_, _, err := c.do("PATCH", fmt.Sprintf("/api/v3/providers/scim/%d/", pk), map[string]interface{}{
		"name":                    name,
		"url":                     baseURL,
		"token":                   token,
		"property_mappings":       userMappingPKs,
		"property_mappings_group": groupMappingPKs,
		"compatibility_mode":      "aws",
	})
	return err
}

// UpsertSCIMProvider creates the SCIM provider if it doesn't exist, or updates its
// token if it does. Returns the provider PK. Use this instead of CreateSCIMProvider
// for idempotent enable/update flows — avoids orphaned providers after "hal delete".
func (c *AuthentikClient) UpsertSCIMProvider(name, baseURL, token string, userMappingPKs, groupMappingPKs []string) (int, error) {
	existingPK, _, err := c.GetSCIMProviderByName(name)
	if err != nil {
		return 0, fmt.Errorf("lookup scim provider %q: %w", name, err)
	}
	if existingPK != 0 {
		if err := c.UpdateSCIMProvider(existingPK, name, baseURL, token, userMappingPKs, groupMappingPKs); err != nil {
			return 0, fmt.Errorf("update scim provider %q: %w", name, err)
		}
		return existingPK, nil
	}
	return c.CreateSCIMProvider(name, baseURL, token, userMappingPKs, groupMappingPKs)
}

// AuthentikGroup holds the minimal fields needed for per-object SCIM sync.
type AuthentikGroup struct {
	PK   string
	Name string
}

// AuthentikUser holds the minimal fields needed for per-object SCIM sync.
type AuthentikUser struct {
	PK       string
	Username string
}

// GetAllUsers returns all non-service users in Authentik.
func (c *AuthentikClient) GetAllUsers() ([]AuthentikUser, error) {
	var users []AuthentikUser
	nextURL := "/api/v3/core/users/?page_size=100&type=internal"
	for nextURL != "" {
		data, _, err := c.do("GET", nextURL, nil)
		if err != nil {
			return nil, err
		}
		results, _ := data["results"].([]interface{})
		for _, r := range results {
			item, ok := r.(map[string]interface{})
			if !ok {
				continue
			}
			// pk may be a float64 (JSON number) — convert to string.
			var pk string
			switch v := item["pk"].(type) {
			case string:
				pk = v
			case float64:
				pk = fmt.Sprintf("%.0f", v)
			}
			username, _ := item["username"].(string)
			users = append(users, AuthentikUser{PK: pk, Username: username})
		}
		nextRaw, _ := data["next"].(string)
		if nextRaw == "" || nextRaw == "null" {
			break
		}
		nextURL = strings.TrimPrefix(nextRaw, c.baseURL)
	}
	return users, nil
}

// GetAllGroups returns all groups in Authentik.
func (c *AuthentikClient) GetAllGroups() ([]AuthentikGroup, error) {
	var groups []AuthentikGroup
	nextURL := "/api/v3/core/groups/?page_size=100"
	for nextURL != "" {
		data, _, err := c.do("GET", nextURL, nil)
		if err != nil {
			return nil, err
		}
		results, _ := data["results"].([]interface{})
		for _, r := range results {
			item, ok := r.(map[string]interface{})
			if !ok {
				continue
			}
			pk, _ := item["pk"].(string)
			name, _ := item["name"].(string)
			groups = append(groups, AuthentikGroup{PK: pk, Name: name})
		}
		// Authentik pagination: "next" is a full URL or null
		nextRaw, _ := data["next"].(string)
		if nextRaw == "" || nextRaw == "null" {
			break
		}
		// Strip base URL — do() prepends it
		nextURL = strings.TrimPrefix(nextRaw, c.baseURL)
	}
	return groups, nil
}

// SyncSCIMObject triggers a per-object SCIM sync for a single Authentik object.
// model should be "authentik.core.models.Group" or "authentik.core.models.User".
func (c *AuthentikClient) SyncSCIMObject(providerPK int, model, objectPK string) error {
	_, _, err := c.do("POST", fmt.Sprintf("/api/v3/providers/scim/%d/sync/object/", providerPK),
		map[string]interface{}{
			"sync_object_model": model,
			"sync_object_id":    objectPK,
		})
	return err
}
