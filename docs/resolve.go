package topics

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// EnvDir is the environment variable that points the resolver at a directory of
// topics, for a user who keeps their own.
const EnvDir = "HAL_TOPICS_DIR"

// Source says which copy of the topics Resolve picked.
type Source string

const (
	// SourceEnv is the directory named by HAL_TOPICS_DIR.
	SourceEnv Source = "env"
	// SourceCheckout is <cwd>/docs/topics, for a coding agent working in a checkout.
	SourceCheckout Source = "checkout"
	// SourceEmbed is the copy baked into the binary, which is what the Homebrew
	// binary and the container serve.
	SourceEmbed Source = "embed"
)

// Embedded returns the topics baked into the binary, rooted at the topics
// directory so that "vault/jwt.md" is a valid path in it.
func Embedded() (fs.FS, error) {
	sub, err := fs.Sub(FS, "topics")
	if err != nil {
		return nil, fmt.Errorf("embedded topics unavailable: %w", err)
	}
	return sub, nil
}

// Resolve picks the topics to read, in this order:
//  1. HAL_TOPICS_DIR, when set;
//  2. <cwd>/docs/topics, when it exists (a repo checkout);
//  3. the copy embedded in the binary.
//
// It follows resolveSkillsFS in cmd/mcp, with one difference: a HAL_TOPICS_DIR
// that is set but is not a directory is an error. Falling back to another copy
// would silently teach from files the user did not point at.
func Resolve() (fs.FS, Source, error) {
	wd, err := os.Getwd()
	if err != nil {
		wd = "" // no checkout to look in; the embed still answers
	}
	return resolve(os.Getenv(EnvDir), wd)
}

func resolve(envDir, wd string) (fs.FS, Source, error) {
	if dir := strings.TrimSpace(envDir); dir != "" {
		info, err := os.Stat(dir)
		if err != nil {
			return nil, "", fmt.Errorf("%s=%q: %w", EnvDir, dir, err)
		}
		if !info.IsDir() {
			return nil, "", fmt.Errorf("%s=%q: not a directory", EnvDir, dir)
		}
		return os.DirFS(dir), SourceEnv, nil
	}

	if wd != "" {
		candidate := filepath.Join(wd, "docs", "topics")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return os.DirFS(candidate), SourceCheckout, nil
		}
	}

	sub, err := Embedded()
	if err != nil {
		return nil, "", err
	}
	return sub, SourceEmbed, nil
}

// Path is the inverse of ID: "vault/jwt" is "vault/jwt.md" and "vault" is
// "vault/index.md".
func Path(id string) (string, error) {
	parts := strings.Split(id, "/")
	if len(parts) > 2 {
		return "", fmt.Errorf("topic id %q: want <product> or <product>/<feature>", id)
	}
	for _, p := range parts {
		if !segmentRE.MatchString(p) {
			return "", fmt.Errorf("topic id %q: %q is not lowercase letters, digits and hyphens", id, p)
		}
	}
	if len(parts) == 1 {
		return id + "/index.md", nil
	}
	if parts[1] == "index" {
		return "", fmt.Errorf("topic id %q: the product topic is %q, index is its file name", id, parts[0])
	}
	return id + ".md", nil
}

// Load reads and parses one topic by id.
func Load(fsys fs.FS, id string) (Topic, error) {
	p, err := Path(id)
	if err != nil {
		return Topic{}, err
	}
	src, err := fs.ReadFile(fsys, p)
	if err != nil {
		return Topic{}, fmt.Errorf("topic %s: %w", id, err)
	}
	return Parse(p, src)
}

// LoadAll parses every topic under the root of fsys, sorted by id. Only .md
// files are topics; other files are ignored, and so are names starting with "."
// or "_", which is the rule //go:embed applies too, so the disk and the embed
// list the same files. One bad file fails the whole load, with every problem
// named: a topic that does not parse is a bug to fix, not a topic to skip.
func LoadAll(fsys fs.FS) ([]Topic, error) {
	var (
		topics []Topic
		errs   []error
	)
	walkErr := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if p != "." && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || path.Ext(p) != ".md" {
			return nil
		}
		src, err := fs.ReadFile(fsys, p)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
			return nil
		}
		t, err := Parse(p, src)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
			return nil
		}
		topics = append(topics, t)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	sort.Slice(topics, func(i, j int) bool { return topics[i].ID < topics[j].ID })
	return topics, nil
}
