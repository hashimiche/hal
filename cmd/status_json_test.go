package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

// fakeSnapshot is a snapshot shaped like the real one, built with the exported
// types so a field rename in internal/global breaks this test at compile time.
func fakeSnapshot(engine string) ([]byte, error) {
	return json.Marshal(global.StatusSnapshot{
		Timestamp: "2026-11-04T10:00:00Z",
		Engine:    engine,
		Products: []global.ProductStatus{
			{
				Product: "vault", State: "running", Health: "healthy", Reason: "primary resource is running",
				Endpoint: "http://vault.localhost:8200", Containers: []string{"hal-vault"},
				Features: []global.ProductFeature{{Feature: "audit", State: "enabled", Health: "healthy", Reason: "feature is enabled"}},
			},
			{
				Product: "obs", State: "not_deployed", Health: "down", Reason: "required resources are not running",
				Endpoint: "http://grafana.localhost:3000", Containers: []string{"hal-grafana", "hal-prometheus", "hal-loki"},
				Features: []global.ProductFeature{{Feature: "loki", State: "disabled", Health: "down", Reason: "feature is disabled"}},
			},
		},
	})
}

func okDeps(exit func(int)) statusJSONDeps {
	return statusJSONDeps{
		detectEngine: func() (string, error) { return "podman", nil },
		snapshot:     fakeSnapshot,
		exit:         exit,
	}
}

func TestRunStatusJSONStdoutIsOnlyTheSnapshot(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runStatusJSON(&out, &errOut, okDeps(nil)); code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr should be empty on success, got %q", errOut.String())
	}

	raw := out.String()
	// Exactly one JSON document followed by one newline: nothing before, nothing after.
	if !strings.HasSuffix(raw, "}\n") || strings.Count(raw, "\n") != 1 {
		t.Fatalf("stdout must be one JSON line, got %q", raw)
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var snap global.StatusSnapshot
	if err := dec.Decode(&snap); err != nil {
		t.Fatalf("stdout is not a valid snapshot: %v\n%s", err, raw)
	}
	if dec.More() {
		t.Fatalf("trailing data after the JSON document: %q", raw)
	}
	if snap.Engine != "podman" || len(snap.Products) != 2 || snap.Products[0].Product != "vault" {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}

	// No ANSI escape and none of the human banner or emoji of the table view.
	for _, banned := range []string{"\x1b", "\033[", "HAL Global Deployment Status", "===", "💡", "🟢", "⚪", "[DEBUG]", "Tip:"} {
		if strings.Contains(raw, banned) {
			t.Fatalf("stdout must not contain %q: %s", banned, raw)
		}
	}
}

// hal-plus (runtime-status.mjs baselineProductsToUi) reads these keys. Keeping
// them is the contract of this command, so the test pins them by name.
func TestRunStatusJSONKeepsTheShapeHalPlusReads(t *testing.T) {
	var out bytes.Buffer
	runStatusJSON(&out, &bytes.Buffer{}, okDeps(nil))

	var top map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &top); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"timestamp", "engine", "products"} {
		if _, ok := top[key]; !ok {
			t.Fatalf("top-level key %q missing", key)
		}
	}
	if len(top) != 3 {
		t.Fatalf("unexpected top-level keys: %v", keys(top))
	}

	var products []map[string]json.RawMessage
	if err := json.Unmarshal(top["products"], &products); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"product", "state", "health", "reason", "endpoint", "containers", "features"} {
		if _, ok := products[0][key]; !ok {
			t.Fatalf("product key %q missing", key)
		}
	}
	var features []map[string]json.RawMessage
	if err := json.Unmarshal(products[0]["features"], &features); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"feature", "state", "health", "reason"} {
		if _, ok := features[0][key]; !ok {
			t.Fatalf("feature key %q missing", key)
		}
	}
}

func keys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestRunStatusJSONNoEngine(t *testing.T) {
	var out, errOut bytes.Buffer
	d := okDeps(nil)
	d.detectEngine = func() (string, error) {
		return "", errors.New("no container engine found (make sure Docker or Podman is running)")
	}
	d.snapshot = func(string) ([]byte, error) {
		t.Fatal("the snapshot must not be built without an engine")
		return nil, nil
	}

	if code := runStatusJSON(&out, &errOut, d); code == 0 {
		t.Fatal("a missing engine must give a non-zero exit code")
	}
	if out.Len() != 0 {
		t.Fatalf("stdout must stay empty on error, got %q", out.String())
	}
	if !strings.Contains(errOut.String(), "no container engine found") {
		t.Fatalf("stderr %q", errOut.String())
	}
}

func TestRunStatusJSONSnapshotFailure(t *testing.T) {
	var out, errOut bytes.Buffer
	d := okDeps(nil)
	d.snapshot = func(string) ([]byte, error) { return nil, errors.New("marshal exploded") }

	if code := runStatusJSON(&out, &errOut, d); code == 0 {
		t.Fatal("a snapshot failure must give a non-zero exit code")
	}
	if out.Len() != 0 {
		t.Fatalf("stdout must stay empty on error, got %q", out.String())
	}
	if !strings.Contains(errOut.String(), "marshal exploded") {
		t.Fatalf("stderr %q", errOut.String())
	}
}

// failingWriter simulates a closed pipe on stdout.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestRunStatusJSONWriteFailure(t *testing.T) {
	var errOut bytes.Buffer
	if code := runStatusJSON(failingWriter{}, &errOut, okDeps(nil)); code == 0 {
		t.Fatal("a write failure must give a non-zero exit code")
	}
	if !strings.Contains(errOut.String(), "broken pipe") {
		t.Fatalf("stderr %q", errOut.String())
	}
}

// runStatus drives the real `hal status` command through cobra, with the
// snapshot dependencies replaced, the way `hal status --json` is invoked.
func runStatus(t *testing.T, d statusJSONDeps, args ...string) (stdout, stderr string, exitCode int, exited bool) {
	t.Helper()
	prevRuntime, prevJSON := statusJSONRuntime, statusJSON
	t.Cleanup(func() {
		statusJSONRuntime, statusJSON = prevRuntime, prevJSON
		_ = statusCmd.Flags().Set("json", "false")
		statusCmd.Flags().Lookup("json").Changed = false
	})

	var out, errOut bytes.Buffer
	d.exit = func(code int) { exited, exitCode = true, code }
	statusJSONRuntime = d
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)
	rootCmd.SetArgs(append([]string{"status"}, args...))
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v (stderr: %s)", err, errOut.String())
	}
	return out.String(), errOut.String(), exitCode, exited
}

func TestStatusCommandJSONFlagEndToEnd(t *testing.T) {
	stdout, stderr, _, exited := runStatus(t, okDeps(nil), "--json")
	if exited {
		t.Fatalf("a successful run must not call exit; stderr: %s", stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr should be empty, got %q", stderr)
	}
	var snap global.StatusSnapshot
	if err := json.Unmarshal([]byte(stdout), &snap); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if snap.Engine != "podman" {
		t.Fatalf("engine = %q", snap.Engine)
	}
}

func TestStatusCommandJSONFlagErrorExitsNonZeroOnStderr(t *testing.T) {
	d := okDeps(nil)
	d.detectEngine = func() (string, error) { return "", errors.New("no container engine found") }

	stdout, stderr, code, exited := runStatus(t, d, "--json")
	if !exited || code == 0 {
		t.Fatalf("want a non-zero exit, got exited=%v code=%d", exited, code)
	}
	if stdout != "" {
		t.Fatalf("stdout must stay empty, got %q", stdout)
	}
	if !strings.Contains(stderr, "no container engine found") {
		t.Fatalf("stderr %q", stderr)
	}
}

// Without --json the command keeps its human table, which is not JSON. This
// guards against the flag being wired as "always on".
func TestStatusJSONFlagDefaultsToOff(t *testing.T) {
	f := statusCmd.Flags().Lookup("json")
	if f == nil {
		t.Fatal("hal status must define --json")
	}
	if f.DefValue != "false" {
		t.Fatalf("--json default = %q, want false", f.DefValue)
	}
}

func findCommand(root *cobra.Command, path ...string) *cobra.Command {
	cur := root
	for _, name := range path {
		var next *cobra.Command
		for _, c := range cur.Commands() {
			if c.Name() == name {
				next = c
				break
			}
		}
		if next == nil {
			return nil
		}
		cur = next
	}
	return cur
}

func TestStatusCommandIsAnnotatedReadOnly(t *testing.T) {
	if got := statusCmd.Annotations[global.AnnotationMCPReadOnly]; got != global.AnnotationValueTrue {
		t.Fatalf("hal status annotation %q = %q, want %q", global.AnnotationMCPReadOnly, got, global.AnnotationValueTrue)
	}
	if got := statusCmd.Annotations["hal.mcp/readonly"]; got != "true" {
		t.Fatalf("the literal ADR 0003 key/value must be hal.mcp/readonly = \"true\", got %q", got)
	}
}

// Every product that owns containers or a VM has a read-only `logs` command.
// The list is explicit so that adding a product forces a decision about its logs.
func TestEveryProductHasAReadOnlyLogsCommand(t *testing.T) {
	products := []string{"consul", "vault", "nomad", "boundary", "terraform", "obs"}
	for _, p := range products {
		logsCmd := findCommand(rootCmd, p, "logs")
		if logsCmd == nil {
			t.Fatalf("hal %s logs is not registered", p)
		}
		if got := logsCmd.Annotations["hal.mcp/readonly"]; got != "true" {
			t.Fatalf("hal %s logs: hal.mcp/readonly = %q, want \"true\"", p, got)
		}
		if f := logsCmd.Flags().Lookup("tail"); f == nil || f.DefValue != "100" {
			t.Fatalf("hal %s logs must have --tail with default 100, got %+v", p, f)
		}
		if !logsCmd.Runnable() {
			t.Fatalf("hal %s logs is not runnable", p)
		}
	}
}

// Products must not silently gain or lose a logs command: any `logs` command in
// the tree has to be one of the products above, and must be read-only.
func TestEveryLogsCommandInTheTreeIsReadOnly(t *testing.T) {
	known := map[string]bool{"consul": true, "vault": true, "nomad": true, "boundary": true, "terraform": true, "obs": true}
	var walk func(c *cobra.Command, path []string)
	walk = func(c *cobra.Command, path []string) {
		for _, sub := range c.Commands() {
			p := append(append([]string{}, path...), sub.Name())
			if sub.Name() == "logs" {
				if len(p) != 2 || !known[p[0]] {
					t.Errorf("unexpected logs command at hal %s", strings.Join(p, " "))
				}
				if sub.Annotations[global.AnnotationMCPReadOnly] != global.AnnotationValueTrue {
					t.Errorf("hal %s is not annotated read-only", strings.Join(p, " "))
				}
			}
			walk(sub, p)
		}
	}
	walk(rootCmd, nil)
}
