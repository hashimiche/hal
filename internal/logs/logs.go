// Package logs implements the read-only `hal <product> logs` command (ADR 0003,
// decision 8). Every product builds its command from a Spec, so the flags, the
// bounds, the engine handling and the "not running" behavior are identical
// everywhere and tested once.
//
// The command never follows a stream, never writes to the lab and never opens a
// shell: it runs `<engine> logs --tail N <container>` (or `journalctl` inside a
// Multipass VM) and prints the result.
package logs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

const (
	// DefaultTail is the number of lines printed when --tail is not given.
	DefaultTail = 100
	// MaxTail is the hard cap on --tail. Larger values are clamped, not rejected,
	// so a caller asking for "a lot" still gets an answer.
	MaxTail = 1000
	// MaxOutputBytes bounds the printed log even when MaxTail lines are huge
	// (single-line JSON logs can be tens of KiB each). Past the cap the tail is
	// kept, because that is where the errors are (ADR 0003, decision 10).
	MaxOutputBytes = 256 * 1024

	// commandTimeout keeps a hung engine from blocking a hal_read call forever.
	commandTimeout = 30 * time.Second

	// Exit codes. Every failure is 1, like the rest of the CLI: callers only need
	// to tell success from failure, and the reason is on stderr.
	exitOK      = 0
	exitFailure = 1
)

// Kind says where a component's logs live.
type Kind int

const (
	// KindContainer is a container on the Docker or Podman engine.
	KindContainer Kind = iota
	// KindMultipassUnit is a systemd unit inside a Multipass VM.
	KindMultipassUnit
)

// Component is one source of logs that a product owns.
type Component struct {
	// Name is what --component takes: the container name without its "hal-"
	// prefix (the full container name is accepted too).
	Name string
	// Target is the container name, or the Multipass instance name.
	Target string
	// Unit is the systemd unit, for KindMultipassUnit only.
	Unit string
	Kind Kind
	// Note is shown in --help next to the component name.
	Note string
}

// Container describes a container component. Its --component name is the
// container name without the "hal-" prefix, so the two cannot drift apart.
func Container(name, note string) Component {
	return Component{Name: strings.TrimPrefix(name, "hal-"), Target: name, Kind: KindContainer, Note: note}
}

// MultipassUnit describes a systemd unit inside a Multipass VM.
func MultipassUnit(instance, unit, note string) Component {
	return Component{Name: strings.TrimPrefix(instance, "hal-"), Target: instance, Unit: unit, Kind: KindMultipassUnit, Note: note}
}

// Spec describes the logs of one product. Components[0] is the default.
type Spec struct {
	// Product is the CLI name of the product, as typed after "hal" (for example "vault").
	Product    string
	Components []Component
}

// Runner runs an external command and returns its combined stdout and stderr.
// Container applications log to stderr, so both streams are the log.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Deps are the seams that unit tests replace. Production code uses defaultDeps.
type Deps struct {
	DetectEngine func() (string, error)
	Run          Runner
	Exit         func(code int)
}

func defaultDeps() Deps {
	return Deps{
		DetectEngine: global.DetectEngine,
		Run:          execRunner,
		Exit:         os.Exit,
	}
}

func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// New builds the `logs` command for a product.
func New(spec Spec) *cobra.Command {
	return newCommand(spec, defaultDeps())
}

func newCommand(spec Spec, d Deps) *cobra.Command {
	var (
		tail      int
		component string
	)

	cmd := &cobra.Command{
		Use:         "logs",
		Short:       fmt.Sprintf("Show the most recent %s logs (read-only)", spec.Product),
		Long:        longHelp(spec),
		Args:        cobra.NoArgs,
		Annotations: global.MCPReadOnlyAnnotations(),
		Run: func(cmd *cobra.Command, args []string) {
			code := execute(spec, component, tail, d, cmd.OutOrStdout(), cmd.ErrOrStderr())
			if code != exitOK {
				d.Exit(code)
			}
		},
	}

	cmd.Flags().IntVar(&tail, "tail", DefaultTail,
		fmt.Sprintf("Number of most recent log lines to print (max %d)", MaxTail))
	// A single-component product has nothing to choose from, so it gets no flag.
	if len(spec.Components) > 1 {
		cmd.Flags().StringVar(&component, "component", "",
			fmt.Sprintf("Which container to read (default %q)", spec.Components[0].Name))
	}
	return cmd
}

func longHelp(spec Spec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Print the most recent log lines of a hal %s component. Read-only: it never follows the stream and never changes the lab.\n\n", spec.Product)
	fmt.Fprintf(&b, "Output is bounded: %d lines by default, %d at most (larger --tail values are clamped), and %d KiB, keeping the most recent part.\n",
		DefaultTail, MaxTail, MaxOutputBytes/1024)
	if len(spec.Components) > 1 {
		nameWidth, targetWidth := 0, 0
		for _, c := range spec.Components {
			nameWidth = max(nameWidth, len(c.Name))
			targetWidth = max(targetWidth, len(c.Target))
		}
		fmt.Fprintf(&b, "\nWithout --component the primary container (%s) is read. Components:\n", spec.Components[0].Name)
		for _, c := range spec.Components {
			line := fmt.Sprintf("  %-*s  %-*s", nameWidth, c.Name, targetWidth, c.Target)
			if c.Note != "" {
				line += "  " + c.Note
			}
			b.WriteString(strings.TrimRight(line, " ") + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// BoundTail validates a --tail value. Values above MaxTail are clamped and a
// note explains it; values below 1 are rejected because "no lines" is never what
// the caller meant.
func BoundTail(requested int) (tail int, note string, err error) {
	if requested < 1 {
		return 0, "", fmt.Errorf("--tail must be at least 1 (got %d)", requested)
	}
	if requested > MaxTail {
		return MaxTail, fmt.Sprintf("--tail %d exceeds the cap of %d; showing the last %d lines", requested, MaxTail, MaxTail), nil
	}
	return requested, "", nil
}

// boundBytes keeps the last max bytes of data, starting on a line boundary.
func boundBytes(data []byte, max int) (out []byte, truncated bool) {
	if len(data) <= max {
		return data, false
	}
	cut := data[len(data)-max:]
	// Drop the partial first line so the output never starts mid-line.
	if i := bytes.IndexByte(cut, '\n'); i >= 0 && i < len(cut)-1 {
		cut = cut[i+1:]
	}
	return cut, true
}

// resolve picks the component named by --component, or the primary one.
func (s Spec) resolve(name string) (Component, error) {
	if len(s.Components) == 0 {
		return Component{}, fmt.Errorf("hal %s has no log source", s.Product)
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return s.Components[0], nil
	}
	name = strings.TrimPrefix(name, "hal-")
	for _, c := range s.Components {
		if c.Name == name {
			return c, nil
		}
	}
	names := make([]string, 0, len(s.Components))
	for _, c := range s.Components {
		names = append(names, c.Name)
	}
	sort.Strings(names)
	return Component{}, fmt.Errorf("unknown component %q for hal %s logs (valid: %s)", name, s.Product, strings.Join(names, ", "))
}

// execute runs the command and returns the process exit code. Logs go to out;
// every message about the run itself goes to errOut, so a pipe on stdout only
// ever sees log lines.
func execute(spec Spec, componentName string, requestedTail int, d Deps, out, errOut io.Writer) int {
	comp, err := spec.resolve(componentName)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return exitFailure
	}
	tail, note, err := BoundTail(requestedTail)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return exitFailure
	}
	if note != "" {
		fmt.Fprintln(errOut, note)
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	var raw []byte
	switch comp.Kind {
	case KindMultipassUnit:
		raw, err = multipassLogs(ctx, spec, comp, tail, d, errOut)
	default:
		raw, err = containerLogs(ctx, spec, comp, tail, d, errOut)
	}
	if err != nil {
		fmt.Fprintln(errOut, err)
		return exitFailure
	}

	data, truncated := boundBytes(raw, MaxOutputBytes)
	if truncated {
		fmt.Fprintf(errOut, "output truncated to the last %d KiB\n", MaxOutputBytes/1024)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		fmt.Fprintf(errOut, "%s has no log output yet\n", comp.Target)
		return exitOK
	}
	if _, err := out.Write(data); err != nil {
		fmt.Fprintf(errOut, "cannot write logs: %v\n", err)
		return exitFailure
	}
	if data[len(data)-1] != '\n' {
		fmt.Fprintln(out)
	}
	return exitOK
}

func notRunningHint(spec Spec) string {
	return fmt.Sprintf("Check 'hal %s status', or start it with 'hal %s create'.", spec.Product, spec.Product)
}

// containerLogs reads a container's logs through the detected engine.
//
// A container that does not exist is an error. One that exists but is stopped
// (exited, created, paused) still has logs, and those are the most useful ones
// after a crash, so they are printed with a note and the exit code stays 0.
func containerLogs(ctx context.Context, spec Spec, comp Component, tail int, d Deps, errOut io.Writer) ([]byte, error) {
	engine, err := d.DetectEngine()
	if err != nil {
		return nil, fmt.Errorf("cannot read logs: %w", err)
	}

	stateOut, err := d.Run(ctx, engine, "inspect", "-f", "{{.State.Status}}", comp.Target)
	if err != nil {
		detail := strings.TrimSpace(string(stateOut))
		// Docker says "No such object", Podman says "no such container". Only that
		// answer means the container is absent. Anything else (engine unreachable,
		// permission denied) is reported as what it is, not as "not running".
		if strings.Contains(strings.ToLower(detail), "no such") {
			return nil, fmt.Errorf("%s is not running: no container named %s on %s. %s",
				comp.Target, comp.Target, engine, notRunningHint(spec))
		}
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("cannot inspect %s on %s: %s", comp.Target, engine, detail)
	}

	if status := strings.TrimSpace(string(stateOut)); status != "running" {
		fmt.Fprintf(errOut, "%s is %s (not running); showing its last logs\n", comp.Target, status)
	}

	raw, err := d.Run(ctx, engine, "logs", "--tail", strconv.Itoa(tail), comp.Target)
	if err != nil {
		return nil, fmt.Errorf("cannot read logs of %s: %v\n%s", comp.Target, err, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}

// multipassLogs reads the journal of a systemd unit inside a Multipass VM.
func multipassLogs(ctx context.Context, spec Spec, comp Component, tail int, d Deps, errOut io.Writer) ([]byte, error) {
	info, err := d.Run(ctx, "multipass", "info", comp.Target, "--format", "csv")
	if err != nil {
		detail := strings.TrimSpace(string(info))
		if strings.Contains(strings.ToLower(detail), "does not exist") {
			return nil, fmt.Errorf("%s is not running: no Multipass VM with that name. %s", comp.Target, notRunningHint(spec))
		}
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("cannot query Multipass for %s: %s", comp.Target, detail)
	}
	if !strings.Contains(string(info), "Running") {
		return nil, fmt.Errorf("%s is not running: the VM exists but is stopped, so its journal cannot be read. %s", comp.Target, notRunningHint(spec))
	}

	raw, err := d.Run(ctx, "multipass", "exec", comp.Target, "--",
		"journalctl", "-u", comp.Unit, "-n", strconv.Itoa(tail), "--no-pager")
	if err != nil {
		return nil, fmt.Errorf("cannot read the %s journal of %s: %v\n%s", comp.Unit, comp.Target, err, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}
