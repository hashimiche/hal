package terraform

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hal/internal/global"
	"hal/internal/integrations"
)

type tfeFoundationConfig struct {
	BaseURL       string
	OrgName       string
	ProjectName   string
	APIToken      string
	AdminUsername string
	AdminEmail    string
	AdminPassword string
}

func ensureTFEFoundation(engine string, cfg tfeFoundationConfig) (string, string, error) {
	token := strings.TrimSpace(cfg.APIToken)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("TFE_API_TOKEN"))
	}
	if token == "" {
		token = global.LoadCachedTFEAPIToken()
	}
	if token != "" {
		if !isTFEAPITokenUsable(cfg.BaseURL, token) {
			token = ""
			_ = global.RemoveCachedTFEAPIToken()
		}
	}

	if token == "" {
		// Best-effort warmup to reduce startup races without blocking the CLI for minutes.
		_ = waitForTFECoreReadiness(engine, cfg.BaseURL, 30*time.Second)

		autoToken, err := bootstrapTFEAPIToken(engine, cfg.BaseURL, cfg.AdminUsername, cfg.AdminEmail, cfg.AdminPassword)
		if err != nil {
			return "", "", err
		}
		token = autoToken
		_ = global.CacheTFEAPIToken(token)
	}

	projectID, err := ensureTFEOrgAndProject(cfg.BaseURL, token, cfg.OrgName, cfg.ProjectName)
	if err != nil {
		return "", "", err
	}

	return token, projectID, nil
}

func waitForTFECoreReadiness(engine, baseURL string, timeout time.Duration) error {
	containerName, err := tfeCoreContainerForBaseURL(baseURL)
	if err != nil {
		return err
	}

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		vaultReady := exec.Command(
			engine,
			"exec",
			containerName,
			"bash",
			"-lc",
			"VAULT_ADDR=http://127.0.0.1:8200 vault status -format=json 2>/dev/null | grep -q '\"sealed\":false'",
		).Run() == nil

		archivistReady := exec.Command(
			engine,
			"exec",
			containerName,
			"bash",
			"-lc",
			"(echo >/dev/tcp/127.0.0.1/7675) >/dev/null 2>&1",
		).Run() == nil

		if vaultReady && archivistReady {
			return nil
		}

		time.Sleep(2 * time.Second)
	}

	return nil
}

func bootstrapTFEAPIToken(engine, baseURL, username, email, password string) (string, error) {
	if token, err := bootstrapTFEUserTokenFromContainer(engine, baseURL, username, email, "hal-auto-foundation"); err == nil {
		if isTFEAPITokenUsable(baseURL, token) {
			return token, nil
		}
	}

	return bootstrapTFEAPITokenFromIACT(engine, baseURL, username, email, password)
}

func isTFEAPITokenUsable(baseURL, token string) bool {
	body, status, err := integrations.TFERequest("GET", fmt.Sprintf("%s/api/v2/account/details", baseURL), token, nil)
	if err == nil {
		return true
	}

	if status == 401 || status == 403 {
		return false
	}

	msg := strings.ToLower(strings.TrimSpace(string(body)))
	if strings.Contains(msg, "unauthorized") || strings.Contains(msg, "forbidden") {
		return false
	}

	return true
}

func bootstrapTFEAPITokenFromIACT(engine, baseURL, username, email, password string) (string, error) {
	containerName, err := tfeCoreContainerForBaseURL(baseURL)
	if err != nil {
		return "", err
	}

	out, err := exec.Command(engine, "exec", containerName, "tfectl", "admin", "token").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to retrieve IACT token: %s", strings.TrimSpace(string(out)))
	}
	iactToken := strings.TrimSpace(string(out))
	if iactToken == "" {
		return "", fmt.Errorf("received empty IACT token")
	}

	token, body, status, err := integrations.TFECreateInitialAdmin(baseURL, iactToken, username, email, password)
	if err == nil {
		return token, nil
	}

	resp := strings.TrimSpace(string(body))
	respLower := strings.ToLower(resp)
	if status == 401 || status == 409 || status == 422 || strings.Contains(respLower, "already") || strings.Contains(respLower, "exists") || strings.Contains(respLower, "not allowed") {
		return "", fmt.Errorf("initial admin bootstrap not available on this instance; automatic token generation also failed")
	}

	if resp == "" {
		return "", fmt.Errorf("initial admin bootstrap failed (%d): %w", status, err)
	}
	return "", fmt.Errorf("initial admin bootstrap failed (%d): %s: %w", status, resp, err)
}

func bootstrapTFEUserTokenFromContainer(engine, baseURL, username, email, description string) (string, error) {
	containerName, err := tfeCoreContainerForBaseURL(baseURL)
	if err != nil {
		return "", err
	}

	usernameLiteral, _ := json.Marshal(strings.TrimSpace(username))
	emailLiteral, _ := json.Marshal(strings.TrimSpace(email))
	descriptionLiteral, _ := json.Marshal(strings.TrimSpace(description))
	rubySnippet := fmt.Sprintf("user = User.with_insensitive_username(%s).first || User.find_by!(email: %s); token = Api::V2::AuthenticationTokenCreator.new(parent: user, created_by: user, description: %s).create; puts token.token", string(usernameLiteral), string(emailLiteral), string(descriptionLiteral))
	shellScript := fmt.Sprintf("source /run/terraform-enterprise/atlas/atlas-env && source /run/terraform-enterprise/atlas/redis-env && cd /app && bundle exec rails runner %s 2>/dev/null", shellSingleQuote(rubySnippet))
	out, err := exec.Command(engine, "exec", containerName, "bash", "-lc", shellScript).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to mint user token from container runtime: %s", strings.TrimSpace(string(out)))
	}

	token := extractAtlasUserToken(string(out))
	if token == "" {
		return "", fmt.Errorf("container token mint output did not include a user token")
	}

	return token, nil
}

func tfeCoreContainerForBaseURL(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("invalid TFE base URL %q: %w", baseURL, err)
	}

	hostname := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if hostname == "" {
		return tfeCoreContainer, nil
	}

	if hostname == defaultTFETwinHostname {
		layout, layoutErr := buildTFETwinLayout()
		if layoutErr != nil {
			return "", layoutErr
		}
		return layout.CoreContainer, nil
	}

	return tfeCoreContainer, nil
}

// taskWorkerConfigTemplatePath is the in-container path of the task-worker config template that TFE
// renders into /run/terraform-enterprise/task-worker/config.hcl at boot.
const taskWorkerConfigTemplatePath = "/etc/task-worker/config.hcl.tmpl"

// writableTaskWorkerTemplatePath prepares a task-worker config template with the shared disk-cache
// mount (/tmp/terraform) flipped from read-only to writable and returns a host path to bind-mount
// over taskWorkerConfigTemplatePath inside the TFE container.
//
// Why a bind mount instead of an in-container patch: TFE renders the task-worker run config
// (/run/terraform-enterprise/task-worker/config.hcl) from this template within ~40ms of container
// start — far too early to win a post-start `exec` sed race — and this image has no supervisord to
// restart the task-worker so it could reload a late patch (PID 1 is the terraform-enterprise process
// manager; killing the task-worker does not respawn it, and supervisorctl has no socket). Delivering
// the patched template as a mount present at container-creation time is the only reliable fix.
//
// The template is extracted from the exact image tag being deployed, so it never drifts from the TFE
// version — only the readonly flag is changed. The disk cache itself cannot be removed:
// TFE_DISK_CACHE_VOLUME_NAME / TFE_DISK_CACHE_PATH are required for the Docker run pipeline.
func writableTaskWorkerTemplatePath(engine, image, hostDir string) (string, error) {
	raw, err := exec.Command(engine, "run", "--rm", "--entrypoint", "cat", image, taskWorkerConfigTemplatePath).Output()
	if err != nil {
		return "", fmt.Errorf("extract task-worker template from %s: %w", image, err)
	}

	patched := strings.ReplaceAll(string(raw), `readonly = "true"`, `readonly = "false"`)
	dst := filepath.Join(hostDir, "task-worker-config.hcl.tmpl")
	if err := os.WriteFile(dst, []byte(patched), 0o644); err != nil {
		return "", fmt.Errorf("write patched task-worker template: %w", err)
	}
	return dst, nil
}

func extractAtlasUserToken(raw string) string {
	tokenPattern := regexp.MustCompile(`[A-Za-z0-9_-]+\.atlasv1\.[A-Za-z0-9_-]+`)
	return strings.TrimSpace(tokenPattern.FindString(raw))
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func extractHexToken(raw string) string {
	tokenPattern := regexp.MustCompile(`\b[a-fA-F0-9]{64}\b`)
	return strings.TrimSpace(tokenPattern.FindString(raw))
}

func ensureTFEOrgAndProject(baseURL, apiToken, orgName, projectName string) (string, error) {
	org := strings.ToLower(strings.TrimSpace(orgName))
	if org == "" {
		return "", fmt.Errorf("organization name cannot be empty")
	}

	orgURL := fmt.Sprintf("%s/api/v2/organizations/%s", baseURL, org)
	orgBody, orgStatus, orgErr := integrations.TFERequest("GET", orgURL, apiToken, nil)
	if orgErr != nil {
		if orgStatus != 404 {
			detail := strings.TrimSpace(string(orgBody))
			if detail == "" {
				detail = orgErr.Error()
			}
			return "", fmt.Errorf("organization lookup failed (status %d): %s", orgStatus, detail)
		}

		createOrgPayload := map[string]interface{}{
			"data": map[string]interface{}{
				"type": "organizations",
				"attributes": map[string]interface{}{
					"name":  org,
					"email": "hal@localhost",
				},
			},
		}
		createOrgURL := fmt.Sprintf("%s/api/v2/organizations", baseURL)
		resp, _, err := integrations.TFERequest("POST", createOrgURL, apiToken, createOrgPayload)
		if err != nil {
			return "", fmt.Errorf("organization creation failed: %s", strings.TrimSpace(string(resp)))
		}
	}

	if strings.TrimSpace(projectName) == "" {
		// Some flows only need organization + token bootstrap and intentionally do not
		// require creating a default project.
		return "", nil
	}

	listURL := fmt.Sprintf("%s/api/v2/organizations/%s/projects", baseURL, org)
	body, _, err := integrations.TFERequest("GET", listURL, apiToken, nil)
	if err != nil {
		return "", fmt.Errorf("project list failed: %s", strings.TrimSpace(string(body)))
	}

	var listResp map[string]interface{}
	_ = json.Unmarshal(body, &listResp)
	if data, ok := listResp["data"].([]interface{}); ok {
		for _, item := range data {
			proj, _ := item.(map[string]interface{})
			attrs, _ := proj["attributes"].(map[string]interface{})
			if fmt.Sprintf("%v", attrs["name"]) == projectName {
				return fmt.Sprintf("%v", proj["id"]), nil
			}
		}
	}

	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "projects",
			"attributes": map[string]interface{}{
				"name": projectName,
			},
		},
	}
	createURL := fmt.Sprintf("%s/api/v2/organizations/%s/projects", baseURL, org)
	createBody, _, createErr := integrations.TFERequest("POST", createURL, apiToken, payload)
	if createErr != nil {
		return "", fmt.Errorf("project creation failed: %s", strings.TrimSpace(string(createBody)))
	}

	var createResp map[string]interface{}
	_ = json.Unmarshal(createBody, &createResp)
	data, _ := createResp["data"].(map[string]interface{})
	return fmt.Sprintf("%v", data["id"]), nil
}
