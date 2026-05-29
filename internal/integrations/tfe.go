package integrations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"crypto/tls"
)

const tfeJSONAPI = "application/vnd.api+json"

func tfeHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	return &http.Client{Timeout: 20 * time.Second, Transport: transport}
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
		return respBody, resp.StatusCode, fmt.Errorf("tfe api returned status %d", resp.StatusCode)
	}

	return respBody, resp.StatusCode, nil
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
