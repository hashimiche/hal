package labsecrets

import (
	"fmt"
	"io"
	"strings"
)

// Render prints the whole active tree: every running product with its
// scenarios nested underneath.
func Render(w io.Writer, t Tree) {
	for _, section := range t.Sections {
		renderSection(w, section, 0)
	}
}

// RenderSection prints one product's block, for a product status command.
// It reports whether the product had anything to show.
func RenderSection(w io.Writer, t Tree, key string) bool {
	section, ok := t.Section(key)
	if !ok {
		return false
	}
	renderSection(w, section, 0)
	return true
}

func renderSection(w io.Writer, s Section, depth int) {
	pad := strings.Repeat("   ", depth+1)
	fmt.Fprintf(w, "%s%s %s\n", strings.Repeat("   ", depth), s.Icon, s.Label)

	width := labelWidth(s)
	if s.URL != "" {
		fmt.Fprintf(w, "%s%s : %s\n", pad, padRight("URL", width), s.URL)
	}
	for _, entry := range s.Entries {
		fmt.Fprintf(w, "%s%s\n", pad, renderEntry(entry, width, pairWidth(s)))
	}
	if len(s.Commands) > 0 {
		fmt.Fprintln(w)
		for _, command := range s.Commands {
			fmt.Fprintf(w, "%s%s\n", pad, command)
		}
	}
	fmt.Fprintln(w)

	for _, nested := range s.Sections {
		renderSection(w, nested, depth+1)
	}
}

// renderEntry prints a labelled value ("Root token : root"), a labelled login
// ("Admin : haladmin / hal9000FTW") or a bare demo login ("alice / password"),
// each with its note.
func renderEntry(e Entry, width, pair int) string {
	value := e.Secret
	if e.Username != "" && e.Secret != "" {
		value = padRight(e.Username, pair) + " / " + e.Secret
	} else if e.Username != "" {
		value = e.Username
	}

	if value == "" {
		value = fmt.Sprintf("(%s)", e.Note)
		if e.Note == "" {
			value = "(not available)"
		}
		if e.Name == "" {
			return value
		}
		return padRight(e.Name, width) + " : " + value
	}

	line := value
	if e.Name != "" {
		line = padRight(e.Name, width) + " : " + value
	}
	if e.Note != "" {
		line += "  →  " + e.Note
	}
	return line
}

// labelWidth aligns the labelled entries of one section with each other.
func labelWidth(s Section) int {
	width := 0
	if s.URL != "" {
		width = len("URL")
	}
	for _, entry := range s.Entries {
		if len(entry.Name) > width {
			width = len(entry.Name)
		}
	}
	return width
}

// pairWidth aligns the usernames of the demo logins in one section.
func pairWidth(s Section) int {
	width := 0
	for _, entry := range s.Entries {
		if entry.Secret != "" && len(entry.Username) > width {
			width = len(entry.Username)
		}
	}
	return width
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
