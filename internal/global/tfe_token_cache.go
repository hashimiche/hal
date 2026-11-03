package global

import (
	"os"
	"path/filepath"
	"strings"

	"hal/internal/labsecrets"
)

func tfeAPITokenCachePath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".hal", "tfe-app-api-token")
}

func LoadCachedTFEAPIToken() string {
	path := tfeAPITokenCachePath()
	if path == "" {
		return ""
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
}

// CacheTFEAPIToken saves the token TFE boots its automation with, and mirrors
// it into the shared credential store (ADR 0005).
func CacheTFEAPIToken(token string) error {
	path := tfeAPITokenCachePath()
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(token)+"\n"), 0o600); err != nil {
		return err
	}
	return labsecrets.Save(map[string]string{labsecrets.KeyTFEAPIToken: token})
}

func RemoveCachedTFEAPIToken() error {
	path := tfeAPITokenCachePath()
	if path == "" {
		return nil
	}
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	// Only the token: this is also called when a cached token stops working,
	// which says nothing about the admin password the lab booted with.
	return labsecrets.Remove(labsecrets.KeyTFEAPIToken)
}
