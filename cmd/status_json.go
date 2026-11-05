package cmd

import (
	"fmt"
	"io"
	"os"

	"hal/internal/global"
)

// statusJSON is bound to `hal status --json`.
var statusJSON bool

// statusJSONDeps are the seams unit tests replace, so no real engine is needed.
type statusJSONDeps struct {
	detectEngine func() (string, error)
	snapshot     func(engine string) ([]byte, error)
	exit         func(code int)
}

var statusJSONRuntime = statusJSONDeps{
	detectEngine: global.DetectEngine,
	snapshot:     global.BuildStatusSnapshot,
	exit:         os.Exit,
}

// runStatusJSON prints the lab snapshot (global.BuildStatusSnapshot, the shape
// hal-plus reads) as one JSON document and returns the process exit code.
//
// The contract of --json is that stdout is the snapshot and nothing else: no
// banner, no ANSI, no progress text, so a machine can parse it. Every error goes
// to errOut with a non-zero exit. The error is written here rather than returned
// to cobra so that it is prefixed with the command and cobra does not append its
// usage text to a runtime failure.
func runStatusJSON(out, errOut io.Writer, d statusJSONDeps) int {
	engine, err := d.detectEngine()
	if err != nil {
		fmt.Fprintf(errOut, "hal status --json: %v\n", err)
		return 1
	}
	data, err := d.snapshot(engine)
	if err != nil {
		fmt.Fprintf(errOut, "hal status --json: cannot build the status snapshot: %v\n", err)
		return 1
	}
	if _, err := out.Write(append(data, '\n')); err != nil {
		fmt.Fprintf(errOut, "hal status --json: cannot write the snapshot: %v\n", err)
		return 1
	}
	return 0
}
