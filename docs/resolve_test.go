package topics

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

// fixtureTopic is a valid topic whose summary says where it came from, so a test
// can tell which copy the resolver picked.
func fixtureTopic(summary string) string {
	return "summary: " + summary + "\n" + twoDocs + "\n\nbody\n"
}

// writeTopics writes files (slash-separated path to content) under root.
func writeTopics(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, content := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func summaryOf(t *testing.T, fsys fs.FS, id string) string {
	t.Helper()
	topic, err := Load(fsys, id)
	if err != nil {
		t.Fatalf("Load(%q): %v", id, err)
	}
	return topic.Summary
}

func TestResolveOrder(t *testing.T) {
	envDir := filepath.Join(t.TempDir(), "mine")
	writeTopics(t, envDir, map[string]string{"demo/env.md": fixtureTopic("from env")})

	checkout := t.TempDir()
	writeTopics(t, filepath.Join(checkout, "docs", "topics"), map[string]string{"demo/checkout.md": fixtureTopic("from checkout")})

	empty := t.TempDir() // a directory with no docs/topics in it

	t.Run("HAL_TOPICS_DIR wins over the checkout", func(t *testing.T) {
		fsys, src, err := resolve(envDir, checkout)
		if err != nil {
			t.Fatal(err)
		}
		if src != SourceEnv {
			t.Errorf("source = %q, want %q", src, SourceEnv)
		}
		if got := summaryOf(t, fsys, "demo/env"); got != "from env" {
			t.Errorf("summary = %q", got)
		}
		if _, err := Load(fsys, "demo/checkout"); err == nil {
			t.Error("the checkout leaked into the env directory")
		}
	})

	t.Run("a checkout is used when no env is set", func(t *testing.T) {
		fsys, src, err := resolve("", checkout)
		if err != nil {
			t.Fatal(err)
		}
		if src != SourceCheckout {
			t.Errorf("source = %q, want %q", src, SourceCheckout)
		}
		if got := summaryOf(t, fsys, "demo/checkout"); got != "from checkout" {
			t.Errorf("summary = %q", got)
		}
	})

	t.Run("a blank env counts as unset", func(t *testing.T) {
		_, src, err := resolve("  \t", checkout)
		if err != nil || src != SourceCheckout {
			t.Errorf("source = %q, err = %v; want the checkout", src, err)
		}
	})

	t.Run("the embed is the last resort", func(t *testing.T) {
		for name, wd := range map[string]string{"cwd without docs/topics": empty, "no cwd": ""} {
			fsys, src, err := resolve("", wd)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if src != SourceEmbed {
				t.Errorf("%s: source = %q, want %q", name, src, SourceEmbed)
			}
			// Rooted at the topics directory means files are <product>/<name>.md.
			// Which topics exist is not this test's business.
			if files, err := fs.Glob(fsys, "*/*.md"); err != nil || len(files) == 0 {
				t.Errorf("%s: the embed is not rooted at the topics directory (%v, %v)", name, files, err)
			}
		}
	})

	t.Run("docs/topics that is a file is not a checkout", func(t *testing.T) {
		wd := t.TempDir()
		writeTopics(t, filepath.Join(wd, "docs"), map[string]string{"topics": "not a directory"})
		_, src, err := resolve("", wd)
		if err != nil || src != SourceEmbed {
			t.Errorf("source = %q, err = %v; want the embed", src, err)
		}
	})

	t.Run("an env that is missing is an error, not a fallback", func(t *testing.T) {
		_, _, err := resolve(filepath.Join(t.TempDir(), "nope"), checkout)
		if err == nil || !strings.Contains(err.Error(), EnvDir) {
			t.Errorf("err = %v, want one naming %s", err, EnvDir)
		}
	})

	t.Run("an env that is a file is an error", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file")
		writeTopics(t, filepath.Dir(file), map[string]string{"file": "x"})
		_, _, err := resolve(file, checkout)
		if err == nil || !strings.Contains(err.Error(), "not a directory") {
			t.Errorf("err = %v, want \"not a directory\"", err)
		}
	})
}

// TestResolveUsesEnvironmentAndCwd covers the public entry point, which reads the
// real environment variable and the real working directory.
func TestResolveUsesEnvironmentAndCwd(t *testing.T) {
	envDir := t.TempDir()
	writeTopics(t, envDir, map[string]string{"demo/env.md": fixtureTopic("from env")})
	checkout := t.TempDir()
	writeTopics(t, filepath.Join(checkout, "docs", "topics"), map[string]string{"demo/checkout.md": fixtureTopic("from checkout")})

	t.Chdir(checkout)

	t.Setenv(EnvDir, "")
	if _, src, err := Resolve(); err != nil || src != SourceCheckout {
		t.Errorf("no env: source = %q, err = %v; want the checkout", src, err)
	}

	t.Setenv(EnvDir, envDir)
	if _, src, err := Resolve(); err != nil || src != SourceEnv {
		t.Errorf("env set: source = %q, err = %v; want the env", src, err)
	}

	t.Setenv(EnvDir, "")
	t.Chdir(t.TempDir())
	if _, src, err := Resolve(); err != nil || src != SourceEmbed {
		t.Errorf("elsewhere: source = %q, err = %v; want the embed", src, err)
	}
}

func TestLoad(t *testing.T) {
	fsys := fstest.MapFS{
		"vault/jwt.md":   {Data: []byte(fixtureTopic("the jwt"))},
		"vault/index.md": {Data: []byte(fixtureTopic("the product"))},
		"vault/bad.md":   {Data: []byte("summary: no docs\n")},
	}
	if got := summaryOf(t, fsys, "vault/jwt"); got != "the jwt" {
		t.Errorf("vault/jwt: %q", got)
	}
	if got := summaryOf(t, fsys, "vault"); got != "the product" {
		t.Errorf("vault: %q", got)
	}
	if _, err := Load(fsys, "vault/missing"); err == nil || !strings.Contains(err.Error(), "topic vault/missing") {
		t.Errorf("a missing topic: err = %v", err)
	}
	if _, err := Load(fsys, "vault/bad"); err == nil || !strings.Contains(err.Error(), "documents") {
		t.Errorf("a malformed topic: err = %v", err)
	}
	for _, id := range []string{"../secret", "vault/jwt/x", "Vault", ""} {
		if _, err := Load(fsys, id); err == nil {
			t.Errorf("Load(%q) accepted a bad id", id)
		}
	}
}

func TestLoadAll(t *testing.T) {
	t.Run("loads every topic sorted by id, and ignores what is not one", func(t *testing.T) {
		fsys := fstest.MapFS{
			"vault/jwt.md":           {Data: []byte(fixtureTopic("jwt"))},
			"vault/index.md":         {Data: []byte(fixtureTopic("vault"))},
			"boundary/ssh.md":        {Data: []byte(fixtureTopic("ssh"))},
			"vault/notes.txt":        {Data: []byte("not a topic")},
			"vault/.draft.md":        {Data: []byte("not even a header")},
			"vault/_template.md":     {Data: []byte("not even a header")},
			".github/pull.md":        {Data: []byte("not even a header")},
			"_drafts/vault/wip.md":   {Data: []byte("not even a header")},
			"vault/sub/too/deep.txt": {Data: []byte("x")},
		}
		got, err := LoadAll(fsys)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, topic := range got {
			ids = append(ids, topic.ID)
		}
		want := []string{"boundary/ssh", "vault", "vault/jwt"}
		if !reflect.DeepEqual(ids, want) {
			t.Errorf("ids = %v, want %v", ids, want)
		}
	})

	t.Run("one bad file fails the load and every bad file is named", func(t *testing.T) {
		fsys := fstest.MapFS{
			"vault/jwt.md":   {Data: []byte(fixtureTopic("jwt"))},
			"vault/one.md":   {Data: []byte("summary: no docs\n")},
			"vault/two.md":   {Data: []byte("read: hal x\n")},
			"README.md":      {Data: []byte("a topic cannot sit at the root")},
			"vault/Three.md": {Data: []byte(fixtureTopic("bad id"))},
		}
		_, err := LoadAll(fsys)
		if err == nil {
			t.Fatal("LoadAll accepted bad files")
		}
		for _, name := range []string{"vault/one.md", "vault/two.md", "README.md", "vault/Three.md"} {
			if !strings.Contains(err.Error(), name) {
				t.Errorf("the error does not name %s:\n%v", name, err)
			}
		}
		if strings.Contains(err.Error(), "vault/jwt.md") {
			t.Errorf("the error names a good file:\n%v", err)
		}
	})

	t.Run("an empty tree is no topics, not an error", func(t *testing.T) {
		got, err := LoadAll(fstest.MapFS{})
		if err != nil || len(got) != 0 {
			t.Errorf("got %v, %v", got, err)
		}
	})
}
