package vault

import (
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"hal/internal/global"
	"hal/internal/global/globaltest"
)

var (
	// The user's own default cluster: same name as HAL's, but never on hal-net.
	userKindNode = globaltest.Container{ID: "usr0000000001", Name: "kind-control-plane", Cluster: "kind", Networks: []string{"kind"}}
	// HAL's cluster, as ensureHALKindCluster leaves it.
	halKindNode = globaltest.Container{ID: "hal0000000001", Name: "kind-control-plane", Cluster: "kind", Networks: []string{"hal-net"}}
)

func TestHalKindClusterRunningIgnoresUsersCluster(t *testing.T) {
	globaltest.NewLab(t, []globaltest.Container{userKindNode}, nil)
	if halKindClusterRunning() {
		t.Fatal("a user's default kind cluster was treated as HAL's")
	}
}

func TestHalKindClusterRunningSeesHALsCluster(t *testing.T) {
	globaltest.NewLab(t, []globaltest.Container{halKindNode}, nil)
	if !halKindClusterRunning() {
		t.Fatal("HAL's own cluster was not recognized")
	}
}

func TestDestroyHALKindClusterLeavesUsersClusterAlone(t *testing.T) {
	lab := globaltest.NewLab(t, []globaltest.Container{userKindNode}, nil)

	out := captureOutput(t, func() { destroyHALKindCluster("docker") })

	if got := lab.ContainerIDs(); !reflect.DeepEqual(got, []string{userKindNode.ID}) {
		t.Fatalf("user's cluster was removed: %v", got)
	}
	for _, line := range lab.Lines() {
		if strings.HasPrefix(line, "kind delete") || strings.Contains(line, " rm ") {
			t.Fatalf("destructive call against a foreign cluster: %s", line)
		}
	}
	if !strings.Contains(out, "not created by HAL") {
		t.Fatalf("user is not told why the cluster was kept:\n%s", out)
	}
}

func TestDestroyHALKindClusterDeletesHALsCluster(t *testing.T) {
	lab := globaltest.NewLab(t, []globaltest.Container{halKindNode}, nil)

	captureOutput(t, func() { destroyHALKindCluster("docker") })

	if got := lab.ContainerIDs(); len(got) != 0 {
		t.Fatalf("HAL's cluster survived: %v", got)
	}
	if !lab.Ran("kind delete cluster --name kind") {
		t.Fatalf("expected an explicit --name delete; ran: %v", lab.Lines())
	}
}

func TestEnsureHALKindClusterRefusesUsersCluster(t *testing.T) {
	lab := globaltest.NewLab(t, []globaltest.Container{userKindNode}, nil)

	var err error
	captureOutput(t, func() { err = ensureHALKindCluster("docker", "") })

	if err == nil || !strings.Contains(err.Error(), "did not create it") {
		t.Fatalf("err = %v, want a refusal to reuse the user's cluster", err)
	}
	// Reusing it would attach it to hal-net and turn it into a "HAL cluster".
	for _, line := range lab.Lines() {
		if strings.Contains(line, "network connect") || strings.HasPrefix(line, "kind create") || strings.HasPrefix(line, "kind delete") {
			t.Fatalf("touched the user's cluster: %s", line)
		}
	}
	if got := lab.ContainerIDs(); !reflect.DeepEqual(got, []string{userKindNode.ID}) {
		t.Fatalf("user's cluster changed: %v", got)
	}
}

func TestEnsureHALKindClusterCreatesNamedClusterOnHalNet(t *testing.T) {
	lab := globaltest.NewLab(t, nil, nil)

	var err error
	captureOutput(t, func() { err = ensureHALKindCluster("docker", "") })
	if err != nil {
		t.Fatal(err)
	}

	var create *globaltest.Call
	for _, c := range lab.Calls() {
		if c.Name == "kind" && len(c.Args) > 0 && c.Args[0] == "create" {
			c := c
			create = &c
		}
	}
	if create == nil {
		t.Fatalf("kind create was never run; ran: %v", lab.Lines())
	}
	if create.Args[2] != "--name" || create.Args[3] != global.KindClusterName {
		t.Fatalf("kind create must pass --name %s explicitly: %v", global.KindClusterName, create.Args)
	}
	if !halKindClusterRunning() {
		t.Fatal("the cluster ensureHALKindCluster just made is not recognized as HAL's, so hal delete would leave it behind")
	}
}

func TestEnsureHALKindClusterReusesHALsCluster(t *testing.T) {
	lab := globaltest.NewLab(t, []globaltest.Container{halKindNode}, nil)

	var err error
	captureOutput(t, func() { err = ensureHALKindCluster("docker", "") })
	if err != nil {
		t.Fatal(err)
	}
	if lab.Ran("kind create") {
		t.Fatalf("recreated HAL's existing cluster; ran: %v", lab.Lines())
	}
}

func TestEnsureHALKindClusterRollsBackWhenAttachFails(t *testing.T) {
	// kind ignores the network env and hal-net cannot be joined afterwards. The
	// new cluster would fail the ownership test and be stranded, so it is removed.
	lab := globaltest.NewLab(t, nil, nil)
	lab.KindIgnoresNetworkEnv = true
	lab.RefuseNetworkConnect = true

	var err error
	captureOutput(t, func() { err = ensureHALKindCluster("docker", "") })

	if err == nil {
		t.Fatal("expected the attach failure to be reported")
	}
	if got := lab.ContainerIDs(); len(got) != 0 {
		t.Fatalf("a cluster HAL cannot own was left behind: %v", got)
	}
}

// captureOutput runs fn and returns what it printed to stdout.
func captureOutput(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	os.Stdout = old
	_ = w.Close()
	return <-done
}
