package topics

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// The topic file contract is normative in ADR 0006 and in the plan's "Contrat"
// section. In short: there is no YAML frontmatter, the header is the only part
// the code parses, and it runs up to the first blank line.
//
//	summary: GitLab CI logs into Vault with a JWT bound to a tag.
//	read: hal vault jwt | hal vault status
//	actions: hal vault jwt enable | hal vault jwt update | hal vault jwt disable
//	docs:
//	- Vault JWT auth: https://developer.hashicorp.com/vault/docs/auth/jwt#bound-claims
//	- GitLab CI JWT: https://docs.gitlab.com/ci/secrets/id_token_authentication/
//
// Everything after the blank line is prose and is never parsed.

const (
	// MinDocs and MaxDocs bound the docs list: "two or three titles, URLs, and
	// anchors" (ADR 0006).
	MinDocs = 2
	MaxDocs = 3

	keySummary = "summary"
	keyRead    = "read"
	keyActions = "actions"
	keyDocs    = "docs"
)

// Doc is one entry of the docs list. The URL is the maintained part: HAL Plus
// fetches the page at question time and cuts the section at the fragment.
type Doc struct {
	Title string
	URL   string
}

// Anchor is the URL fragment, the section of the page the lesson is built on.
// Parse refuses a document without one (ADR 0006: "titles, URLs, and
// anchors"), so it is empty only for a Doc built by hand.
func (d Doc) Anchor() string {
	u, err := url.Parse(d.URL)
	if err != nil {
		return ""
	}
	return u.Fragment
}

// Topic is one parsed topic file.
type Topic struct {
	// ID is "vault/jwt" for vault/jwt.md and "vault" for vault/index.md.
	ID      string
	Summary string
	// Read are the commands run before the lesson is written. Actions are the
	// commands that change the lab. Each one starts with "hal ".
	Read    []string
	Actions []string
	Docs    []Doc
	// Body is the prose under the header.
	Body string
	// Text is the whole file, as the MCP resource serves it.
	Text string
}

var segmentRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ID derives a topic id from a slash-separated path relative to the topics
// root. "vault/jwt.md" is "vault/jwt", and "vault/index.md" is "vault": a file
// cannot sit beside a directory of the same name, so the product's own topic is
// its index. Anything else is refused, so the layout stays one level of
// products and one level of features.
func ID(path string) (string, error) {
	if !strings.HasSuffix(path, ".md") {
		return "", fmt.Errorf("topic path %q: not a .md file", path)
	}
	parts := strings.Split(strings.TrimSuffix(path, ".md"), "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("topic path %q: want <product>/<feature>.md or <product>/index.md", path)
	}
	for _, p := range parts {
		if !segmentRE.MatchString(p) {
			return "", fmt.Errorf("topic path %q: %q is not lowercase letters, digits and hyphens", path, p)
		}
	}
	if parts[1] == "index" {
		return parts[0], nil
	}
	return parts[0] + "/" + parts[1], nil
}

// Parse reads one topic file. path is slash-separated and relative to the
// topics root, and gives the id. The error names the topic and, when it can,
// the line.
func Parse(path string, src []byte) (Topic, error) {
	id, err := ID(path)
	if err != nil {
		return Topic{}, err
	}

	text := strings.TrimPrefix(string(src), "\uFEFF") // a BOM would hide the first key
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")

	t := Topic{ID: id, Text: text}
	fail := func(line int, format string, args ...any) (Topic, error) {
		msg := fmt.Sprintf(format, args...)
		if line > 0 {
			return Topic{}, fmt.Errorf("topic %s: line %d: %s", id, line, msg)
		}
		return Topic{}, fmt.Errorf("topic %s: %s", id, msg)
	}

	seen := map[string]int{}
	inDocs := false
	end := len(lines)
	for i, raw := range lines {
		n := i + 1
		line := strings.TrimSpace(raw)
		if line == "" {
			end = i
			break
		}

		if line == "---" {
			return fail(n, "YAML frontmatter is not used, the header starts with summary:")
		}
		if strings.HasPrefix(line, "-") {
			if !inDocs {
				return fail(n, "a document line is only valid under docs:")
			}
			doc, err := parseDoc(line)
			if err != nil {
				return fail(n, "%v", err)
			}
			t.Docs = append(t.Docs, doc)
			continue
		}

		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return fail(n, "want key: value, got %q", line)
		}
		value = strings.TrimSpace(value)
		switch key {
		case keySummary, keyRead, keyActions, keyDocs:
		default:
			return fail(n, "unknown header key %q (want summary, read, actions or docs)", key)
		}
		if first, dup := seen[key]; dup {
			return fail(n, "%s: already set on line %d", key, first)
		}
		seen[key] = n
		inDocs = key == keyDocs

		switch key {
		case keySummary:
			if value == "" {
				return fail(n, "summary: is empty")
			}
			t.Summary = value
		case keyRead:
			cmds, err := parseCommands(value)
			if err != nil {
				return fail(n, "read: %v", err)
			}
			if verb := mutatingVerb(cmds); verb != "" {
				return fail(n, "read: %q changes the lab, it belongs under actions:", verb)
			}
			t.Read = cmds
		case keyActions:
			cmds, err := parseCommands(value)
			if err != nil {
				return fail(n, "actions: %v", err)
			}
			t.Actions = cmds
		case keyDocs:
			if value != "" {
				return fail(n, "docs: takes no value, put one %q line per document under it", "- Title: https://...")
			}
		}
	}

	if _, ok := seen[keySummary]; !ok {
		return fail(0, "summary: is required")
	}
	if len(t.Docs) < MinDocs || len(t.Docs) > MaxDocs {
		return fail(0, "docs: has %d documents, want %d to %d", len(t.Docs), MinDocs, MaxDocs)
	}

	if end < len(lines) {
		t.Body = strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
	}
	return t, nil
}

// parseDoc reads "- Title: https://host/path#anchor". The URL has no space, so
// the last ": " is the one that ends the title, and a title may hold a colon.
func parseDoc(line string) (Doc, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(line, "-"))
	i := strings.LastIndex(rest, ": ")
	if i < 0 {
		return Doc{}, fmt.Errorf("want \"- Title: https://...\", got %q", line)
	}
	title, raw := strings.TrimSpace(rest[:i]), strings.TrimSpace(rest[i+2:])
	if title == "" {
		return Doc{}, fmt.Errorf("document has no title: %q", line)
	}
	if strings.ContainsAny(raw, " \t") {
		return Doc{}, fmt.Errorf("document URL %q holds a space", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Doc{}, fmt.Errorf("document URL %q: %v", raw, err)
	}
	if u.Scheme != "https" {
		return Doc{}, fmt.Errorf("document URL %q must start with https://", raw)
	}
	if u.Hostname() == "" {
		return Doc{}, fmt.Errorf("document URL %q has no host", raw)
	}
	if u.Fragment == "" {
		return Doc{}, fmt.Errorf("document URL %q has no #anchor: name the section the lesson is built on", raw)
	}
	return Doc{Title: title, URL: raw}, nil
}

// parseCommands splits a "|" separated list. Each command starts with "hal "
// and names something to run after it. An empty list is written by leaving the
// key out, so an empty value or an empty item is a mistake, not a choice.
func parseCommands(value string) ([]string, error) {
	if value == "" {
		return nil, fmt.Errorf("is empty, leave the key out instead")
	}
	var cmds []string
	for _, item := range strings.Split(value, "|") {
		fields := strings.Fields(item)
		if len(fields) == 0 {
			return nil, fmt.Errorf("empty command in %q", value)
		}
		if fields[0] != "hal" || len(fields) < 2 {
			return nil, fmt.Errorf("command %q must start with \"hal \" and name a command", strings.TrimSpace(item))
		}
		cmds = append(cmds, strings.Join(fields, " "))
	}
	return cmds, nil
}

// mutatingVerbs are the words a read command never carries (ADR 0006).
var mutatingVerbs = map[string]bool{
	"create": true, "update": true, "delete": true, "enable": true, "disable": true,
	"--create": true, "--update": true, "--delete": true, "--enable": true, "--disable": true,
}

// mutatingVerb returns the first mutating word found in the commands, or "".
// This is a lexical guard for the author. What really decides whether a command
// is read-only is the cobra tree and its hal.mcp/readonly annotation, which the
// CI test of the topics checks. A command asking for --help changes nothing.
func mutatingVerb(cmds []string) string {
	for _, cmd := range cmds {
		fields := strings.Fields(cmd)[1:]
		help := false
		for _, f := range fields {
			if f == "--help" || f == "-h" {
				help = true
			}
		}
		if help {
			continue
		}
		for _, f := range fields {
			if mutatingVerbs[f] {
				return f
			}
		}
	}
	return ""
}
