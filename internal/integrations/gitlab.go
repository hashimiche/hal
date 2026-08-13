package integrations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"hal/internal/global"
)

// GitLabContainerName is the shared singleton container HAL runs for every
// GitLab-backed lab (vault jwt, terraform vcs-workflow, ...).
const GitLabContainerName = "hal-gitlab"

// DefaultGitLabPort is the host/container port used when a caller does not
// request a specific one.
const DefaultGitLabPort = 8080

// GitLabHandle describes the shared GitLab singleton resolved for a caller.
// Port is the port the live container is actually reachable on, and the base
// URLs are pre-built so every product targets the same instance consistently.
type GitLabHandle struct {
	Port int
	// HostBaseURL is reachable from the host (HAL CLI), e.g. http://127.0.0.1:<port>.
	HostBaseURL string
	// AliasBaseURL is reachable from other containers via the hal-net alias,
	// e.g. http://gitlab.localhost:<port>. This is also GitLab's external_url,
	// so it doubles as the OIDC/JWT issuer.
	AliasBaseURL string
	// ContainerBaseURL is reachable from other containers via the container
	// name, e.g. http://hal-gitlab:<port> (used for GitLab Runner registration).
	ContainerBaseURL string
	// Reused is true when the container was already running and this call only
	// attached to it (the requested port was ignored in favor of the live one).
	Reused bool
}

// gitLabBaseURLs builds a GitLabHandle's URL fields for a resolved port.
func gitLabBaseURLs(port int, reused bool) *GitLabHandle {
	if port <= 0 {
		port = DefaultGitLabPort
	}
	return &GitLabHandle{
		Port:             port,
		HostBaseURL:      fmt.Sprintf("http://127.0.0.1:%d", port),
		AliasBaseURL:     fmt.Sprintf("http://gitlab.localhost:%d", port),
		ContainerBaseURL: fmt.Sprintf("http://%s:%d", GitLabContainerName, port),
		Reused:           reused,
	}
}

func WaitForGitLab(baseURL string, maxRetries int) error {
	client := http.Client{Timeout: 3 * time.Second}
	for i := 0; i < maxRetries; i++ {
		resp, err := client.Get(baseURL + "/users/sign_in")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("timeout")
}

func GitLabPasswordToken(urlStr, username, password string) (string, error) {
	client := http.Client{Timeout: 5 * time.Second}
	for i := 0; i < 5; i++ {
		resp, err := client.PostForm(urlStr, url.Values{
			"grant_type": {"password"},
			"username":   {username},
			"password":   {password},
		})
		if err == nil && resp.StatusCode == 200 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			var result map[string]interface{}
			_ = json.Unmarshal(body, &result)
			token, ok := result["access_token"].(string)
			if ok && token != "" {
				return token, nil
			}
			return "", fmt.Errorf("missing access token in response")
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(5 * time.Second)
	}
	return "", fmt.Errorf("failed to retrieve gitlab token")
}

func GitLabPost(urlStr, token string, payload map[string]interface{}) ([]byte, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", urlStr, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return body, fmt.Errorf("gitlab api returned status %d", resp.StatusCode)
	}

	return body, nil
}

func GitLabGet(urlStr, token string) ([]byte, error) {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return body, fmt.Errorf("gitlab api returned status %d", resp.StatusCode)
	}

	return body, nil
}

// versionLikeName matches a reference component that looks like a bare version
// tag (e.g. "18.11.9-ce.0", "v1.2.3") rather than an image repository.
var versionLikeName = regexp.MustCompile(`^v?\d+([.\-][0-9A-Za-z.\-]+)*$`)

// imageLooksLikeBareTag reports whether image is almost certainly a version tag
// that was mistakenly passed as a full image reference (the classic
// "18.11.9-ce.0" -> pulled as "18.11.9-ce.0:latest" failure).
func imageLooksLikeBareTag(image string) bool {
	name := image
	if i := strings.LastIndex(name, "@"); i >= 0 {
		name = name[:i]
	}
	if i := strings.LastIndex(name, ":"); i >= 0 {
		name = name[:i]
	}
	if name == "" || strings.Contains(name, "/") {
		return false
	}
	return versionLikeName.MatchString(name)
}

// EnsureImageAvailable makes sure a container image can actually be used before
// a caller tries to run it. It fails fast with an actionable message when the
// reference is malformed (e.g. a version passed as an image) and otherwise
// ensures the image is present locally, pulling it if needed. Pull progress is
// streamed because base images (GitLab CE is multi-GB) can take minutes on a
// first run, which would otherwise look like a hang.
func EnsureImageAvailable(engine, image string) error {
	image = strings.TrimSpace(image)
	if image == "" || strings.HasPrefix(image, ":") {
		return fmt.Errorf("no container image was resolved (check --gitlab-image / --gitlab-tag)")
	}

	if imageLooksLikeBareTag(image) {
		return fmt.Errorf("%q looks like a version tag, not a full image reference — did you mean 'gitlab/gitlab-ce:%s'? Set --gitlab-image and --gitlab-tag", image, image)
	}

	// Already present locally: nothing to do.
	if exec.Command(engine, "image", "inspect", image).Run() == nil {
		return nil
	}

	fmt.Printf("⬇️  Pulling image %s (first run can take a few minutes)...\n", image)
	cmd := exec.Command(engine, "pull", image)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("could not pull image %q — verify the reference is correct (e.g. --gitlab-image gitlab/gitlab-ce --gitlab-tag <valid-tag>) and that the registry is reachable", image)
	}
	return nil
}

func EnsureGitLabCE(engine, image, rootPassword string, port int) (bool, error) {
	if out, err := exec.Command(engine, "inspect", "-f", "{{.State.Running}}", GitLabContainerName).Output(); err == nil {
		if string(bytes.TrimSpace(out)) == "true" {
			return true, nil
		}
	}

	if port <= 0 {
		port = DefaultGitLabPort
	}

	// Preflight the image before attempting to run it, so a malformed reference
	// or an unreachable tag surfaces as a clear, actionable message instead of a
	// raw engine "pull access denied" error mid-boot.
	if err := EnsureImageAvailable(engine, image); err != nil {
		return false, err
	}

	// 🎯 FIX: Resolve the forged certificate and mount it
	homeDir, _ := os.UserHomeDir()
	certPath := filepath.Join(homeDir, ".hal", "tfe-certs", "cert.pem")

	args := []string{
		"run", "-d", "--name", GitLabContainerName,
		"--network", "hal-net",
		"--network-alias", "gitlab.localhost",
		"-p", fmt.Sprintf("%d:%d", port, port),
		"--shm-size", "256m",
		"--privileged",
		"-v", fmt.Sprintf("%s:/etc/gitlab/trusted-certs/tfe.localhost.crt:z", certPath), // 🎯 FIX: Inject CA
		"-e", fmt.Sprintf("GITLAB_OMNIBUS_CONFIG=external_url 'http://gitlab.localhost:%d'; nginx['listen_port'] = %d; nginx['listen_addresses'] = ['0.0.0.0', '[::]']; puma['port'] = 8081; gitlab_rails['initial_root_password'] = '%s';", port, port, rootPassword),
		image,
	}

	if out, err := exec.Command(engine, args...).CombinedOutput(); err != nil {
		return false, fmt.Errorf("failed to start GitLab: %s", string(out))
	}

	return false, nil
}

// IsGitLabRunning reports whether the shared hal-gitlab container is running.
func IsGitLabRunning(engine string) bool {
	out, err := exec.Command(engine, "inspect", "-f", "{{.State.Running}}", GitLabContainerName).Output()
	if err != nil {
		return false
	}
	return string(bytes.TrimSpace(out)) == "true"
}

// DetectGitLabPort returns the port the live hal-gitlab container is published
// on. HAL always publishes GitLab as <port>:<port> (host==container) and makes
// nginx listen on that same port, so the single bound host port is the port
// every consumer must use. Returns 0 when it cannot be determined.
func DetectGitLabPort(engine string) int {
	out, err := exec.Command(engine, "inspect", "-f",
		`{{range $p, $conf := .NetworkSettings.Ports}}{{if $conf}}{{(index $conf 0).HostPort}} {{end}}{{end}}`,
		GitLabContainerName).Output()
	if err != nil {
		return 0
	}
	for _, field := range strings.Fields(string(out)) {
		if p, convErr := strconv.Atoi(field); convErr == nil && p > 0 {
			return p
		}
	}
	return 0
}

// EnsureSharedGitLab guarantees the shared hal-gitlab singleton is running and
// returns a handle describing the port and endpoints it is actually reachable
// on. This is the single bring-up path every GitLab-backed product should use
// (vault jwt, terraform vcs-workflow, ...).
//
// GitLab is a shared service: when it is already running, requestedPort is
// ignored and the live published port is detected and returned, so a lab
// spun up by one product (e.g. terraform vcs-workflow on a custom port) is
// consumed correctly by another (e.g. vault jwt) without port drift. When it
// is not running, it is booted on requestedPort (defaulting to 8080).
//
// When waitSeconds > 0 the call also blocks until the GitLab API answers on the
// resolved host URL (or times out). Pass 0 to skip waiting (e.g. to print a
// progress message before waiting yourself).
func EnsureSharedGitLab(engine, image, rootPassword string, requestedPort, waitSeconds int) (*GitLabHandle, error) {
	if requestedPort <= 0 {
		requestedPort = DefaultGitLabPort
	}

	reused, err := EnsureGitLabCE(engine, image, rootPassword, requestedPort)
	if err != nil {
		return nil, err
	}

	port := requestedPort
	if reused {
		if detected := DetectGitLabPort(engine); detected > 0 {
			port = detected
		}
	}

	handle := gitLabBaseURLs(port, reused)

	if waitSeconds > 0 {
		if err := WaitForGitLab(handle.HostBaseURL, waitSeconds); err != nil {
			return handle, fmt.Errorf("GitLab failed to become ready in time")
		}
	}

	return handle, nil
}

// StopSharedGitLabIfUnused removes hal-gitlab when no registry consumers remain
// and no TFE runtime is still running. Stale TFE VCS/SAML consumers must be
// pruned first via global.PruneStaleTFEBackedSharedConsumers.
func StopSharedGitLabIfUnused(engine string) {
	remaining := global.GetSharedServiceConsumers(global.SharedGitLabServiceKey)
	if len(remaining) > 0 {
		fmt.Printf("  ℹ️  Shared GitLab left running (still used by: %s)\n", strings.Join(remaining, ", "))
		return
	}
	if global.IsTFERuntimeRunning(engine) {
		fmt.Println("  ℹ️  Shared GitLab left running (a Terraform Enterprise runtime is still active)")
		return
	}

	if global.IsContainerRunning(engine, GitLabContainerName) {
		if out, rmErr := exec.Command(engine, "rm", "-f", GitLabContainerName).CombinedOutput(); rmErr != nil {
			fmt.Printf("⚠️  Failed to remove shared GitLab: %s\n", strings.TrimSpace(string(out)))
			return
		}
		fmt.Println("  🧹 Stopped shared GitLab (no remaining consumers).")
	}
	_ = global.ClearSharedService(global.SharedGitLabServiceKey)
}
