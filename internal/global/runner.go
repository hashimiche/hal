package global

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Runner runs an external tool (a container engine, kind, multipass) and
// returns its standard output. extraEnv entries are appended to the process
// environment. A non-zero exit is returned as an error that carries the tool's
// standard error, so callers can show it without it polluting the parsed output.
type Runner func(extraEnv []string, name string, args ...string) ([]byte, error)

// runner is how the kind, Multipass and ownership helpers (and the teardown
// code built on them) reach the host. streamRunner does the same for commands
// whose progress the user should watch, such as `kind create cluster`. Both are
// variables only so tests can prove which commands would run without touching a
// real container engine or VM.
var (
	runner       Runner = execRunner
	streamRunner Runner = execStreamRunner
)

func execRunner(extraEnv []string, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return out, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return out, err
	}
	return out, nil
}

func execStreamRunner(extraEnv []string, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return nil, cmd.Run()
}

// Run executes name with args through the package Runner and returns stdout.
func Run(extraEnv []string, name string, args ...string) ([]byte, error) {
	return runner(extraEnv, name, args...)
}

// RunStreaming executes name with args, showing its output to the user as it
// is produced.
func RunStreaming(extraEnv []string, name string, args ...string) error {
	_, err := streamRunner(extraEnv, name, args...)
	return err
}

// SetRunner replaces the Runner used by Run, RunStreaming and the kind and
// Multipass helpers, and returns a function that restores the previous ones. It
// exists for tests (see package globaltest); production code never calls it.
func SetRunner(r Runner) (restore func()) {
	previousRunner, previousStream := runner, streamRunner
	runner, streamRunner = r, r
	return func() { runner, streamRunner = previousRunner, previousStream }
}
