// Package globaltest fakes the host tools (container engine, kind, multipass)
// that the teardown helpers in package global run, so tests can assert exactly
// which commands would execute without touching a real engine or VM.
package globaltest

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"hal/internal/global"
)

// Call is one command the code under test asked to run.
type Call struct {
	Env  []string
	Name string
	Args []string
}

// String renders the command line, without the environment.
func (c Call) String() string {
	return strings.TrimSpace(c.Name + " " + strings.Join(c.Args, " "))
}

// Handler decides the output of one fake command.
type Handler func(Call) ([]byte, error)

// Fake records every command and answers it with its Handler.
type Fake struct {
	mu      sync.Mutex
	calls   []Call
	handler Handler
}

// Install routes the global runner to a Fake for the rest of the test.
//
// It also empties PATH. Teardown code that has not been moved onto the runner
// yet still shells out through os/exec; with PATH empty those commands cannot
// find docker, podman, kind or multipass, so a test can never reach the real
// lab. A nil handler answers every command with empty output and no error.
func Install(t testing.TB, handler Handler) *Fake {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	f := &Fake{handler: handler}
	t.Cleanup(global.SetRunner(f.run))
	return f
}

func (f *Fake) run(env []string, name string, args ...string) ([]byte, error) {
	call := Call{Env: env, Name: name, Args: args}
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
	if f.handler == nil {
		return nil, nil
	}
	return f.handler(call)
}

// Calls returns a copy of the commands run so far, in order.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Call(nil), f.calls...)
}

// Lines returns the commands run so far as command lines.
func (f *Fake) Lines() []string {
	var lines []string
	for _, c := range f.Calls() {
		lines = append(lines, c.String())
	}
	return lines
}

// Ran reports whether a command line starting with prefix was run.
func (f *Fake) Ran(prefix string) bool {
	for _, line := range f.Lines() {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// Errorf is a helper for handlers that reject an unexpected command.
func Errorf(c Call) ([]byte, error) {
	return nil, fmt.Errorf("unexpected command: %s", c)
}
