package global

import (
	"path/filepath"
	"testing"
)

func TestPruneStaleTFEBackedSharedConsumersDropsDeadTFEOwners(t *testing.T) {
	sharedServicesPathOverride = filepath.Join(t.TempDir(), "shared-services.json")
	t.Cleanup(func() { sharedServicesPathOverride = "" })

	if err := AddSharedServiceConsumer(SharedGitLabServiceKey, GitLabConsumerVaultJWT); err != nil {
		t.Fatal(err)
	}
	if err := AddSharedServiceConsumer(SharedGitLabServiceKey, GitLabConsumerVCSPrimary); err != nil {
		t.Fatal(err)
	}
	if err := AddSharedServiceConsumer(SharedGitLabServiceKey, GitLabConsumerVCSTwin); err != nil {
		t.Fatal(err)
	}
	if err := AddSharedServiceConsumer(SharedAuthentikServiceKey, AuthentikConsumerTFESAMLPrimary); err != nil {
		t.Fatal(err)
	}
	if err := AddSharedServiceConsumer(SharedAuthentikServiceKey, AuthentikConsumerTFESAMLTwin); err != nil {
		t.Fatal(err)
	}

	// Primary TFE is gone; twin is still up. Vault JWT is independent of TFE.
	remainingGitLab := pruneStaleTFEBackedConsumers(func(container string) bool {
		return container == tfeTwinRuntimeContainer
	})

	assertConsumers(t, remainingGitLab, GitLabConsumerVaultJWT, GitLabConsumerVCSTwin)
	assertConsumers(t, GetSharedServiceConsumers(SharedAuthentikServiceKey), AuthentikConsumerTFESAMLTwin)
}

func TestPruneStaleTFEBackedSharedConsumersClearsAllWhenTFEGone(t *testing.T) {
	sharedServicesPathOverride = filepath.Join(t.TempDir(), "shared-services.json")
	t.Cleanup(func() { sharedServicesPathOverride = "" })

	if err := AddSharedServiceConsumer(SharedGitLabServiceKey, GitLabConsumerVaultJWT); err != nil {
		t.Fatal(err)
	}
	if err := AddSharedServiceConsumer(SharedGitLabServiceKey, GitLabConsumerVCSPrimary); err != nil {
		t.Fatal(err)
	}

	remaining := pruneStaleTFEBackedConsumers(func(string) bool { return false })
	assertConsumers(t, remaining, GitLabConsumerVaultJWT)
	if got := GetSharedServiceConsumers(SharedAuthentikServiceKey); len(got) != 0 {
		t.Fatalf("authentik consumers = %v, want none", got)
	}
}

func TestVaultMariaDBKeptUntilLastConsumerReleases(t *testing.T) {
	sharedServicesPathOverride = filepath.Join(t.TempDir(), "shared-services.json")
	t.Cleanup(func() { sharedServicesPathOverride = "" })

	for _, c := range []string{VaultMariaDBConsumerDatabase, VaultMariaDBConsumerAgenticIAM, VaultMariaDBConsumerDatabase} {
		if err := AddSharedServiceConsumer(SharedVaultMariaDBServiceKey, c); err != nil {
			t.Fatal(err)
		}
	}
	// Re-running enable does not count the database lab twice.
	assertConsumers(t, GetSharedServiceConsumers(SharedVaultMariaDBServiceKey), VaultMariaDBConsumerDatabase, VaultMariaDBConsumerAgenticIAM)

	// hal vault database disable: the Agentic IAM lab keeps the container.
	remaining, err := RemoveSharedServiceConsumer(SharedVaultMariaDBServiceKey, VaultMariaDBConsumerDatabase)
	if err != nil {
		t.Fatal(err)
	}
	assertConsumers(t, remaining, VaultMariaDBConsumerAgenticIAM)

	// A second disable of a lab that is no longer registered changes nothing.
	remaining, err = RemoveSharedServiceConsumer(SharedVaultMariaDBServiceKey, VaultMariaDBConsumerDatabase)
	if err != nil {
		t.Fatal(err)
	}
	assertConsumers(t, remaining, VaultMariaDBConsumerAgenticIAM)

	// Last consumer out: the container may go.
	remaining, err = RemoveSharedServiceConsumer(SharedVaultMariaDBServiceKey, VaultMariaDBConsumerAgenticIAM)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining = %v, want none", remaining)
	}
	if got := GetSharedServiceConsumers(SharedVaultMariaDBServiceKey); len(got) != 0 {
		t.Fatalf("registry = %v, want none", got)
	}
}

func TestVaultMariaDBClearDropsOnlyItsOwnConsumers(t *testing.T) {
	sharedServicesPathOverride = filepath.Join(t.TempDir(), "shared-services.json")
	t.Cleanup(func() { sharedServicesPathOverride = "" })

	if err := AddSharedServiceConsumer(SharedVaultMariaDBServiceKey, VaultMariaDBConsumerAgenticIAM); err != nil {
		t.Fatal(err)
	}
	if err := AddSharedServiceConsumer(SharedAuthentikServiceKey, AuthentikConsumerTFESAMLPrimary); err != nil {
		t.Fatal(err)
	}

	// A fresh container (or hal vault delete) clears the MariaDB consumers,
	// whose state died with the old container, and only those.
	if err := ClearSharedService(SharedVaultMariaDBServiceKey); err != nil {
		t.Fatal(err)
	}
	if err := AddSharedServiceConsumer(SharedVaultMariaDBServiceKey, VaultMariaDBConsumerDatabase); err != nil {
		t.Fatal(err)
	}

	assertConsumers(t, GetSharedServiceConsumers(SharedVaultMariaDBServiceKey), VaultMariaDBConsumerDatabase)
	assertConsumers(t, GetSharedServiceConsumers(SharedAuthentikServiceKey), AuthentikConsumerTFESAMLPrimary)
}

func TestVaultDatabaseUsesMariaDB(t *testing.T) {
	cases := []struct {
		name      string
		consumers []string
		want      bool
	}{
		{"no registration (container predates counting)", nil, true},
		{"database only", []string{VaultMariaDBConsumerDatabase}, true},
		{"agentic iam only", []string{VaultMariaDBConsumerAgenticIAM}, false},
		{"both", []string{VaultMariaDBConsumerAgenticIAM, VaultMariaDBConsumerDatabase}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sharedServicesPathOverride = filepath.Join(t.TempDir(), "shared-services.json")
			t.Cleanup(func() { sharedServicesPathOverride = "" })

			for _, c := range tc.consumers {
				if err := AddSharedServiceConsumer(SharedVaultMariaDBServiceKey, c); err != nil {
					t.Fatal(err)
				}
			}
			if got := VaultDatabaseUsesMariaDB(); got != tc.want {
				t.Fatalf("VaultDatabaseUsesMariaDB() = %v, want %v", got, tc.want)
			}
		})
	}
}

func assertConsumers(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("consumers = %v, want %v", got, want)
	}
	seen := map[string]bool{}
	for _, c := range got {
		seen[c] = true
	}
	for _, c := range want {
		if !seen[c] {
			t.Fatalf("consumers = %v, missing %q", got, c)
		}
	}
}
