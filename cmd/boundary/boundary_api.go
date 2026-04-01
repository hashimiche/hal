package boundary

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type BoundaryClient struct {
	Address string
	Token   string
	Client  *http.Client
}

func normalizeBoundaryCreatePayload(endpoint string, payload map[string]interface{}) map[string]interface{} {
	normalized := make(map[string]interface{}, len(payload))
	for k, v := range payload {
		normalized[k] = v
	}

	attributes, _ := normalized["attributes"].(map[string]interface{})
	if attributes == nil {
		attributes = map[string]interface{}{}
	}

	moveToAttributes := func(key string) {
		if val, ok := normalized[key]; ok {
			attributes[key] = val
			delete(normalized, key)
		}
	}

	switch endpoint {
	case "hosts":
		moveToAttributes("address")
	case "accounts":
		moveToAttributes("login_name")
		moveToAttributes("password")
	case "targets":
		moveToAttributes("default_port")
	}

	if len(attributes) > 0 {
		normalized["attributes"] = attributes
	}

	return normalized
}

// Helper to grab the dev-mode auth method ID
func (b *BoundaryClient) GetDevAuthMethodID() (string, error) {
	resp, err := b.Client.Get(fmt.Sprintf("%s/v1/auth-methods?scope_id=global", b.Address))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to list auth methods (%d): %s", resp.StatusCode, string(errBody))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode auth methods response: %v", err)
	}

	itemsRaw, ok := result["items"]
	if !ok || itemsRaw == nil {
		return "", fmt.Errorf("dev auth method not found")
	}

	items, ok := itemsRaw.([]interface{})
	if !ok {
		return "", fmt.Errorf("unexpected auth methods response shape")
	}

	for _, item := range items {
		method, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		methodType, _ := method["type"].(string)
		isPrimary, _ := method["is_primary"].(bool)
		id, _ := method["id"].(string)
		if methodType == "password" && isPrimary && id != "" {
			return id, nil
		}
	}

	for _, item := range items {
		method, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		methodType, _ := method["type"].(string)
		id, _ := method["id"].(string)
		if methodType == "password" && id != "" {
			return id, nil
		}
	}

	// Fallback for older/newer dev setups where field shape differs.
	for _, item := range items {
		method, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		id, _ := method["id"].(string)
		name, _ := method["name"].(string)
		if strings.HasPrefix(id, "ampw_") || strings.Contains(strings.ToLower(name), "password") {
			if id != "" {
				return id, nil
			}
		}
	}

	return "", fmt.Errorf("dev auth method not found")
}

func (b *BoundaryClient) Authenticate(authMethodID, username, password string) error {
	payload := map[string]interface{}{
		"attributes": map[string]interface{}{
			"login_name": username,
			"password":   password,
		},
	}
	body, _ := json.Marshal(payload)

	resp, err := b.Client.Post(fmt.Sprintf("%s/v1/auth-methods/%s:authenticate", b.Address, authMethodID), "application/json", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("authentication failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("authentication failed (%d): %s", resp.StatusCode, string(errBody))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("authentication failed: invalid response: %v", err)
	}

	attributes, ok := result["attributes"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("authentication failed: token attributes missing")
	}

	token, ok := attributes["token"].(string)
	if !ok || strings.TrimSpace(token) == "" {
		return fmt.Errorf("authentication failed: token missing")
	}

	b.Token = token
	return nil
}

// The generic magic function that creates ANY Boundary resource
func (b *BoundaryClient) CreateResource(endpoint string, payload map[string]interface{}) (string, error) {
	normalizedPayload := normalizeBoundaryCreatePayload(endpoint, payload)
	body, _ := json.Marshal(normalizedPayload)
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/v1/%s", b.Address, endpoint), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.Token)

	resp, err := b.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API error (%d): %s", resp.StatusCode, string(errBody))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode create response: %v", err)
	}

	if id, ok := result["id"].(string); ok && strings.TrimSpace(id) != "" {
		return id, nil
	}

	if item, ok := result["item"].(map[string]interface{}); ok {
		if id, ok := item["id"].(string); ok && strings.TrimSpace(id) != "" {
			return id, nil
		}
	}

	return "", fmt.Errorf("create response missing id")
}

// Specific helper for adding hosts to sets, or sets to targets
func (b *BoundaryClient) AddResourceAction(endpoint string, id string, action string, payload map[string]interface{}) error {
	actionPayload := make(map[string]interface{}, len(payload)+1)
	for k, v := range payload {
		actionPayload[k] = v
	}

	if _, hasVersion := actionPayload["version"]; !hasVersion {
		version, err := b.GetResourceVersion(endpoint, id)
		if err == nil && version > 0 {
			actionPayload["version"] = version
		}
	}

	body, _ := json.Marshal(actionPayload)
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/v1/%s/%s:%s", b.Address, endpoint, id, action), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.Token)

	resp, err := b.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute action %s on %s: %v", action, id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to execute action %s on %s (%d): %s", action, id, resp.StatusCode, string(errBody))
	}
	return nil
}

func (b *BoundaryClient) GetResourceVersion(endpoint, id string) (int, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/v1/%s/%s", b.Address, endpoint, id), nil)
	req.Header.Set("Authorization", "Bearer "+b.Token)

	resp, err := b.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(errBody))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	versionFloat, ok := result["version"].(float64)
	if !ok {
		return 0, fmt.Errorf("resource version missing for %s/%s", endpoint, id)
	}

	return int(versionFloat), nil
}

func (b *BoundaryClient) ListResources(endpoint string, params map[string]string) ([]map[string]interface{}, error) {
	query := url.Values{}
	for k, v := range params {
		query.Set(k, v)
	}

	url := fmt.Sprintf("%s/v1/%s", b.Address, endpoint)
	if len(query) > 0 {
		url = url + "?" + query.Encode()
	}

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.Token)

	resp, err := b.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(errBody))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	itemsRaw, ok := result["items"]
	if !ok || itemsRaw == nil {
		return []map[string]interface{}{}, nil
	}

	itemsList, ok := itemsRaw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected API response shape for %s", endpoint)
	}

	items := make([]map[string]interface{}, 0, len(itemsList))
	for _, raw := range itemsList {
		item, ok := raw.(map[string]interface{})
		if ok {
			items = append(items, item)
		}
	}

	return items, nil
}

func (b *BoundaryClient) FindResourceIDByField(endpoint, field, value string, params map[string]string) (string, error) {
	items, err := b.ListResources(endpoint, params)
	if err != nil {
		return "", err
	}

	for _, item := range items {
		if itemValue, ok := item[field].(string); ok && itemValue == value {
			if id, ok := item["id"].(string); ok {
				return id, nil
			}
		}

		if attrs, ok := item["attributes"].(map[string]interface{}); ok {
			if attrValue, ok := attrs[field].(string); ok && attrValue == value {
				if id, ok := item["id"].(string); ok {
					return id, nil
				}
			}
		}
	}

	return "", nil
}

func (b *BoundaryClient) DeleteResource(endpoint, id string) error {
	req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/v1/%s/%s", b.Address, endpoint, id), nil)
	req.Header.Set("Authorization", "Bearer "+b.Token)

	resp, err := b.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (%d): %s", resp.StatusCode, string(errBody))
	}

	return nil
}

func (b *BoundaryClient) CreateOrGetResource(endpoint string, payload map[string]interface{}, lookupField string, params map[string]string) (string, error) {
	id, err := b.CreateResource(endpoint, payload)
	if err == nil {
		return id, nil
	}

	errMsg := strings.ToLower(err.Error())
	if !strings.Contains(errMsg, "already") && !strings.Contains(errMsg, "duplicate") && !strings.Contains(errMsg, "exists") {
		return "", err
	}

	lookupValue, ok := payload[lookupField].(string)
	if !ok || strings.TrimSpace(lookupValue) == "" {
		return "", err
	}

	existingID, findErr := b.FindResourceIDByField(endpoint, lookupField, lookupValue, params)
	if findErr != nil {
		return "", findErr
	}
	if existingID == "" {
		return "", err
	}

	return existingID, nil
}
