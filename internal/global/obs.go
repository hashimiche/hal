package global

import (
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

func IsObsRunning(engine string) bool {
	out, _ := exec.Command(engine, "ps", "-q", "-f", "name=hal-prometheus$").Output()
	return strings.TrimSpace(string(out)) != ""
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

	if err := EnsureGrafanaDashboardIfKnown(product); err != nil && Debug {
		fmt.Printf("[DEBUG] Dashboard import skipped for %s: %v\n", product, err)
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

	return os.WriteFile(dashboardPath, body, 0o644)
}
