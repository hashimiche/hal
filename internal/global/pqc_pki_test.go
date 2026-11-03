package global

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPQCStateCleanupOwnership(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := PQCPKIStateDir()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = RemovePQCPKIState(); err == nil {
		t.Fatal("removed directory without marker")
	}
	if err = os.WriteFile(filepath.Join(dir, "owner"), []byte("someone-else"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = RemovePQCPKIState(); err == nil {
		t.Fatal("removed foreign state")
	}
	if err = os.WriteFile(filepath.Join(dir, "owner"), []byte(PQCPKIOwner), 0600); err != nil {
		t.Fatal(err)
	}
	if err = RemovePQCPKIState(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("owned state remains: %v", err)
	}
}
