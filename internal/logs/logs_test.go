package logs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"hal/internal/global"
)

// call is one command the fake runner was asked to run.
type call struct {
	name string
	args []string
}

func (c call) String() string { return c.name + " " + strings.Join(c.args, " ") }

// reply is what the fake runner answers for a command.
type reply struct {
	out []byte
	err error
}

// fakeRunner answers by the first matching prefix of "name arg0 arg1 ..." and
// records every call, so tests can assert exactly what would have been executed.
type fakeRunner struct {
	replies map[string]reply
	calls   []call
}

func newFakeRunner() *fakeRunner { return &fakeRunner{replies: map[string]reply{}} }

func (f *fakeRunner) on(prefix string, out string, err error) *fakeRunner {
	f.replies[prefix] = reply{out: []byte(out), err: err}
	return f
}

func (f *fakeRunner) run(_ context.Context, name string, args ...string) ([]byte, error) {
	c := call{name: name, args: args}
	f.calls = append(f.calls, c)
	line := c.String()
	// Longest matching prefix wins.
	best, found := "", false
	for prefix := range f.replies {
		if strings.HasPrefix(line, prefix) && len(prefix) >= len(best) {
			best, found = prefix, true
		}
	}
	if !found {
		return nil, fmt.Errorf("fakeRunner: unexpected command %q", line)
	}
	r := f.replies[best]
	return r.out, r.err
}

func (f *fakeRunner) called(prefix string) bool {
	for _, c := range f.calls {
		if strings.HasPrefix(c.String(), prefix) {
			return true
		}
	}
	return false
}

// harness runs a logs command end to end through cobra with injected deps.
type harness struct {
	stdout, stderr bytes.Buffer
	exitCode       int
	exited         bool
	runner         *fakeRunner
	execErr        error
}

func runCommand(t *testing.T, spec Spec, runner *fakeRunner, engine string, engineErr error, args ...string) *harness {
	t.Helper()
	h := &harness{runner: runner}
	cmd := newCommand(spec, Deps{
		DetectEngine: func() (string, error) { return engine, engineErr },
		Run:          runner.run,
		Exit:         func(code int) { h.exited, h.exitCode = true, code },
	})
	cmd.SetOut(&h.stdout)
	cmd.SetErr(&h.stderr)
	cmd.SetArgs(args)
	h.execErr = cmd.Execute()
	return h
}

var (
	oneContainer = Spec{
		Product:    "consul",
		Components: []Component{Container("hal-consul", "")},
	}
	manyContainers = Spec{
		Product: "obs",
		Components: []Component{
			Container("hal-grafana", "dashboards"),
			Container("hal-loki", "log store"),
			Container("hal-prometheus", "metrics store"),
		},
	}
	nomadSpec = Spec{
		Product:    "nomad",
		Components: []Component{MultipassUnit("hal-nomad", "nomad", "journal")},
	}
)

func runningContainer(name, logLines string) *fakeRunner {
	return newFakeRunner().
		on("docker inspect -f {{.State.Status}} "+name, "running\n", nil).
		on("docker logs --tail", logLines, nil)
}

func TestBoundTail(t *testing.T) {
	cases := []struct {
		in       int
		want     int
		wantNote bool
		wantErr  bool
	}{
		{in: -5, wantErr: true},
		{in: 0, wantErr: true},
		{in: 1, want: 1},
		{in: DefaultTail, want: DefaultTail},
		{in: MaxTail, want: MaxTail},
		{in: MaxTail + 1, want: MaxTail, wantNote: true},
		{in: 1_000_000, want: MaxTail, wantNote: true},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.in), func(t *testing.T) {
			got, note, err := BoundTail(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("BoundTail(%d) error = %v, wantErr %v", tc.in, err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("BoundTail(%d) = %d, want %d", tc.in, got, tc.want)
			}
			if (note != "") != tc.wantNote {
				t.Fatalf("BoundTail(%d) note = %q, wantNote %v", tc.in, note, tc.wantNote)
			}
		})
	}
}

func TestDefaultsAreSane(t *testing.T) {
	if DefaultTail < 1 || DefaultTail > MaxTail {
		t.Fatalf("DefaultTail %d must be within [1, %d]", DefaultTail, MaxTail)
	}
}

func TestTailFlagParsing(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantTail string
		wantNote string
	}{
		{name: "default", args: nil, wantTail: "100"},
		{name: "explicit", args: []string{"--tail", "5"}, wantTail: "5"},
		{name: "equals form", args: []string{"--tail=7"}, wantTail: "7"},
		{name: "at the cap", args: []string{"--tail", "1000"}, wantTail: "1000"},
		{name: "above the cap is clamped", args: []string{"--tail", "5000"}, wantTail: "1000", wantNote: "exceeds the cap of 1000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := runCommand(t, oneContainer, runningContainer("hal-consul", "line\n"), "docker", nil, tc.args...)
			if h.exited {
				t.Fatalf("unexpected exit %d, stderr: %s", h.exitCode, h.stderr.String())
			}
			want := "docker logs --tail " + tc.wantTail + " hal-consul"
			if !h.runner.called(want) {
				t.Fatalf("expected call %q, got %v", want, h.runner.calls)
			}
			if tc.wantNote != "" && !strings.Contains(h.stderr.String(), tc.wantNote) {
				t.Fatalf("stderr %q should mention %q", h.stderr.String(), tc.wantNote)
			}
			if tc.wantNote == "" && h.stderr.Len() != 0 {
				t.Fatalf("stderr should be empty, got %q", h.stderr.String())
			}
		})
	}
}

func TestTailFlagRejectsBadValues(t *testing.T) {
	t.Run("zero and negative exit 1 without touching the engine", func(t *testing.T) {
		for _, v := range []string{"0", "-3"} {
			h := runCommand(t, oneContainer, newFakeRunner(), "docker", nil, "--tail="+v)
			if !h.exited || h.exitCode != 1 {
				t.Fatalf("--tail=%s: want exit 1, got exited=%v code=%d", v, h.exited, h.exitCode)
			}
			if !strings.Contains(h.stderr.String(), "--tail must be at least 1") {
				t.Fatalf("--tail=%s: stderr %q", v, h.stderr.String())
			}
			if len(h.runner.calls) != 0 {
				t.Fatalf("--tail=%s: engine must not be called, got %v", v, h.runner.calls)
			}
			if h.stdout.Len() != 0 {
				t.Fatalf("--tail=%s: stdout must stay empty, got %q", v, h.stdout.String())
			}
		}
	})
	t.Run("non-numeric is a flag error", func(t *testing.T) {
		h := runCommand(t, oneContainer, newFakeRunner(), "docker", nil, "--tail", "abc")
		if h.execErr == nil {
			t.Fatal("expected a flag parsing error for --tail abc")
		}
		if len(h.runner.calls) != 0 {
			t.Fatalf("engine must not be called, got %v", h.runner.calls)
		}
	})
	t.Run("positional arguments are refused", func(t *testing.T) {
		h := runCommand(t, oneContainer, newFakeRunner(), "docker", nil, "hal-vault")
		if h.execErr == nil {
			t.Fatal("expected an error for a positional argument")
		}
	})
}

func TestLogsGoToStdoutAndNotesToStderr(t *testing.T) {
	h := runCommand(t, oneContainer, runningContainer("hal-consul", "a\nb\nc\n"), "docker", nil, "--tail", "5000")
	if h.exited {
		t.Fatalf("unexpected exit %d", h.exitCode)
	}
	if got := h.stdout.String(); got != "a\nb\nc\n" {
		t.Fatalf("stdout must be exactly the log lines, got %q", got)
	}
	if !strings.Contains(h.stderr.String(), "exceeds the cap") {
		t.Fatalf("the clamp note belongs on stderr, got %q", h.stderr.String())
	}
}

func TestMissingTrailingNewlineIsAdded(t *testing.T) {
	h := runCommand(t, oneContainer, runningContainer("hal-consul", "no newline"), "docker", nil)
	if got := h.stdout.String(); got != "no newline\n" {
		t.Fatalf("got %q", got)
	}
}

func TestEmptyLogsExitZeroWithNote(t *testing.T) {
	h := runCommand(t, oneContainer, runningContainer("hal-consul", "  \n"), "docker", nil)
	if h.exited {
		t.Fatalf("empty logs are not an error, got exit %d", h.exitCode)
	}
	if h.stdout.Len() != 0 {
		t.Fatalf("stdout should be empty, got %q", h.stdout.String())
	}
	if !strings.Contains(h.stderr.String(), "no log output yet") {
		t.Fatalf("stderr %q", h.stderr.String())
	}
}

func TestMissingContainer(t *testing.T) {
	cases := []struct {
		name   string
		engine string
		out    string
	}{
		{name: "podman", engine: "podman", out: "Error: no such container \"hal-consul\"\n"},
		{name: "docker", engine: "docker", out: "Error: No such object: hal-consul\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := newFakeRunner().on(tc.engine+" inspect", tc.out, errors.New("exit status 1"))
			h := runCommand(t, oneContainer, runner, tc.engine, nil)

			if !h.exited || h.exitCode != 1 {
				t.Fatalf("a missing container must exit 1, got exited=%v code=%d", h.exited, h.exitCode)
			}
			msg := h.stderr.String()
			for _, want := range []string{"hal-consul is not running", tc.engine, "hal consul status", "hal consul create"} {
				if !strings.Contains(msg, want) {
					t.Fatalf("message %q should contain %q", msg, want)
				}
			}
			if strings.Contains(msg, "panic") || strings.Contains(msg, "goroutine") {
				t.Fatalf("message must be a plain sentence, got %q", msg)
			}
			if h.stdout.Len() != 0 {
				t.Fatalf("stdout must stay empty on failure, got %q", h.stdout.String())
			}
			if h.runner.called(tc.engine + " logs") {
				t.Fatalf("logs must not be requested for a missing container: %v", h.runner.calls)
			}
		})
	}
}

func TestEngineErrorIsNotReportedAsNotRunning(t *testing.T) {
	runner := newFakeRunner().on("docker inspect", "Cannot connect to the Docker daemon at unix:///var/run/docker.sock\n", errors.New("exit status 1"))
	h := runCommand(t, oneContainer, runner, "docker", nil)

	if !h.exited || h.exitCode != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", h.exited, h.exitCode)
	}
	msg := h.stderr.String()
	if strings.Contains(msg, "is not running") {
		t.Fatalf("an unreachable engine must not be reported as 'not running': %q", msg)
	}
	if !strings.Contains(msg, "Cannot connect to the Docker daemon") {
		t.Fatalf("the engine's own message should be kept: %q", msg)
	}
}

func TestNoEngine(t *testing.T) {
	h := runCommand(t, oneContainer, newFakeRunner(), "", errors.New("no container engine found (make sure Docker or Podman is running)"))
	if !h.exited || h.exitCode != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", h.exited, h.exitCode)
	}
	if !strings.Contains(h.stderr.String(), "no container engine found") {
		t.Fatalf("stderr %q", h.stderr.String())
	}
	if len(h.runner.calls) != 0 {
		t.Fatalf("no engine means no command: %v", h.runner.calls)
	}
}

func TestStoppedContainerStillShowsItsLastLogs(t *testing.T) {
	runner := newFakeRunner().
		on("podman inspect", "exited\n", nil).
		on("podman logs --tail 100 hal-consul", "crash: bye\n", nil)
	h := runCommand(t, oneContainer, runner, "podman", nil)

	if h.exited {
		t.Fatalf("a stopped container with logs is not a failure, got exit %d", h.exitCode)
	}
	if h.stdout.String() != "crash: bye\n" {
		t.Fatalf("stdout %q", h.stdout.String())
	}
	if !strings.Contains(h.stderr.String(), "is exited (not running)") {
		t.Fatalf("stderr should say the container is stopped: %q", h.stderr.String())
	}
}

func TestLogsCommandFailure(t *testing.T) {
	runner := newFakeRunner().
		on("docker inspect", "running\n", nil).
		on("docker logs", "Error: boom\n", errors.New("exit status 125"))
	h := runCommand(t, oneContainer, runner, "docker", nil)

	if !h.exited || h.exitCode != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", h.exited, h.exitCode)
	}
	if !strings.Contains(h.stderr.String(), "cannot read logs of hal-consul") || !strings.Contains(h.stderr.String(), "boom") {
		t.Fatalf("stderr %q", h.stderr.String())
	}
	if h.stdout.Len() != 0 {
		t.Fatalf("stdout must stay empty, got %q", h.stdout.String())
	}
}

func TestEngineIsTakenFromDetection(t *testing.T) {
	for _, engine := range []string{"docker", "podman"} {
		runner := newFakeRunner().
			on(engine+" inspect", "running\n", nil).
			on(engine+" logs --tail 100 hal-consul", "ok\n", nil)
		h := runCommand(t, oneContainer, runner, engine, nil)
		if h.exited {
			t.Fatalf("%s: unexpected exit %d: %s", engine, h.exitCode, h.stderr.String())
		}
		for _, c := range h.runner.calls {
			if c.name != engine {
				t.Fatalf("%s: ran %q, every call must use the detected engine", engine, c)
			}
		}
	}
}

func TestOnlyReadOnlyEngineVerbsAreUsed(t *testing.T) {
	h := runCommand(t, manyContainers, runningContainer("hal-loki", "x\n"), "docker", nil, "--component", "loki")
	if h.exited {
		t.Fatalf("unexpected exit %d: %s", h.exitCode, h.stderr.String())
	}
	allowed := map[string]bool{"inspect": true, "logs": true}
	for _, c := range h.runner.calls {
		if !allowed[c.args[0]] {
			t.Fatalf("logs must only inspect and read logs, but ran %q", c)
		}
		if c.args[0] == "logs" {
			for _, a := range c.args {
				if a == "-f" || a == "--follow" {
					t.Fatalf("logs must never follow a stream: %q", c)
				}
			}
		}
	}
}

func TestComponentSelection(t *testing.T) {
	t.Run("default is the first component", func(t *testing.T) {
		runner := newFakeRunner().
			on("docker inspect", "running\n", nil).
			on("docker logs --tail 100 hal-grafana", "g\n", nil)
		h := runCommand(t, manyContainers, runner, "docker", nil)
		if h.exited || h.stdout.String() != "g\n" {
			t.Fatalf("exited=%v stdout=%q stderr=%q", h.exited, h.stdout.String(), h.stderr.String())
		}
	})
	t.Run("short and full names are both accepted", func(t *testing.T) {
		for _, name := range []string{"loki", "hal-loki", "LOKI"} {
			runner := newFakeRunner().
				on("docker inspect", "running\n", nil).
				on("docker logs --tail 100 hal-loki", "l\n", nil)
			h := runCommand(t, manyContainers, runner, "docker", nil, "--component", name)
			if h.exited || h.stdout.String() != "l\n" {
				t.Fatalf("--component %s: exited=%v stdout=%q stderr=%q", name, h.exited, h.stdout.String(), h.stderr.String())
			}
		}
	})
	t.Run("unknown component lists the valid ones and touches nothing", func(t *testing.T) {
		h := runCommand(t, manyContainers, newFakeRunner(), "docker", nil, "--component", "nope")
		if !h.exited || h.exitCode != 1 {
			t.Fatalf("want exit 1, got exited=%v code=%d", h.exited, h.exitCode)
		}
		for _, want := range []string{`unknown component "nope"`, "grafana", "loki", "prometheus"} {
			if !strings.Contains(h.stderr.String(), want) {
				t.Fatalf("stderr %q should contain %q", h.stderr.String(), want)
			}
		}
		if len(h.runner.calls) != 0 {
			t.Fatalf("no command expected, got %v", h.runner.calls)
		}
	})
	t.Run("a single-component product has no --component flag", func(t *testing.T) {
		h := runCommand(t, oneContainer, newFakeRunner(), "docker", nil, "--component", "x")
		if h.execErr == nil {
			t.Fatal("--component must not exist when there is only one source of logs")
		}
	})
}

func TestMultipassUnit(t *testing.T) {
	t.Run("running VM reads the journal with the bounded tail", func(t *testing.T) {
		runner := newFakeRunner().
			on("multipass info hal-nomad --format csv", "Name,State\nhal-nomad,Running\n", nil).
			on("multipass exec hal-nomad -- journalctl -u nomad -n 20 --no-pager", "-- Logs begin --\nnomad up\n", nil)
		h := runCommand(t, nomadSpec, runner, "docker", nil, "--tail", "20")
		if h.exited {
			t.Fatalf("unexpected exit %d: %s", h.exitCode, h.stderr.String())
		}
		if !strings.Contains(h.stdout.String(), "nomad up") {
			t.Fatalf("stdout %q", h.stdout.String())
		}
		// Multipass does not depend on the container engine.
		if h.runner.called("docker") || h.runner.called("podman") {
			t.Fatalf("nomad logs must not call the container engine: %v", h.runner.calls)
		}
	})
	t.Run("clamps like the container path", func(t *testing.T) {
		runner := newFakeRunner().
			on("multipass info", "hal-nomad,Running\n", nil).
			on("multipass exec hal-nomad -- journalctl -u nomad -n 1000 --no-pager", "x\n", nil)
		h := runCommand(t, nomadSpec, runner, "docker", nil, "--tail", "99999")
		if h.exited {
			t.Fatalf("unexpected exit %d: %s", h.exitCode, h.stderr.String())
		}
	})
	t.Run("missing VM", func(t *testing.T) {
		runner := newFakeRunner().on("multipass info", "info failed: instance \"hal-nomad\" does not exist\n", errors.New("exit status 2"))
		h := runCommand(t, nomadSpec, runner, "docker", nil)
		if !h.exited || h.exitCode != 1 {
			t.Fatalf("want exit 1, got exited=%v code=%d", h.exited, h.exitCode)
		}
		for _, want := range []string{"hal-nomad is not running", "hal nomad create"} {
			if !strings.Contains(h.stderr.String(), want) {
				t.Fatalf("stderr %q should contain %q", h.stderr.String(), want)
			}
		}
	})
	t.Run("stopped VM", func(t *testing.T) {
		runner := newFakeRunner().on("multipass info", "hal-nomad,Stopped\n", nil)
		h := runCommand(t, nomadSpec, runner, "docker", nil)
		if !h.exited || h.exitCode != 1 {
			t.Fatalf("want exit 1, got exited=%v code=%d", h.exited, h.exitCode)
		}
		if !strings.Contains(h.stderr.String(), "VM exists but is stopped") {
			t.Fatalf("stderr %q", h.stderr.String())
		}
		if h.runner.called("multipass exec") {
			t.Fatal("must not exec into a stopped VM")
		}
	})
	t.Run("multipass failure that is not 'does not exist' is reported as itself", func(t *testing.T) {
		runner := newFakeRunner().on("multipass info", "cannot connect to the multipass socket\n", errors.New("exit status 1"))
		h := runCommand(t, nomadSpec, runner, "docker", nil)
		if strings.Contains(h.stderr.String(), "is not running") {
			t.Fatalf("a daemon error is not 'not running': %q", h.stderr.String())
		}
		if !strings.Contains(h.stderr.String(), "cannot connect to the multipass socket") {
			t.Fatalf("stderr %q", h.stderr.String())
		}
	})
}

func TestOutputIsBoundedKeepingTheTail(t *testing.T) {
	var big strings.Builder
	line := strings.Repeat("x", 99) + "\n" // 100 bytes
	total := MaxOutputBytes/len(line) + 500
	for i := 0; i < total; i++ {
		fmt.Fprintf(&big, "%05d%s", i, line[5:])
	}
	last := fmt.Sprintf("%05d", total-1)

	h := runCommand(t, oneContainer, runningContainer("hal-consul", big.String()), "docker", nil)
	if h.exited {
		t.Fatalf("unexpected exit %d: %s", h.exitCode, h.stderr.String())
	}
	got := h.stdout.String()
	if len(got) > MaxOutputBytes+1 {
		t.Fatalf("stdout is %d bytes, cap is %d", len(got), MaxOutputBytes)
	}
	if !strings.Contains(got, last) {
		t.Fatalf("the tail (%s) must be kept", last)
	}
	if strings.HasPrefix(got, "00000") {
		t.Fatal("the head must be dropped")
	}
	if first := strings.SplitN(got, "\n", 2)[0]; len(first) != len(line)-1 {
		t.Fatalf("output must start on a line boundary, first line has %d bytes", len(first))
	}
	if !strings.Contains(h.stderr.String(), "output truncated") {
		t.Fatalf("a truncation note belongs on stderr, got %q", h.stderr.String())
	}
}

func TestBoundBytes(t *testing.T) {
	t.Run("short data is untouched", func(t *testing.T) {
		out, truncated := boundBytes([]byte("a\nb\n"), 100)
		if truncated || string(out) != "a\nb\n" {
			t.Fatalf("out=%q truncated=%v", out, truncated)
		}
	})
	t.Run("exactly at the cap is untouched", func(t *testing.T) {
		out, truncated := boundBytes([]byte("abcd"), 4)
		if truncated || string(out) != "abcd" {
			t.Fatalf("out=%q truncated=%v", out, truncated)
		}
	})
	t.Run("keeps the tail from a line start", func(t *testing.T) {
		out, truncated := boundBytes([]byte("11111\n22222\n33333\n"), 8)
		if !truncated || string(out) != "33333\n" {
			t.Fatalf("out=%q truncated=%v", out, truncated)
		}
	})
	t.Run("a single huge line is cut, not dropped", func(t *testing.T) {
		out, truncated := boundBytes([]byte("abcdefghij"), 4)
		if !truncated || string(out) != "ghij" {
			t.Fatalf("out=%q truncated=%v", out, truncated)
		}
	})
}

func TestCommandIsAnnotatedReadOnly(t *testing.T) {
	for _, spec := range []Spec{oneContainer, manyContainers, nomadSpec} {
		cmd := New(spec)
		if got := cmd.Annotations[global.AnnotationMCPReadOnly]; got != global.AnnotationValueTrue {
			t.Fatalf("hal %s logs: annotation %q = %q, want %q", spec.Product, global.AnnotationMCPReadOnly, got, global.AnnotationValueTrue)
		}
		if cmd.Use != "logs" {
			t.Fatalf("Use = %q", cmd.Use)
		}
	}
}

func TestHelpListsComponents(t *testing.T) {
	long := New(manyContainers).Long
	for _, want := range []string{"grafana", "loki", "prometheus", "hal-loki", fmt.Sprint(DefaultTail), fmt.Sprint(MaxTail)} {
		if !strings.Contains(long, want) {
			t.Fatalf("help should mention %q:\n%s", want, long)
		}
	}
}

func TestSpecResolveWithoutComponents(t *testing.T) {
	if _, err := (Spec{Product: "x"}).resolve(""); err == nil {
		t.Fatal("a product without log sources must be an error, not a panic")
	}
}

func TestContainerAndMultipassHelpers(t *testing.T) {
	c := Container("hal-vault-mariadb", "db")
	if c.Name != "vault-mariadb" || c.Target != "hal-vault-mariadb" || c.Kind != KindContainer {
		t.Fatalf("%+v", c)
	}
	m := MultipassUnit("hal-nomad", "nomad", "j")
	if m.Name != "nomad" || m.Target != "hal-nomad" || m.Unit != "nomad" || m.Kind != KindMultipassUnit {
		t.Fatalf("%+v", m)
	}
}
