package global

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	grafanaURL    = "http://127.0.0.1:3000"
	halFolderUID  = "hal"
	halFolderName = "HAL"
	promDSUID     = "hal-prometheus"
)

type grafanaFolder struct {
	ID    int64  `json:"id"`
	UID   string `json:"uid"`
	Title string `json:"title"`
}

func obsBaseDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".hal", "obs")
}

func ObsTargetsDir() string {
	base := obsBaseDir()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "targets")
}

func ObsDashboardsDir() string {
	base := obsBaseDir()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "dashboards")
}

func RemoveObsState() error {
	base := obsBaseDir()
	if base == "" {
		return nil
	}
	if err := os.RemoveAll(base); err != nil {
		return err
	}
	return nil
}

func IsObsRunning(engine string) bool {
	out, _ := exec.Command(engine, "ps", "-q", "-f", "name=hal-prometheus$").Output()
	return strings.TrimSpace(string(out)) != ""
}

func IsObsReady(engine string) bool {
	return IsObsRunning(engine) && isGrafanaRunning(engine)
}

func ObsMissingComponents(engine string) []string {
	missing := []string{}
	if !IsObsRunning(engine) {
		missing = append(missing, "Prometheus")
	}
	if !isGrafanaRunning(engine) {
		missing = append(missing, "Grafana")
	}
	return missing
}

func isGrafanaRunning(engine string) bool {
	out, _ := exec.Command(engine, "ps", "-q", "-f", "name=hal-grafana$").Output()
	return strings.TrimSpace(string(out)) != ""
}

func RegisterObsArtifacts(product string, targets []string) []string {
	warnings := []string{}

	if err := EnsureGrafanaDashboardIfKnown(product); err != nil {
		warnings = append(warnings, fmt.Sprintf("dashboard provisioning skipped: %v", err))
	}

	engine, err := DetectEngine()
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("observability target registration skipped: %v", err))
		return warnings
	}

	if err := UpsertObsPromTargetIfRunning(engine, product, targets); err != nil {
		warnings = append(warnings, fmt.Sprintf("observability target registration skipped: %v", err))
	}

	if isGrafanaRunning(engine) {
		if err := ImportGrafanaDashboardIfKnown(product); err != nil {
			warnings = append(warnings, fmt.Sprintf("dashboard import skipped: %v", err))
		}
	}

	return warnings
}

func UpsertObsPromTargetIfRunning(engine, product string, targets []string) error {
	if !IsObsRunning(engine) {
		return nil
	}

	targetsDir := ObsTargetsDir()
	if targetsDir == "" {
		return fmt.Errorf("failed to resolve observability targets directory")
	}
	if err := os.MkdirAll(targetsDir, 0o755); err != nil {
		return err
	}

	payload := []map[string]interface{}{
		{
			"targets": targets,
			"labels": map[string]string{
				"job": product,
			},
		},
	}

	buf, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(targetsDir, product+".json"), buf, 0o644); err != nil {
		return err
	}

	return nil
}

func RemoveObsPromTargetFile(product string) error {
	targetsDir := ObsTargetsDir()
	if targetsDir == "" {
		return nil
	}

	targetPath := filepath.Join(targetsDir, product+".json")
	if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func EnsureGrafanaDashboardIfKnown(product string) error {
	dashboardIDByProduct := map[string]int{
		"terraform": 15630,
		"nomad":     15764,
		"vault":     12904,
		"consul":    13396,
	}

	dashboardID, ok := dashboardIDByProduct[product]
	if !ok {
		return nil
	}

	dashboardsDir := ObsDashboardsDir()
	if dashboardsDir == "" {
		return fmt.Errorf("failed to resolve observability dashboards directory")
	}
	if err := os.MkdirAll(dashboardsDir, 0o755); err != nil {
		return err
	}

	dashboardPath := filepath.Join(dashboardsDir, product+".json")
	if _, err := os.Stat(dashboardPath); err == nil {
		if err := normalizeDashboardFile(dashboardPath); err != nil {
			return err
		}
		return nil
	}

	downloadURL := fmt.Sprintf("https://grafana.com/api/dashboards/%d/revisions/latest/download", dashboardID)
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("dashboard download failed with status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	normalizedBody, err := normalizeDashboardJSON(body)
	if err != nil {
		return err
	}

	return os.WriteFile(dashboardPath, normalizedBody, 0o644)
}

func ImportGrafanaDashboardIfKnown(product string) error {
	dashboardIDByProduct := map[string]int{
		"terraform": 15630,
		"nomad":     15764,
		"vault":     12904,
		"consul":    13396,
	}

	if _, ok := dashboardIDByProduct[product]; !ok {
		return nil
	}

	dashboardsDir := ObsDashboardsDir()
	if dashboardsDir == "" {
		return fmt.Errorf("failed to resolve observability dashboards directory")
	}

	dashboardPath := filepath.Join(dashboardsDir, product+".json")
	body, err := os.ReadFile(dashboardPath)
	if err != nil {
		return err
	}

	normalizedBody, err := normalizeDashboardJSON(body)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dashboardPath, normalizedBody, 0o644); err != nil {
		return err
	}

	var dashboard map[string]interface{}
	if err := json.Unmarshal(normalizedBody, &dashboard); err != nil {
		return err
	}

	folderUID, err := ensureGrafanaFolder(halFolderUID, halFolderName)
	if err != nil {
		return err
	}

	importPayload := map[string]interface{}{
		"dashboard": dashboard,
		"overwrite": true,
		"folderUid": folderUID,
	}
	buf, err := json.Marshal(importPayload)
	if err != nil {
		return err
	}

	client := http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequest(http.MethodPost, grafanaURL+"/api/dashboards/db", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := doGrafanaRequestWithAuthFallback(client, req, buf)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("grafana import failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	return nil
}

func ensureGrafanaFolder(uid, title string) (string, error) {
	existingUID, err := findGrafanaFolderUIDByTitle(title)
	if err != nil {
		return "", err
	}
	if existingUID != "" {
		return existingUID, nil
	}

	payload := map[string]interface{}{
		"uid":       uid,
		"title":     title,
		"overwrite": true,
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	client := http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequest(http.MethodPost, grafanaURL+"/api/folders", bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := doGrafanaRequestWithAuthFallback(client, req, buf)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("grafana folder ensure failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	return uid, nil
}

func findGrafanaFolderUIDByTitle(title string) (string, error) {
	client := http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequest(http.MethodGet, grafanaURL+"/api/folders?limit=2000", nil)
	if err != nil {
		return "", err
	}

	resp, err := doGrafanaRequestWithAuthFallback(client, req, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("grafana folders list failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var folders []grafanaFolder
	if err := json.NewDecoder(resp.Body).Decode(&folders); err != nil {
		return "", err
	}

	for _, folder := range folders {
		if folder.Title == title && folder.UID != "" {
			return folder.UID, nil
		}
	}

	return "", nil
}

func doGrafanaRequestWithAuthFallback(client http.Client, req *http.Request, body []byte) (*http.Response, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		return resp, nil
	}
	resp.Body.Close()

	reqAuth, err := http.NewRequest(req.Method, req.URL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for key, values := range req.Header {
		for _, value := range values {
			reqAuth.Header.Add(key, value)
		}
	}
	reqAuth.SetBasicAuth("admin", "admin")

	return client.Do(reqAuth)
}

func normalizeDashboardFile(path string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	normalizedBody, err := normalizeDashboardJSON(body)
	if err != nil {
		return err
	}

	return os.WriteFile(path, normalizedBody, 0o644)
}

func normalizeDashboardJSON(body []byte) ([]byte, error) {
	var dashboard map[string]interface{}
	if err := json.Unmarshal(body, &dashboard); err != nil {
		return nil, err
	}

	forcePrometheusDatasource(dashboard)

	return json.MarshalIndent(dashboard, "", "  ")
}

func forcePrometheusDatasource(node interface{}) {
	switch v := node.(type) {
	case map[string]interface{}:
		for key, value := range v {
			if key == "datasource" {
				v[key] = map[string]interface{}{
					"type": "prometheus",
					"uid":  promDSUID,
				}
				continue
			}
			forcePrometheusDatasource(value)
		}
	case []interface{}:
		for _, value := range v {
			forcePrometheusDatasource(value)
		}
	}
}
