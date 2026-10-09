package agenticiam

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSourceHashIsStable(t *testing.T) {
	again, err := hashTree(Sources())
	if err != nil {
		t.Fatal(err)
	}
	if SourceHash() != again {
		t.Fatalf("SourceHash() = %q, hashing the same tree again gives %q", SourceHash(), again)
	}
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(SourceHash()) {
		t.Fatalf("SourceHash() = %q, want 12 hex characters", SourceHash())
	}
}

func TestSourceHashChangesWithTheTree(t *testing.T) {
	base := fstest.MapFS{
		"Dockerfile":        {Data: []byte("FROM python")},
		"agentic_iam/a.py":  {Data: []byte("print('a')")},
		"agentic_iam/bc.py": {Data: []byte("")},
	}
	variants := map[string]fstest.MapFS{
		"content changed": with(base, "agentic_iam/a.py", "print('b')"),
		"file added":      with(base, "agentic_iam/new.py", ""),
		"file renamed": {
			"Dockerfile":        base["Dockerfile"],
			"agentic_iam/b.py":  base["agentic_iam/a.py"],
			"agentic_iam/bc.py": base["agentic_iam/bc.py"],
		},
		// Path and content are delimited: moving bytes from one to the other changes the hash.
		"bytes moved between path and content": {
			"Dockerfile":       base["Dockerfile"],
			"agentic_iam/a.py": base["agentic_iam/a.py"],
			"agentic_iam/b":    {Data: []byte("c.py")},
		},
	}

	baseHash := mustHash(t, base)
	if again := mustHash(t, base); again != baseHash {
		t.Fatalf("same tree hashed twice: %q then %q", baseHash, again)
	}
	for name, variant := range variants {
		if mustHash(t, variant) == baseHash {
			t.Errorf("%s: hash unchanged (%q)", name, baseHash)
		}
	}
}

func TestImageRef(t *testing.T) {
	want := regexp.MustCompile(`^localhost/hal-agentic-iam:[0-9a-f]{12}$`)
	if !want.MatchString(ImageRef()) {
		t.Fatalf("ImageRef() = %q, want %s", ImageRef(), want)
	}
	if !strings.HasSuffix(ImageRef(), ":"+SourceHash()) {
		t.Fatalf("ImageRef() = %q is not tagged with SourceHash() %q", ImageRef(), SourceHash())
	}
}

func TestRepositoryImages(t *testing.T) {
	list := strings.Join([]string{
		"localhost/hal-agentic-iam:4f1210aa90b2",
		"<none>:<none>",
		"docker.io/library/python:3.13-slim",
		"localhost/hal-agentic-iam-other:58627d7e5d70",
		"localhost/hal-agentic-iam:<none>",
		"  localhost/hal-agentic-iam:16a00cb2b289  ",
		"",
	}, "\n")

	got := repositoryImages(list)
	want := []string{"localhost/hal-agentic-iam:4f1210aa90b2", "localhost/hal-agentic-iam:16a00cb2b289"}
	if !slices.Equal(got, want) {
		t.Fatalf("repositoryImages() = %q, want %q", got, want)
	}
}

func TestEmbeddedTreeIsTheBuildContext(t *testing.T) {
	var got []string
	err := fs.WalkDir(Sources(), ".", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			got = append(got, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"Dockerfile",
		"agentic_iam/__init__.py",
		"agentic_iam/__main__.py",
		"agentic_iam/agent.py",
		"agentic_iam/chat.py",
		"agentic_iam/database.py",
		"agentic_iam/env.py",
		"agentic_iam/fake_model.py",
		"agentic_iam/idp.py",
		"agentic_iam/model.py",
		"agentic_iam/oauth.py",
		"agentic_iam/static/app.css",
		"agentic_iam/static/app.js",
		"agentic_iam/static/index.html",
		"agentic_iam/tools.py",
		"agentic_iam/vault.py",
		"agentic_iam/web.py",
		"requirements.txt",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("embedded files:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// ADR 0004, static verification: no latest tag and no unpinned Python dependency.
func TestBaseImageIsPinnedByTagAndDigest(t *testing.T) {
	dockerfile, err := fs.ReadFile(Sources(), "Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	from := regexp.MustCompile(`(?m)^FROM\s+(\S+)`).FindAllSubmatch(dockerfile, -1)
	if len(from) != 1 {
		t.Fatalf("want exactly one FROM line, got %d", len(from))
	}
	pinned := regexp.MustCompile(`^docker\.io/library/python:[0-9][0-9.]*-slim-[a-z]+@sha256:[0-9a-f]{64}$`)
	if ref := string(from[0][1]); !pinned.MatchString(ref) {
		t.Fatalf("base image %q is not pinned by version tag and digest", ref)
	}
	if bytes.Contains(dockerfile, []byte(":latest")) {
		t.Fatal("the Dockerfile mentions a latest tag")
	}
	if !regexp.MustCompile(`--require-hashes --no-deps`).Match(dockerfile) {
		t.Fatal("pip must install with --require-hashes --no-deps")
	}
}

func TestRequirementsAreLockedWithHashes(t *testing.T) {
	lock, err := fs.ReadFile(Sources(), "requirements.txt")
	if err != nil {
		t.Fatal(err)
	}
	// One requirement per logical line: continuation lines carry its hashes.
	logical := strings.ReplaceAll(string(lock), "\\\n", " ")
	pin := regexp.MustCompile(`^[A-Za-z0-9._-]+==[^\s;]+(\s*;[^-]+)?\s+(--hash=sha256:[0-9a-f]{64}\s*)+$`)
	count := 0
	for _, line := range strings.Split(logical, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		count++
		if !pin.MatchString(line) {
			t.Errorf("not pinned with hashes: %.120s", line)
		}
	}
	for _, direct := range []string{"pydantic-ai-slim==", "pymysql=="} {
		if !strings.Contains(logical, "\n"+direct) {
			t.Errorf("direct dependency %s missing from the lock", strings.TrimSuffix(direct, "=="))
		}
	}
	if count == 0 {
		t.Fatal("no requirement in requirements.txt")
	}
}

func TestWriteTreeCopiesTheBuildContext(t *testing.T) {
	dir := t.TempDir()
	if err := writeTree(dir, Sources()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"Dockerfile", "requirements.txt", "agentic_iam/__main__.py", "agentic_iam/static/index.html"} {
		want, err := fs.ReadFile(Sources(), path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: written content differs from the embedded file", path)
		}
	}
}

func with(base fstest.MapFS, path, content string) fstest.MapFS {
	out := fstest.MapFS{}
	for k, v := range base {
		out[k] = v
	}
	out[path] = &fstest.MapFile{Data: []byte(content)}
	return out
}

func mustHash(t *testing.T, fsys fs.FS) string {
	t.Helper()
	hash, err := hashTree(fsys)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}
