package cmd

import (
	"fmt"
	"testing"
)

func TestIsHALKindCluster(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"kind", true},
		{"hal-k8s", true},
		{"hal-demo", true},
		{"kind-dev", false},
		{"other", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isHALKindCluster(tc.name); got != tc.want {
			t.Errorf("isHALKindCluster(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestParseKindClustersOutput(t *testing.T) {
	got := parseKindClustersOutput("kind\nhal-k8s\n")
	if len(got) != 2 || got[0] != "kind" || got[1] != "hal-k8s" {
		t.Fatalf("parseKindClustersOutput = %v, want [kind hal-k8s]", got)
	}
	if got := parseKindClustersOutput(""); len(got) != 0 {
		t.Fatalf("empty input = %v, want none", got)
	}
}

func TestIsKindPodmanLabelTemplateError(t *testing.T) {
	userWarning := `exit status 1: using podman due to KIND_EXPERIMENTAL_PROVIDER
enabling experimental podman provider
ERROR: failed to list clusters: command "podman ps -a --filter label=io.x-k8s.kind.cluster --format '{{index .Labels "io.x-k8s.kind.cluster"}}'" failed with error: exit status 125

Command Output: Error: template: ps:1:13: executing "ps" at <index .Labels "io.x-k8s.kind.cluster">: error calling index: cannot index slice/array with type string`
	if !isKindPodmanLabelTemplateError(fmt.Errorf("%s", userWarning)) {
		t.Fatal("expected Podman 6 Labels-template error to be recognized")
	}
	if isKindPodmanLabelTemplateError(fmt.Errorf("executable file not found in $PATH")) {
		t.Fatal("missing kind binary should still surface")
	}
	if isKindPodmanLabelTemplateError(nil) {
		t.Fatal("nil error should not match")
	}
}

func TestUniqueStrings(t *testing.T) {
	got := uniqueStrings([]string{"kind", "", "kind", "hal-k8s"})
	if len(got) != 2 || got[0] != "kind" || got[1] != "hal-k8s" {
		t.Fatalf("uniqueStrings = %v, want [kind hal-k8s]", got)
	}
}
