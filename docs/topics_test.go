package topics

import (
	"reflect"
	"strings"
	"testing"
)

// These tests build every topic they parse from the fixtures below. They need no
// file under docs/topics.

// adrJWT is the header of ADR 0006, character for character, with a short body.
const adrJWT = `summary: GitLab CI logs into Vault with a JWT bound to a tag.
read: hal vault jwt | hal vault status | hal creds status
actions: hal vault jwt enable | hal vault jwt update | hal vault jwt disable
docs:
- Vault JWT auth: https://developer.hashicorp.com/vault/docs/auth/jwt#bound-claims
- GitLab CI JWT: https://docs.gitlab.com/ci/secrets/id_token_authentication/#token-payload

# Vault JWT auth with GitLab CI

GitLab is the issuer.
`

const indexFixture = `summary: Vault in this lab.
docs:
- What is Vault: https://developer.hashicorp.com/vault/docs/about-vault/what-is-vault#what-is-vault
- Dev server mode: https://developer.hashicorp.com/vault/docs/concepts/dev-server#properties

Prose.
`

// withHeader builds a topic from header lines, so a case only spells out what it
// changes.
func withHeader(lines ...string) string {
	return strings.Join(lines, "\n") + "\n\nbody\n"
}

const (
	twoDocs = "docs:\n- A: https://example.com/a#x\n- B: https://example.com/b#y"
	sum     = "summary: S."
)

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		path string
		src  string
		want Topic
	}{
		{
			name: "the JWT example of the ADR",
			path: "vault/jwt.md",
			src:  adrJWT,
			want: Topic{
				ID:      "vault/jwt",
				Summary: "GitLab CI logs into Vault with a JWT bound to a tag.",
				Read:    []string{"hal vault jwt", "hal vault status", "hal creds status"},
				Actions: []string{"hal vault jwt enable", "hal vault jwt update", "hal vault jwt disable"},
				Docs: []Doc{
					{Title: "Vault JWT auth", URL: "https://developer.hashicorp.com/vault/docs/auth/jwt#bound-claims"},
					{Title: "GitLab CI JWT", URL: "https://docs.gitlab.com/ci/secrets/id_token_authentication/#token-payload"},
				},
				Body: "# Vault JWT auth with GitLab CI\n\nGitLab is the issuer.",
			},
		},
		{
			name: "an index.md is the product topic, with no read and no actions",
			path: "vault/index.md",
			src:  indexFixture,
			want: Topic{
				ID:      "vault",
				Summary: "Vault in this lab.",
				Docs: []Doc{
					{Title: "What is Vault", URL: "https://developer.hashicorp.com/vault/docs/about-vault/what-is-vault#what-is-vault"},
					{Title: "Dev server mode", URL: "https://developer.hashicorp.com/vault/docs/concepts/dev-server#properties"},
				},
				Body: "Prose.",
			},
		},
		{
			name: "three documents is the most allowed",
			path: "consul/acl.md",
			src:  withHeader(sum, "docs:", "- A: https://example.com/a#a", "- B: https://example.com/b#b", "- C: https://example.com/c#c"),
			want: Topic{
				ID: "consul/acl", Summary: "S.",
				Docs: []Doc{
					{Title: "A", URL: "https://example.com/a#a"},
					{Title: "B", URL: "https://example.com/b#b"},
					{Title: "C", URL: "https://example.com/c#c"},
				},
				Body: "body",
			},
		},
		{
			name: "a title may hold a colon, the URL has none to confuse it",
			path: "vault/pki.md",
			src:  withHeader(sum, "docs:", "- PKI: the basics: https://example.com/pki#basics", "- B: https://example.com/b#y"),
			want: Topic{
				ID: "vault/pki", Summary: "S.",
				Docs: []Doc{
					{Title: "PKI: the basics", URL: "https://example.com/pki#basics"},
					{Title: "B", URL: "https://example.com/b#y"},
				},
				Body: "body",
			},
		},
		{
			name: "spacing around the pipe and the colon does not matter",
			path: "vault/ldap.md",
			src:  withHeader("summary:   S.  ", "read:hal  vault ldap|hal vault status", twoDocs),
			want: Topic{
				ID: "vault/ldap", Summary: "S.",
				Read: []string{"hal vault ldap", "hal vault status"},
				Docs: []Doc{
					{Title: "A", URL: "https://example.com/a#x"},
					{Title: "B", URL: "https://example.com/b#y"},
				},
				Body: "body",
			},
		},
		{
			name: "a read command may ask for help even if it names a verb",
			path: "vault/audit.md",
			src:  withHeader(sum, "read: hal vault create --help", twoDocs),
			want: Topic{
				ID: "vault/audit", Summary: "S.",
				Read: []string{"hal vault create --help"},
				Docs: []Doc{
					{Title: "A", URL: "https://example.com/a#x"},
					{Title: "B", URL: "https://example.com/b#y"},
				},
				Body: "body",
			},
		},
		{
			name: "CRLF line endings and a BOM are read like LF",
			path: "vault/os.md",
			src:  "\uFEFF" + strings.ReplaceAll(withHeader(sum, twoDocs), "\n", "\r\n"),
			want: Topic{
				ID: "vault/os", Summary: "S.",
				Docs: []Doc{
					{Title: "A", URL: "https://example.com/a#x"},
					{Title: "B", URL: "https://example.com/b#y"},
				},
				Body: "body",
			},
		},
		{
			name: "a key-looking line in the prose is prose, not header",
			path: "vault/aap.md",
			src:  withHeader(sum, twoDocs) + "\nread: hal delete\n- X: ftp://nowhere\n",
			want: Topic{
				ID: "vault/aap", Summary: "S.",
				Docs: []Doc{
					{Title: "A", URL: "https://example.com/a#x"},
					{Title: "B", URL: "https://example.com/b#y"},
				},
				Body: "body\n\nread: hal delete\n- X: ftp://nowhere",
			},
		},
		{
			name: "a header with no body",
			path: "vault/k8s.md",
			src:  sum + "\n" + twoDocs,
			want: Topic{
				ID: "vault/k8s", Summary: "S.",
				Docs: []Doc{
					{Title: "A", URL: "https://example.com/a#x"},
					{Title: "B", URL: "https://example.com/b#y"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.path, []byte(tt.src))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			// Text is the whole file, normalized; compare it apart from the fields.
			if !strings.Contains(got.Text, "summary:") {
				t.Errorf("Text does not hold the file: %q", got.Text)
			}
			got.Text = ""
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parse mismatch\n got: %#v\nwant: %#v", got, tt.want)
			}
		})
	}
}

func TestParseRejects(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		src     string
		wantErr string // a fragment the message must hold
	}{
		{
			name:    "a docs line without https",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "docs:", "- A: http://example.com/a#x", "- B: https://example.com/b#y"),
			wantErr: "must start with https://",
		},
		{
			name:    "a docs line with no scheme at all",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "docs:", "- A: example.com/a#x", "- B: https://example.com/b#y"),
			wantErr: "must start with https://",
		},
		{
			name:    "a docs line with a host missing",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "docs:", "- A: https:///a#x", "- B: https://example.com/b#y"),
			wantErr: "has no host",
		},
		{
			name:    "a docs URL with no anchor",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "docs:", "- A: https://example.com/a", "- B: https://example.com/b#y"),
			wantErr: "has no #anchor",
		},
		{
			name:    "a docs URL with an empty anchor",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "docs:", "- A: https://example.com/a#", "- B: https://example.com/b#y"),
			wantErr: "has no #anchor",
		},
		{
			name:    "a docs line with no URL",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "docs:", "- Just a title", "- B: https://example.com/b#y"),
			wantErr: `want "- Title: https://..."`,
		},
		{
			name:    "a docs line with no title",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "docs:", "- : https://example.com/a", "- B: https://example.com/b#y"),
			wantErr: "no title",
		},
		{
			name:    "a docs URL holding a space",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "docs:", "- A: https://example.com/a b", "- B: https://example.com/b#y"),
			wantErr: "holds a space",
		},
		{
			name:    "more than three documents",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "docs:", "- A: https://e.com/a#x", "- B: https://e.com/b#x", "- C: https://e.com/c#x", "- D: https://e.com/d#x"),
			wantErr: "has 4 documents, want 2 to 3",
		},
		{
			name:    "a single document",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "docs:", "- A: https://e.com/a#x"),
			wantErr: "has 1 documents, want 2 to 3",
		},
		{
			name:    "no docs at all",
			path:    "vault/jwt.md",
			src:     withHeader(sum),
			wantErr: "has 0 documents",
		},
		{
			name:    "a read command that does not start with hal",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "read: vault read auth/jwt/config", twoDocs),
			wantErr: `must start with "hal "`,
		},
		{
			name:    "a second read command that does not start with hal",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "read: hal vault status | docker ps", twoDocs),
			wantErr: `command "docker ps" must start with "hal "`,
		},
		{
			name:    "an actions command that does not start with hal",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "actions: ./hal vault jwt enable", twoDocs),
			wantErr: `must start with "hal "`,
		},
		{
			name:    "hal alone names no command",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "read: hal", twoDocs),
			wantErr: `must start with "hal "`,
		},
		{
			name:    "hal glued to another word is not hal",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "read: halt vault", twoDocs),
			wantErr: `must start with "hal "`,
		},
		{
			name:    "an empty item in a list",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "read: hal vault status | | hal vault jwt", twoDocs),
			wantErr: "empty command",
		},
		{
			name:    "an empty read value",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "read:", twoDocs),
			wantErr: "leave the key out",
		},
		{
			name:    "a read command that changes the lab",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "read: hal vault jwt enable", twoDocs),
			wantErr: "belongs under actions",
		},
		{
			name:    "a read command with the verb as a flag",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "read: hal vault jwt --disable", twoDocs),
			wantErr: "belongs under actions",
		},
		{
			name:    "no summary",
			path:    "vault/jwt.md",
			src:     withHeader("read: hal vault status", twoDocs),
			wantErr: "summary: is required",
		},
		{
			name:    "an empty summary",
			path:    "vault/jwt.md",
			src:     withHeader("summary:", twoDocs),
			wantErr: "summary: is empty",
		},
		{
			name:    "an unknown key",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "tags: vault", twoDocs),
			wantErr: `unknown header key "tags"`,
		},
		{
			name:    "a duplicate key",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "summary: again", twoDocs),
			wantErr: "already set on line 1",
		},
		{
			name:    "a line that is not key: value",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "just words", twoDocs),
			wantErr: "want key: value",
		},
		{
			name:    "a document line outside docs",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "read: hal vault status", "- A: https://e.com/a#x", "- B: https://e.com/b#x"),
			wantErr: "only valid under docs:",
		},
		{
			name:    "docs with a value on its own line",
			path:    "vault/jwt.md",
			src:     withHeader(sum, "docs: https://e.com/a#x", "- B: https://e.com/b#x"),
			wantErr: "docs: takes no value",
		},
		{
			name:    "YAML frontmatter",
			path:    "vault/jwt.md",
			src:     "---\nsummary: S.\n---\n" + twoDocs + "\n",
			wantErr: "YAML frontmatter is not used",
		},
		{
			name:    "an empty file",
			path:    "vault/jwt.md",
			src:     "",
			wantErr: "summary: is required",
		},
		{
			name:    "a blank first line leaves no header",
			path:    "vault/jwt.md",
			src:     "\n" + sum + "\n" + twoDocs + "\n",
			wantErr: "summary: is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.path, []byte(tt.src))
			if err == nil {
				t.Fatalf("Parse accepted the file, want an error holding %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not hold %q", err, tt.wantErr)
			}
			if !strings.Contains(err.Error(), "topic vault/jwt") {
				t.Errorf("error %q does not name the topic", err)
			}
		})
	}
}

func TestParseReportsTheLine(t *testing.T) {
	src := withHeader(sum, "read: hal vault status", "docs:", "- A: http://e.com/a", "- B: https://e.com/b#x")
	_, err := Parse("vault/jwt.md", []byte(src))
	if err == nil || !strings.Contains(err.Error(), "line 4") {
		t.Fatalf("want an error on line 4, got %v", err)
	}
}

func TestID(t *testing.T) {
	good := map[string]string{
		"vault/jwt.md":              "vault/jwt",
		"vault/index.md":            "vault",
		"terraform/vcs-workflow.md": "terraform/vcs-workflow",
		"boundary/ssh.md":           "boundary/ssh",
		"consul/index.md":           "consul",
	}
	for path, want := range good {
		got, err := ID(path)
		if err != nil || got != want {
			t.Errorf("ID(%q) = %q, %v; want %q", path, got, err, want)
		}
	}

	bad := []string{
		"vault/jwt",           // not .md
		"vault/jwt.txt",       // not .md
		"jwt.md",              // no product
		"index.md",            // no product
		"vault/sub/jwt.md",    // too deep
		"Vault/jwt.md",        // uppercase
		"vault/JWT.md",        // uppercase
		"vault/my_topic.md",   // underscore
		"vault/-jwt.md",       // leading hyphen
		"vault/jwt-.md",       // trailing hyphen
		"vault//jwt.md",       // empty segment
		"/vault/jwt.md",       // absolute
		"vault/jwt.md.md",     // dotted name
		"vault/.hidden.md",    // hidden
		"vault/with space.md", // space
	}
	for _, path := range bad {
		if got, err := ID(path); err == nil {
			t.Errorf("ID(%q) = %q, want an error", path, got)
		}
	}
}

func TestPath(t *testing.T) {
	for id, want := range map[string]string{
		"vault":                  "vault/index.md",
		"vault/jwt":              "vault/jwt.md",
		"terraform/vcs-workflow": "terraform/vcs-workflow.md",
	} {
		got, err := Path(id)
		if err != nil || got != want {
			t.Errorf("Path(%q) = %q, %v; want %q", id, got, err, want)
		}
		// ID and Path are inverses.
		back, err := ID(got)
		if err != nil || back != id {
			t.Errorf("ID(Path(%q)) = %q, %v", id, back, err)
		}
	}
	// "vault/index" is refused: the product topic is "vault".
	for _, id := range []string{"", "a/b/c", "Vault", "vault/", "../etc/passwd", "vault/jwt.md", "vault/index"} {
		if got, err := Path(id); err == nil {
			t.Errorf("Path(%q) = %q, want an error", id, got)
		}
	}
}

func TestDocAnchor(t *testing.T) {
	for url, want := range map[string]string{
		"https://developer.hashicorp.com/vault/docs/auth/jwt#bound-claims":          "bound-claims",
		"https://docs.gitlab.com/ci/secrets/id_token_authentication/":               "",
		"https://docs.gitlab.com/ci/secrets/id_token_authentication/#token-payload": "token-payload",
	} {
		if got := (Doc{URL: url}).Anchor(); got != want {
			t.Errorf("Anchor(%q) = %q, want %q", url, got, want)
		}
	}
}
