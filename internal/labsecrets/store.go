package labsecrets

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The runtime store is ~/.hal/lab.creds (mode 0600): the operator-facing
// secrets a lab generates when it boots, plus any catalog password a flag
// overrode. It is a mirror, never the source a lab boots from — Authentik still
// boots from ~/.hal/authentik/env, a prod Vault from ~/.hal/vault-prod/init.json
// and TFE from ~/.hal/tfe-app-api-token. Create and enable write both; delete
// and disable drop the keys here. A reader prefers this file and falls back to
// the boot file, so a lab that was already up before this store existed still
// shows its secret.
//
// A HashiCorp product license is never written here.

// Keys of the runtime store.
const (
	KeyVaultRootToken = "VAULT_ROOT_TOKEN"
	KeyVaultUnsealKey = "VAULT_UNSEAL_KEY"
	KeyVaultCACert    = "VAULT_CACERT"

	KeyAuthentikAdminPassword = "AUTHENTIK_ADMIN_PASSWORD"

	KeyTFEAdminPassword     = "TFE_ADMIN_PASSWORD"
	KeyTFEAPIToken          = "TFE_API_TOKEN"
	KeyTFETwinAdminPassword = "TFE_TWIN_ADMIN_PASSWORD"

	KeyGitLabRootPassword = "GITLAB_ROOT_PASSWORD"

	KeyAAPPassword = "AAP_PASSWORD"
)

// storePathOverride lets tests keep off ~/.hal.
var storePathOverride string

// StorePath returns the path of the runtime store, or "" when the home
// directory cannot be resolved.
func StorePath() string {
	if storePathOverride != "" {
		return storePathOverride
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".hal", "lab.creds")
}

// Values are the runtime secrets, keyed by the constants above.
type Values map[string]string

// Get returns a stored value, or "" when it is absent.
func (v Values) Get(key string) string { return v[key] }

// Or returns the stored value, falling back to the catalog default (or to a
// value read from the lab's own boot file) when nothing is stored.
func (v Values) Or(key, fallback string) string {
	if got := strings.TrimSpace(v[key]); got != "" {
		return got
	}
	return fallback
}

// Load reads the runtime store. A missing or unreadable file is not an error:
// every value falls back to the catalog or to the lab's boot file.
func Load() Values {
	values := Values{}
	path := StorePath()
	if path == "" {
		return values
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return values
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || key == "" {
			continue
		}
		values[key] = value
	}
	return values
}

// Save writes the given keys into the store, leaving the others untouched. An
// empty value removes its key, so a lab that stops generating a secret does not
// leave a stale one behind.
func Save(updates map[string]string) error {
	if len(updates) == 0 {
		return nil
	}
	values := Load()
	for key, value := range updates {
		value = strings.TrimSpace(value)
		if value == "" {
			delete(values, key)
			continue
		}
		values[key] = value
	}
	return write(values)
}

// SaveOverride records a catalog value a flag replaced at boot, so the surfaces
// show what the lab actually booted with. A value back at its catalog default
// drops the stale override instead of storing a duplicate.
func SaveOverride(key, value, catalogDefault string) error {
	if strings.TrimSpace(value) == catalogDefault {
		return Remove(key)
	}
	return Save(map[string]string{key: value})
}

// Remove drops the given keys from the store.
func Remove(keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	values := Load()
	for _, key := range keys {
		delete(values, key)
	}
	return write(values)
}

// RemoveStore deletes the whole runtime store, for a full teardown.
func RemoveStore() error {
	path := StorePath()
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func write(values Values) error {
	path := StorePath()
	if path == "" {
		return nil
	}
	if len(values) == 0 {
		return RemoveStore()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("failed to create HAL state dir: %w", err)
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("# HAL lab credentials, written by create/enable. Mode 0600.\n")
	b.WriteString("# Generated values only — never a HashiCorp product license.\n")
	for _, key := range keys {
		fmt.Fprintf(&b, "%s=%s\n", key, values[key])
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}
