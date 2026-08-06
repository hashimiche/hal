package integrations

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const tfeJSONAPI = "application/vnd.api+json"

func tfeHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, tfeLoopbackDialAddress(address))
	}
	return &http.Client{Timeout: 20 * time.Second, Transport: transport}
}

// tfeLoopbackDialAddress makes RFC 6761 localhost subdomains portable across
// hosts where the OS resolver does not map names such as tfe.localhost to the
// loopback interface. The request URL and TLS server name remain unchanged.
func tfeLoopbackDialAddress(address string) string {
	host, port, err := net.SplitHostPort(address)
	if err != nil || !strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return address
	}
	return net.JoinHostPort("127.0.0.1", port)
}

func TFERequest(method, urlStr, token string, payload interface{}) ([]byte, int, error) {
	var body io.Reader
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, err
		}
		body = bytes.NewReader(buf)
	}

	req, err := http.NewRequest(method, urlStr, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", tfeJSONAPI)
	req.Header.Set("Content-Type", tfeJSONAPI)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := tfeHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := strings.TrimSpace(string(respBody))
		if len(detail) > 2048 {
			detail = detail[:2048] + "..."
		}
		if detail != "" {
			return respBody, resp.StatusCode, fmt.Errorf("tfe api returned status %d: %s", resp.StatusCode, detail)
		}
		return respBody, resp.StatusCode, fmt.Errorf("tfe api returned status %d", resp.StatusCode)
	}

	return respBody, resp.StatusCode, nil
}

// GetTFEOIDCIssuer reads TFE's public workload-identity discovery document and
// returns the exact issuer value embedded in generated JWTs.
func GetTFEOIDCIssuer(baseURL string) (string, error) {
	discoveryURL := strings.TrimRight(baseURL, "/") + "/.well-known/openid-configuration"
	body, err := getTFEOIDCDiscovery(discoveryURL, "")
	if err != nil {
		parsed, parseErr := url.Parse(discoveryURL)
		if parseErr == nil && strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".localhost") {
			originalHost := parsed.Host
			if parsed.Port() == "" {
				parsed.Host = "127.0.0.1"
			} else {
				parsed.Host = net.JoinHostPort("127.0.0.1", parsed.Port())
			}
			body, err = getTFEOIDCDiscovery(parsed.String(), originalHost)
		}
	}
	if err != nil {
		return "", err
	}
	var discovery struct {
		Issuer string `json:"issuer"`
	}
	if err := json.Unmarshal(body, &discovery); err != nil {
		return "", fmt.Errorf("parse OIDC discovery response: %w", err)
	}
	issuer := strings.TrimRight(strings.TrimSpace(discovery.Issuer), "/")
	if issuer == "" {
		return "", fmt.Errorf("OIDC discovery response did not include an issuer")
	}
	return issuer, nil
}

func getTFEOIDCDiscovery(discoveryURL, hostHeader string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, discoveryURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if hostHeader != "" {
		req.Host = hostHeader
	}
	resp, err := tfeHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tfe OIDC discovery returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// GetTFEVersion returns the TFE version string from the X-TFE-Version response
// header exposed by GET /api/v2/ping. Returns an empty string when the header
// is absent (very old builds) rather than an error.
func GetTFEVersion(baseURL, apiToken string) (string, error) {
	req, err := http.NewRequest("GET", baseURL+"/api/v2/ping", nil)
	if err != nil {
		return "", err
	}
	if apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+apiToken)
	}
	req.Header.Set("Accept", tfeJSONAPI)
	resp, err := tfeHTTPClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
	return resp.Header.Get("X-TFE-Version"), nil
}

func TFECreateInitialAdmin(baseURL, iactToken, username, email, password string) (string, []byte, int, error) {
	endpoint := fmt.Sprintf("%s/admin/initial-admin-user?token=%s", baseURL, url.QueryEscape(iactToken))
	payload := map[string]string{
		"username": username,
		"email":    email,
		"password": password,
	}

	buf, err := json.Marshal(payload)
	if err != nil {
		return "", nil, 0, err
	}

	// TFE 2.x returns a 301 on the IACT endpoint.  Go's http.Client silently downgrades
	// POST→GET on 301/302, losing the request body.  The hal-tfe-proxy (nginx) already
	// rewrites the Location port back to :8443 via proxy_redirect, so we only need to
	// stop Go from auto-following and re-POST to whatever Location the proxy hands back.
	client := tfeHTTPClient()
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}

	doPost := func(target string) (*http.Response, error) {
		req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(buf))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		return client.Do(req)
	}

	resp, err := doPost(endpoint)
	if err != nil {
		return "", nil, 0, err
	}

	if resp.StatusCode == 301 || resp.StatusCode == 302 || resp.StatusCode == 307 || resp.StatusCode == 308 {
		resp.Body.Close()
		if loc := resp.Header.Get("Location"); loc != "" {
			resp, err = doPost(loc)
			if err != nil {
				return "", nil, 0, err
			}
		}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, resp.StatusCode, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", body, resp.StatusCode, fmt.Errorf("initial admin creation failed with status %d", resp.StatusCode)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", body, resp.StatusCode, err
	}

	token, _ := parsed["token"].(string)
	if token == "" {
		return "", body, resp.StatusCode, fmt.Errorf("initial admin creation response did not include token")
	}

	return token, body, resp.StatusCode, nil
}

// EnsureTFEVariableSet creates (or reconciles) a global variable set named
// setName in the given TFE organisation, then upserts each key/value in vars
// as an environment-category variable. Existing variables on the set are
// updated in place; missing ones are created.
func EnsureTFEVariableSet(baseURL, token, orgName, setName string, vars map[string]string) error {
	// 1. Find or create the variable set.
	varsetID, err := findOrCreateTFEVarSet(baseURL, token, orgName, setName)
	if err != nil {
		return err
	}

	// 2. Fetch existing variables so we can update rather than duplicate.
	existing, err := listTFEVarSetVars(baseURL, token, varsetID)
	if err != nil {
		return err
	}

	// 3. Upsert each variable.
	for k, v := range vars {
		if id, ok := existing[k]; ok {
			if err := patchTFEVarSetVar(baseURL, token, varsetID, id, k, v); err != nil {
				return fmt.Errorf("update variable %q: %w", k, err)
			}
		} else {
			if err := createTFEVarSetVar(baseURL, token, varsetID, k, v); err != nil {
				return fmt.Errorf("create variable %q: %w", k, err)
			}
		}
	}
	return nil
}

// DeleteTFEVariableSet deletes the named variable set from the given TFE
// organisation. No-op when the set does not exist.
func DeleteTFEVariableSet(baseURL, token, orgName, setName string) error {
	id, err := tfeVarSetIDByName(baseURL, token, orgName, setName)
	if err != nil {
		return err
	}
	if id == "" {
		return nil // already gone
	}
	url := fmt.Sprintf("%s/api/v2/varsets/%s", baseURL, id)
	_, _, err = TFERequest("DELETE", url, token, nil)
	return err
}

// DeleteTFEVariableSetVariable removes one managed variable from a named set.
// It is a no-op when either the set or variable does not exist.
func DeleteTFEVariableSetVariable(baseURL, token, orgName, setName, key string) error {
	varsetID, err := tfeVarSetIDByName(baseURL, token, orgName, setName)
	if err != nil {
		return err
	}
	if varsetID == "" {
		return nil
	}
	variables, err := listTFEVarSetVars(baseURL, token, varsetID)
	if err != nil {
		return err
	}
	varID, exists := variables[key]
	if !exists {
		return nil
	}
	endpoint := fmt.Sprintf("%s/api/v2/varsets/%s/relationships/vars/%s", baseURL, varsetID, varID)
	_, _, err = TFERequest(http.MethodDelete, endpoint, token, nil)
	if err != nil {
		return fmt.Errorf("delete variable %q: %w", key, err)
	}
	return nil
}

// ── internal helpers ──────────────────────────────────────────────────────────

// tfeVarSetIDByName returns the ID of the variable set with the given name in
// the org, or "" if it does not exist.
func tfeVarSetIDByName(baseURL, token, orgName, name string) (string, error) {
	pageURL := fmt.Sprintf("%s/api/v2/organizations/%s/varsets", baseURL, orgName)
	for pageURL != "" {
		body, _, err := TFERequest(http.MethodGet, pageURL, token, nil)
		if err != nil {
			return "", fmt.Errorf("list varsets: %w", err)
		}
		var resp struct {
			Data []struct {
				ID         string `json:"id"`
				Attributes struct {
					Name string `json:"name"`
				} `json:"attributes"`
			} `json:"data"`
			Links struct {
				Next *string `json:"next"`
			} `json:"links"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return "", fmt.Errorf("parse varsets response: %w", err)
		}
		for _, vs := range resp.Data {
			if vs.Attributes.Name == name {
				return vs.ID, nil
			}
		}
		pageURL, err = resolveTFENextPageURL(pageURL, resp.Links.Next)
		if err != nil {
			return "", fmt.Errorf("parse varsets next page: %w", err)
		}
	}
	return "", nil
}

// findOrCreateTFEVarSet returns the ID of the named variable set, creating it
// (global scope) if it does not already exist.
func findOrCreateTFEVarSet(baseURL, token, orgName, name string) (string, error) {
	id, err := tfeVarSetIDByName(baseURL, token, orgName, name)
	if err != nil {
		return "", err
	}
	if id != "" {
		return id, nil
	}
	url := fmt.Sprintf("%s/api/v2/organizations/%s/varsets", baseURL, orgName)
	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "varsets",
			"attributes": map[string]interface{}{
				"name":   name,
				"global": true,
			},
		},
	}
	body, _, err := TFERequest(http.MethodPost, url, token, payload)
	if err != nil {
		return "", fmt.Errorf("create varset: %w", err)
	}
	var resp struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parse create varset response: %w", err)
	}
	return resp.Data.ID, nil
}

// listTFEVarSetVars returns a map of variable key → variable ID for all
// variables currently on the given variable set.
func listTFEVarSetVars(baseURL, token, varsetID string) (map[string]string, error) {
	pageURL := fmt.Sprintf("%s/api/v2/varsets/%s/relationships/vars", baseURL, varsetID)
	out := make(map[string]string)
	for pageURL != "" {
		body, _, err := TFERequest(http.MethodGet, pageURL, token, nil)
		if err != nil {
			return nil, fmt.Errorf("list varset vars: %w", err)
		}
		var resp struct {
			Data []struct {
				ID         string `json:"id"`
				Attributes struct {
					Key string `json:"key"`
				} `json:"attributes"`
			} `json:"data"`
			Links struct {
				Next *string `json:"next"`
			} `json:"links"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("parse varset vars response: %w", err)
		}
		for _, variable := range resp.Data {
			out[variable.Attributes.Key] = variable.ID
		}
		pageURL, err = resolveTFENextPageURL(pageURL, resp.Links.Next)
		if err != nil {
			return nil, fmt.Errorf("parse varset vars next page: %w", err)
		}
	}
	return out, nil
}

func resolveTFENextPageURL(currentURL string, next *string) (string, error) {
	if next == nil || strings.TrimSpace(*next) == "" {
		return "", nil
	}
	nextURL, err := url.Parse(strings.TrimSpace(*next))
	if err != nil {
		return "", err
	}
	if nextURL.IsAbs() {
		return nextURL.String(), nil
	}
	current, err := url.Parse(currentURL)
	if err != nil {
		return "", err
	}
	return current.ResolveReference(nextURL).String(), nil
}

// createTFEVarSetVar adds a new env-category variable to the variable set.
func createTFEVarSetVar(baseURL, token, varsetID, key, value string) error {
	url := fmt.Sprintf("%s/api/v2/varsets/%s/relationships/vars", baseURL, varsetID)
	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "vars",
			"attributes": map[string]interface{}{
				"key":       key,
				"value":     value,
				"category":  "env",
				"sensitive": false,
			},
		},
	}
	_, _, err := TFERequest(http.MethodPost, url, token, payload)
	return err
}

// patchTFEVarSetVar updates an existing variable on a variable set.
func patchTFEVarSetVar(baseURL, token, varsetID, varID, key, value string) error {
	url := fmt.Sprintf("%s/api/v2/varsets/%s/relationships/vars/%s", baseURL, varsetID, varID)
	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"id":   varID,
			"type": "vars",
			"attributes": map[string]interface{}{
				"key":       key,
				"value":     value,
				"category":  "env",
				"sensitive": false,
			},
		},
	}
	_, _, err := TFERequest(http.MethodPatch, url, token, payload)
	return err
}
