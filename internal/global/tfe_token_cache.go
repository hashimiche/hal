package global

import (
	"os"
	"path/filepath"
	"strings"
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

func CacheTFEAPIToken(token string) error {
	path := tfeAPITokenCachePath()
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.TrimSpace(token)+"\n"), 0o600)
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
	return nil
}
