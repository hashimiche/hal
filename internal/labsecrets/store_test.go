package labsecrets

import (
	"os"
	"path/filepath"
	"testing"
)

// useTempStore points the runtime store at a temp dir so a test never touches
// the operator's ~/.hal.
func useTempStore(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lab.creds")
	previous := storePathOverride
	storePathOverride = path
	t.Cleanup(func() { storePathOverride = previous })
	return path
}

func TestMissingStoreLoadsEmpty(t *testing.T) {
	useTempStore(t)

	if got := Load(); len(got) != 0 {
		t.Errorf("expected no values before any lab booted, got %v", got)
	}
}

func TestSaveKeepsTheOtherLabsValues(t *testing.T) {
	useTempStore(t)

	if err := Save(map[string]string{KeyVaultRootToken: "hvs.one"}); err != nil {
		t.Fatalf("save vault token: %v", err)
	}
	if err := Save(map[string]string{KeyTFEAPIToken: "tfe-token"}); err != nil {
		t.Fatalf("save tfe token: %v", err)
	}

	values := Load()
	if values.Get(KeyVaultRootToken) != "hvs.one" || values.Get(KeyTFEAPIToken) != "tfe-token" {
		t.Errorf("enabling one lab dropped another's secret: %v", values)
	}
}

func TestSavingAnEmptyValueDropsTheKey(t *testing.T) {
	useTempStore(t)

	_ = Save(map[string]string{KeyGitLabRootPassword: "chosen-at-boot"})
	_ = Save(map[string]string{KeyGitLabRootPassword: ""})

	if got := Load().Get(KeyGitLabRootPassword); got != "" {
		t.Errorf("expected the stale override to be dropped, got %q", got)
	}
}

func TestRemoveDropsOnlyTheNamedKeys(t *testing.T) {
	useTempStore(t)

	_ = Save(map[string]string{KeyVaultRootToken: "hvs.one", KeyVaultUnsealKey: "unseal", KeyTFEAPIToken: "tfe"})
	if err := Remove(KeyVaultRootToken, KeyVaultUnsealKey); err != nil {
		t.Fatalf("remove: %v", err)
	}

	values := Load()
	if values.Get(KeyVaultRootToken) != "" || values.Get(KeyVaultUnsealKey) != "" {
		t.Errorf("vault delete must drop its own secrets: %v", values)
	}
	if values.Get(KeyTFEAPIToken) != "tfe" {
		t.Errorf("vault delete must leave Terraform's token alone: %v", values)
	}
}

func TestTheStoreIsPrivateToTheOperator(t *testing.T) {
	path := useTempStore(t)

	_ = Save(map[string]string{KeyVaultRootToken: "hvs.one"})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat store: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("store mode = %v, want 0600", mode)
	}
}

func TestOrFallsBackToTheCatalog(t *testing.T) {
	values := Values{KeyTFEAdminPassword: "  "}

	if got := values.Or(KeyTFEAdminPassword, TFEAdminPassword); got != TFEAdminPassword {
		t.Errorf("a blank stored value must fall back to the catalog, got %q", got)
	}
	if got := (Values{KeyTFEAdminPassword: "stored"}).Or(KeyTFEAdminPassword, TFEAdminPassword); got != "stored" {
		t.Errorf("a stored value must win over the catalog, got %q", got)
	}
}

func TestSaveOverrideOnlyStoresWhatTheFlagChanged(t *testing.T) {
	useTempStore(t)

	if err := SaveOverride(KeyGitLabRootPassword, GitLabRootPassword, GitLabRootPassword); err != nil {
		t.Fatalf("save override: %v", err)
	}
	if got := Load().Get(KeyGitLabRootPassword); got != "" {
		t.Errorf("a password left at its catalog default needs no override, got %q", got)
	}

	_ = SaveOverride(KeyGitLabRootPassword, "chosen-at-boot", GitLabRootPassword)
	if got := Load().Get(KeyGitLabRootPassword); got != "chosen-at-boot" {
		t.Errorf("override = %q, want the flag's value", got)
	}

	// Booting again without the flag must not leave the old override behind.
	_ = SaveOverride(KeyGitLabRootPassword, GitLabRootPassword, GitLabRootPassword)
	if got := Load().Get(KeyGitLabRootPassword); got != "" {
		t.Errorf("stale override survived a default boot: %q", got)
	}
}

func TestRemovingTheLastValueRemovesTheFile(t *testing.T) {
	path := useTempStore(t)

	_ = Save(map[string]string{KeyVaultRootToken: "hvs.one"})
	if err := Remove(KeyVaultRootToken); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("an empty store should leave no file behind, stat err = %v", err)
	}
}
