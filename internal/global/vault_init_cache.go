package global

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// vault_init_cache.go is the standard, HAL-wide store for the material produced
// when a production-mode Vault Enterprise instance is initialized
// (`hal vault create --mode prod`). It mirrors tfe_token_cache.go: a single
// 0600 file under ~/.hal, with Path/Cache/Load/Remove helpers so every surface
// (create summary, `hal vault status`, `hal creds status`, delete cleanup)
// reads and writes the same location and never drifts.
//
// The file is the verbatim JSON emitted by `vault operator init -format=json`,
// so it contains the unseal key(s) and the initial root token. It is the ONLY
// copy of the unseal material — losing it means a sealed, unrecoverable Vault.

// VaultProdStateDir returns ~/.hal/vault-prod, the directory holding the prod
// instance's config, TLS certs, and init.json. Empty string if the home
// directory cannot be resolved.
func VaultProdStateDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".hal", "vault-prod")
}

// VaultInitCachePath returns the path to the saved `vault operator init` JSON.
func VaultInitCachePath() string {
	dir := VaultProdStateDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "init.json")
}

// VaultProdCertPath returns the host path to the prod instance's self-signed
// TLS certificate (used for VAULT_CACERT). Empty string if home is unresolvable.
func VaultProdCertPath() string {
	dir := VaultProdStateDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "certs", "cert.pem")
}

// VaultInit is the parsed subset of `vault operator init -format=json` that HAL
// surfaces. Extra fields in the JSON (recovery keys, nonce, etc.) are ignored.
type VaultInit struct {
	UnsealKeysB64 []string `json:"unseal_keys_b64"`
	UnsealKeysHex []string `json:"unseal_keys_hex"`
	RootToken     string   `json:"root_token"`
}

// CacheVaultInit writes the raw `vault operator init -format=json` output to
// ~/.hal/vault-prod/init.json at mode 0600 (parent dir 0700).
func CacheVaultInit(data []byte) error {
	path := VaultInitCachePath()
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// LoadCachedVaultInit reads and parses the saved init material. It returns an
// error if the file is absent or malformed, so callers can fall back to dev-mode
// behavior when no prod init file exists.
func LoadCachedVaultInit() (VaultInit, error) {
	var vi VaultInit
	path := VaultInitCachePath()
	if path == "" {
		return vi, os.ErrNotExist
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return vi, err
	}
	if err := json.Unmarshal(body, &vi); err != nil {
		return vi, err
	}
	return vi, nil
}

// RemoveCachedVaultInit deletes the entire ~/.hal/vault-prod directory (init.json,
// vault.hcl, and TLS certs) so a `hal vault delete` never strands the credential
// file. Missing paths are not an error.
func RemoveCachedVaultInit() error {
	dir := VaultProdStateDir()
	if dir == "" {
		return nil
	}
	err := os.RemoveAll(dir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
