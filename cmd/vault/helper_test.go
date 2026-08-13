package vault

import (
	"strings"
	"testing"

	"hal/internal/global"
)

func TestKindClusterEnvExtras(t *testing.T) {
	docker := kindClusterEnvExtras("docker")
	if containsEnv(docker, "KIND_EXPERIMENTAL_PROVIDER=podman") {
		t.Fatalf("docker extras unexpectedly set KIND_EXPERIMENTAL_PROVIDER: %v", docker)
	}
	if !containsEnv(docker, "KIND_EXPERIMENTAL_DOCKER_NETWORK="+global.HalNetName) {
		t.Fatalf("docker extras missing KIND_EXPERIMENTAL_DOCKER_NETWORK: %v", docker)
	}
	if !containsEnv(docker, "KIND_EXPERIMENTAL_PODMAN_NETWORK="+global.HalNetName) {
		t.Fatalf("docker extras missing KIND_EXPERIMENTAL_PODMAN_NETWORK: %v", docker)
	}

	podman := kindClusterEnvExtras("podman")
	if !containsEnv(podman, "KIND_EXPERIMENTAL_PROVIDER=podman") {
		t.Fatalf("podman extras missing KIND_EXPERIMENTAL_PROVIDER: %v", podman)
	}
	if !containsEnv(podman, "KIND_EXPERIMENTAL_DOCKER_NETWORK="+global.HalNetName) {
		t.Fatalf("podman extras missing KIND_EXPERIMENTAL_DOCKER_NETWORK: %v", podman)
	}
	if !containsEnv(podman, "KIND_EXPERIMENTAL_PODMAN_NETWORK="+global.HalNetName) {
		t.Fatalf("podman extras missing KIND_EXPERIMENTAL_PODMAN_NETWORK: %v", podman)
	}
}

func TestHalNetIPInspectFormat(t *testing.T) {
	got := halNetIPInspectFormat()
	if !strings.Contains(got, global.HalNetName) {
		t.Fatalf("inspect format %q does not select %s", got, global.HalNetName)
	}
	if strings.Contains(got, "range .NetworkSettings.Networks}}{{.IPAddress}}") {
		t.Fatal("inspect format concatenates every NIC IP; it must select hal-net only")
	}
}

func TestIsAlreadyConnectedNetworkError(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"Error response from daemon: endpoint with name kind-control-plane already exists in network hal-net", true},
		{"Error: container kind-control-plane already connected to network hal-net", true},
		{"failed to start KinD: exit status 1", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isAlreadyConnectedNetworkError(tc.msg); got != tc.want {
			t.Errorf("isAlreadyConnectedNetworkError(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

func containsEnv(env []string, want string) bool {
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}
