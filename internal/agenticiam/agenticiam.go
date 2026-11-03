// Package agenticiam holds the Python sources of the Agentic IAM lab (the chat
// and the demo agent, ADR 0004) and builds them into one local image.
//
// The sources are embedded in the hal binary. The image tag is a hash of them,
// so HAL rebuilds the image only when the code changes; otherwise it comes from
// the local image cache. Each change leaves the previous image behind:
// RemoveStaleImages removes it, RemoveImages removes them all. The image contract (commands, ports and environment
// variables of both containers) is documented in app/README.md.
package agenticiam

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// The build context is listed file by file, so that local artefacts such as
// __pycache__ never reach the binary, and the files whose names start with an
// underscore (__init__.py, __main__.py) are included. The tests, the README
// and requirements.in stay out: changing them does not change the image.
//
//go:embed app/Dockerfile app/requirements.txt app/agentic_iam/*.py
//go:embed app/agentic_iam/static/*.html app/agentic_iam/static/*.css app/agentic_iam/static/*.js
var embedded embed.FS

const (
	// ImageRepository is the local repository of the lab image.
	ImageRepository = "localhost/hal-agentic-iam"

	// hashLength is the number of hex characters of the source hash kept in the tag.
	hashLength = 12
)

// Sources returns the build context: the embedded tree, rooted at its Dockerfile.
func Sources() fs.FS {
	sources, err := fs.Sub(embedded, "app")
	if err != nil {
		panic(err) // "app" is a valid, embedded path
	}
	return sources
}

var sourceHash = sync.OnceValue(func() string {
	hash, err := hashTree(Sources())
	if err != nil {
		panic(fmt.Sprintf("agenticiam: hash embedded sources: %v", err)) // embedded files are always readable
	}
	return hash
})

// SourceHash returns a short, deterministic hash of the embedded sources.
func SourceHash() string {
	return sourceHash()
}

// ImageRef returns the reference of the image built from the embedded sources,
// e.g. localhost/hal-agentic-iam:3f9c2a1b7d4e.
func ImageRef() string {
	return ImageRepository + ":" + SourceHash()
}

// EnsureImage makes sure ImageRef is in the local image store of engine
// ("docker" or "podman"), and builds it from the embedded sources if it is not.
// The build pulls the pinned base image and the locked Python packages, so the
// first build needs Docker Hub and PyPI. Build output streams to the terminal.
func EnsureImage(engine string) error {
	ref := ImageRef()
	if exec.Command(engine, "image", "inspect", ref).Run() == nil {
		return nil
	}

	dir, err := os.MkdirTemp("", "hal-agentic-iam-build-*")
	if err != nil {
		return fmt.Errorf("create build directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	if err := writeTree(dir, Sources()); err != nil {
		return fmt.Errorf("write build context: %w", err)
	}

	cmd := exec.Command(engine, "build", "--platform", buildPlatform(), "-t", ref, dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build %s: %w", ref, err)
	}
	return nil
}

// Images returns the images of ImageRepository in the local image store of
// engine: ImageRef if it is built, and those built from earlier sources.
func Images(engine string) ([]string, error) {
	out, err := exec.Command(engine, "images", "--format", "{{.Repository}}:{{.Tag}}").Output()
	if err != nil {
		return nil, fmt.Errorf("list the images of %s: %w", ImageRepository, err)
	}
	return repositoryImages(string(out)), nil
}

// RemoveStaleImages removes the images built from earlier sources and keeps
// ImageRef. It returns the images it removed.
func RemoveStaleImages(engine string) ([]string, error) {
	return removeImages(engine, ImageRef())
}

// RemoveImages removes every image of ImageRepository, ImageRef included. It
// returns the images it removed.
func RemoveImages(engine string) ([]string, error) {
	return removeImages(engine, "")
}

// removeImages removes every image of ImageRepository but keep. It never
// forces the removal, which would also remove the containers that run the
// image: an image in use stays, and is reported.
func removeImages(engine, keep string) ([]string, error) {
	images, err := Images(engine)
	if err != nil {
		return nil, err
	}
	var removed []string
	var errs []error
	for _, ref := range images {
		if ref == keep {
			continue
		}
		if out, err := exec.Command(engine, "image", "rm", ref).CombinedOutput(); err != nil {
			errs = append(errs, fmt.Errorf("remove %s: %v: %s", ref, err, strings.TrimSpace(string(out))))
			continue
		}
		removed = append(removed, ref)
	}
	return removed, errors.Join(errs...)
}

// repositoryImages returns the tags of ImageRepository in the output of
// `images --format {{.Repository}}:{{.Tag}}`. An untagged image cannot be
// removed by name, so it is left out.
func repositoryImages(list string) []string {
	var images []string
	for _, line := range strings.Split(list, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, ImageRepository+":") && line != ImageRepository+":<none>" {
			images = append(images, line)
		}
	}
	return images
}

// buildPlatform is the image platform matching this machine. On Apple Silicon,
// the Podman or Docker VM is linux/arm64.
func buildPlatform() string {
	if runtime.GOARCH == "arm64" {
		return "linux/arm64"
	}
	return "linux/amd64"
}

// hashTree hashes every file of fsys: its path, its size and its content, in
// the lexical order of fs.WalkDir.
func hashTree(fsys fs.FS) (string, error) {
	h := sha256.New()
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00", path, len(data))
		h.Write(data)
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil))[:hashLength], nil
}

// writeTree copies every file of fsys under dir.
func writeTree(dir string, fsys fs.FS) error {
	return fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(path))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
