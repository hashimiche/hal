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
