package global

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveCryptoAgilityStateOwnership(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := CryptoAgilityStateDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := RemoveCryptoAgilityState(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := RemoveCryptoAgilityState(); err == nil {
		t.Fatal("removed unmarked state")
	}
	if err := os.WriteFile(filepath.Join(dir, "owner"), []byte("someone-else"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveCryptoAgilityState(); err == nil {
		t.Fatal("removed unowned state")
	}
	if err := os.WriteFile(filepath.Join(dir, "owner"), []byte(CryptoAgilityOwner), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveCryptoAgilityState(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("owned state still exists: %v", err)
	}
}

func TestCryptoAgilityReadiness(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	if got := CryptoAgilityReadiness(); got != "down" {
		t.Fatalf("missing deployment = %s", got)
	}
	script := filepath.Join(bin, "kubectl")
	for body, want := range map[string]string{
		"echo '{\"status\":{\"readyReplicas\":1}}'": "up",
		"echo '{\"status\":{\"readyReplicas\":0}}'": "degraded",
	} {
		if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
		if got := CryptoAgilityReadiness(); got != want {
			t.Fatalf("%s readiness = %s", want, got)
		}
	}
}
