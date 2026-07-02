// Package ui provides a consistent, installer-style output for hal commands.
//
// The goal is to standardize the "what is happening" progress output and the
// "command is done" summary across all products. Long-running flows show a
// single updating line (an animated spinner) where each stage overwrites the
// previous one, like a software installer. When stdout is not a terminal, or
// when --verbose is set, the output degrades to one plain line per step so logs
// and MCP transports stay clean and complete.
//
// Typical usage:
//
//	ui.Title("hal vault oidc — deploying Authentik IdP + Vault OIDC")
//	ui.Start()
//	ui.Step("Starting Authentik stack")
//	ui.Step("Configuring Vault OIDC")
//	ui.Stop()
//	ui.Success("Vault OIDC + Authentik IdP ready")
//	ui.Section("Demo users")
//	ui.Item("alice / password  →  admin policy")
package ui

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Verbose, when true, persists every step as its own line even on a TTY.
// It is wired to the global --verbose flag in cmd/root.go.
var Verbose bool

// spinnerFrames are Braille dots that animate smoothly in a single cell.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// current holds the in-flight progress flow, if any. The CLI is single-threaded
// per command, so a package-level handle lets nested helpers (e.g. the Authentik
// integration) emit steps via ui.Step without threading a handle everywhere.
var current *spinner

type spinner struct {
	mu       sync.Mutex
	msg      string
	animated bool // true only on a TTY without --verbose
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// isTTY reports whether stdout is an interactive terminal capable of cursor
// control. A "dumb" terminal (TERM=dumb) is treated as non-interactive so
// animations degrade to plain output.
func isTTY() bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// Title prints a persistent header line that introduces a flow. It always
// prints (TTY or not) and is never overwritten by the spinner.
func Title(format string, args ...any) {
	fmt.Printf("🔧 %s\n\n", fmt.Sprintf(format, args...))
}

// Start begins an installer-style progress flow. On a TTY (and without
// --verbose) it launches an animated single-line spinner that subsequent
// Step calls update in place. Otherwise it is a no-op and Step prints plain
// lines.
func Start() {
	s := &spinner{
		animated: isTTY() && !Verbose,
		stopCh:   make(chan struct{}),
	}
	current = s
	if !s.animated {
		return
	}
	s.wg.Add(1)
	go s.run()
}

// Step advances the flow to a new stage. In animated mode it replaces the
// current line; otherwise it prints a plain one-line-per-step entry.
func Step(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	s := current
	if s == nil || !s.animated {
		fmt.Printf("  → %s\n", msg)
		return
	}
	s.mu.Lock()
	s.msg = msg
	s.mu.Unlock()
}

// Stop ends the flow and clears the active spinner line. Call it before
// printing a summary or an error so the line does not linger.
func Stop() {
	s := current
	current = nil
	if s == nil || !s.animated {
		return
	}
	close(s.stopCh)
	s.wg.Wait()
	fmt.Print("\r\033[K") // return to col 0 and clear to end of line
}

// Fail stops the flow (clearing the spinner line) and prints an error line.
func Fail(format string, args ...any) {
	Stop()
	fmt.Printf("❌ %s\n", fmt.Sprintf(format, args...))
}

func (s *spinner) run() {
	defer s.wg.Done()
	ticker := time.NewTicker(90 * time.Millisecond)
	defer ticker.Stop()
	i := 0
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.mu.Lock()
			msg := s.msg
			s.mu.Unlock()
			if msg != "" {
				fmt.Printf("\r\033[K  %s %s", spinnerFrames[i%len(spinnerFrames)], msg)
			}
			i++
		}
	}
}

// ─── Summary primitives ────────────────────────────────────────────────────────
// These produce a consistent "command is done" block across products.

// Success prints the headline result line of a completed flow.
func Success(format string, args ...any) {
	fmt.Printf("\n✅ %s\n", fmt.Sprintf(format, args...))
}

// Section prints an indented section heading inside a summary block.
func Section(format string, args ...any) {
	fmt.Printf("\n  %s\n", fmt.Sprintf(format, args...))
}

// Item prints an indented list item inside a section.
func Item(format string, args ...any) {
	fmt.Printf("    %s\n", fmt.Sprintf(format, args...))
}

// Field prints an aligned "label : value" line inside a section.
func Field(label, value string) {
	fmt.Printf("    %-10s %s\n", label+":", value)
}

// Hint prints a trailing next-step suggestion.
func Hint(format string, args ...any) {
	fmt.Printf("\n💡 %s\n", fmt.Sprintf(format, args...))
}
