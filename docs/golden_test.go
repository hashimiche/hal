package topics

import (
	"io/fs"
	"os"
	"reflect"
	"strings"
	"testing"
)

// These tests read the real files under docs/topics, once from the disk and once
// from the embed. They are the done-criteria of the golden Vault JWT topic.

// source is one way of reaching the topics.
type source struct {
	name string
	fsys func(t *testing.T) fs.FS
}

func sources() []source {
	return []source{
		{"disk", func(t *testing.T) fs.FS {
			t.Helper()
			// go test runs in the package directory, which is docs/.
			if _, err := os.Stat("topics"); err != nil {
				t.Fatalf("docs/topics is not next to the test: %v", err)
			}
			return os.DirFS("topics")
		}},
		{"embed", func(t *testing.T) fs.FS {
			t.Helper()
			fsys, err := Embedded()
			if err != nil {
				t.Fatal(err)
			}
			return fsys
		}},
	}
}

func TestGoldenTopicsLoad(t *testing.T) {
	for _, src := range sources() {
		t.Run(src.name, func(t *testing.T) {
			fsys := src.fsys(t)

			jwt, err := Load(fsys, "vault/jwt")
			if err != nil {
				t.Fatalf("vault/jwt: %v", err)
			}
			// The header is the contract of ADR 0006, word for word.
			if want := "GitLab CI logs into Vault with a JWT bound to a tag."; jwt.Summary != want {
				t.Errorf("summary = %q, want %q", jwt.Summary, want)
			}
			if want := []string{"hal vault jwt", "hal vault status", "hal creds status"}; !reflect.DeepEqual(jwt.Read, want) {
				t.Errorf("read = %v, want %v", jwt.Read, want)
			}
			if want := []string{"hal vault jwt enable", "hal vault jwt update", "hal vault jwt disable"}; !reflect.DeepEqual(jwt.Actions, want) {
				t.Errorf("actions = %v, want %v", jwt.Actions, want)
			}
			wantDocs := []Doc{
				{Title: "Vault JWT auth", URL: "https://developer.hashicorp.com/vault/docs/auth/jwt#bound-claims"},
				{Title: "GitLab CI JWT", URL: "https://docs.gitlab.com/ci/secrets/id_token_authentication/#token-payload"},
			}
			if !reflect.DeepEqual(jwt.Docs, wantDocs) {
				t.Errorf("docs = %v, want %v", jwt.Docs, wantDocs)
			}
			if jwt.Body == "" {
				t.Error("vault/jwt has no prose")
			}

			index, err := Load(fsys, "vault")
			if err != nil {
				t.Fatalf("vault: %v", err)
			}
			if index.ID != "vault" || index.Summary == "" || index.Body == "" {
				t.Errorf("vault index is incomplete: %+v", index)
			}
			if len(index.Actions) == 0 || len(index.Read) == 0 {
				t.Errorf("vault index has no read or actions: %+v", index)
			}
			// `hal vault delete` destroys the whole Vault lab: the lesson never offers
			// it as an action, the learner types it in a terminal.
			for _, a := range index.Actions {
				if strings.Contains(a, "delete") {
					t.Errorf("vault index offers %q as an action", a)
				}
			}
			// The index points to the JWT topic and does not copy it.
			if !strings.Contains(index.Body, "`vault/jwt`") {
				t.Error("the index does not point to vault/jwt")
			}
			for _, copied := range []string{"cicd-role", "bound claims", "kv-jwt", "secret-zero"} {
				if strings.Contains(index.Body, copied) {
					t.Errorf("the index repeats the JWT topic (%q)", copied)
				}
			}

			all, err := LoadAll(fsys)
			if err != nil {
				t.Fatalf("LoadAll: %v", err)
			}
			var ids []string
			for _, topic := range all {
				ids = append(ids, topic.ID)
			}
			for _, want := range []string{"vault", "vault/jwt"} {
				found := false
				for _, id := range ids {
					found = found || id == want
				}
				if !found {
					t.Errorf("LoadAll misses %s, got %v", want, ids)
				}
			}
		})
	}
}

// TestDiskAndEmbedAgree catches an embed that is stale: both copies must give
// the same topics. They are the same files at build time, so a difference means
// the embed was built from other files than the ones on disk.
func TestDiskAndEmbedAgree(t *testing.T) {
	var loaded [][]Topic
	for _, src := range sources() {
		topics, err := LoadAll(src.fsys(t))
		if err != nil {
			t.Fatalf("%s: %v", src.name, err)
		}
		loaded = append(loaded, topics)
	}
	if !reflect.DeepEqual(loaded[0], loaded[1]) {
		t.Error("the embedded topics differ from docs/topics on disk")
	}
}

// TestNoOperatorLoginInTopics is the guard of ADR 0006: no topic file holds an
// operator login. It reads every topic, not only the golden ones, so each file
// added later is checked without anyone remembering to.
func TestNoOperatorLoginInTopics(t *testing.T) {
	for _, src := range sources() {
		t.Run(src.name, func(t *testing.T) {
			topics, err := LoadAll(src.fsys(t))
			if err != nil {
				t.Fatal(err)
			}
			if len(topics) == 0 {
				t.Fatal("no topic to check")
			}
			for _, topic := range topics {
				if err := Check(topic, labSecrets...); err != nil {
					t.Error(err)
				}
			}
		})
	}
}

// TestGoldenTopicsStayOutOfTheRunbookHabits holds the golden file to the other
// rule of the plan: it must not teach VAULT_TOKEN=root nor `vault read` as a
// procedure, which is what the old skills did.
func TestGoldenTopicsStayOutOfTheRunbookHabits(t *testing.T) {
	fsys, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"vault", "vault/jwt"} {
		topic, err := Load(fsys, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"VAULT_TOKEN", "vault read", "vault login", "vault kv", "hvs."} {
			if strings.Contains(topic.Text, banned) {
				t.Errorf("%s contains %q", id, banned)
			}
		}
	}
}

// TestJWTTopicStatesTheLabGap checks that the prose carries the facts the plan
// asks for: how the lab differs from the textbook.
func TestJWTTopicStatesTheLabGap(t *testing.T) {
	fsys, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	topic, err := Load(fsys, "vault/jwt")
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range []string{
		"GitLab is the issuer",
		"`cicd-role`",
		"project and the ref",
		"CI authentication",
		"browser SSO",
		"OIDC topic",
	} {
		if !strings.Contains(topic.Body, fact) {
			t.Errorf("the JWT prose does not state %q", fact)
		}
	}
}
