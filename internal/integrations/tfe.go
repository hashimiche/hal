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

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(buf))
	if err != nil {
		return "", nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := tfeHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, 0, err
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
