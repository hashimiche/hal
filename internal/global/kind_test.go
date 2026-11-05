package global_test

import (
	"reflect"
	"strings"
	"testing"

	"hal/internal/global"
	"hal/internal/global/globaltest"
)

var (
	halNode     = globaltest.Container{ID: "hal0000000001", Cluster: "kind", Networks: []string{"hal-net"}}
	halNodeDual = globaltest.Container{ID: "hal0000000002", Cluster: "kind", Networks: []string{"kind", "hal-net"}}
	userDefault = globaltest.Container{ID: "usr0000000001", Cluster: "kind", Networks: []string{"kind"}}
	userNamed   = globaltest.Container{ID: "usr0000000002", Cluster: "dev", Networks: []string{"kind"}}
	// A user cluster whose name starts with hal- is still not HAL's: HAL's
	// cluster is only ever called "kind".
	userHalPrefixed = globaltest.Container{ID: "usr0000000003", Cluster: "hal-demo", Networks: []string{"hal-net"}}
)

func TestIsHALKindCluster(t *testing.T) {
	cases := []struct {
		name    string
		cluster global.KindCluster
		want    bool
	}{
		{"HAL cluster on hal-net", global.KindCluster{Name: "kind", Nodes: []global.KindNode{{ID: "a", Cluster: "kind", Networks: []string{"hal-net"}}}}, true},
		{"HAL cluster dual-homed", global.KindCluster{Name: "kind", Nodes: []global.KindNode{{ID: "a", Cluster: "kind", Networks: []string{"kind", "hal-net"}}}}, true},
		{"user default cluster", global.KindCluster{Name: "kind", Nodes: []global.KindNode{{ID: "a", Cluster: "kind", Networks: []string{"kind"}}}}, false},
		{"default name, no network", global.KindCluster{Name: "kind", Nodes: []global.KindNode{{ID: "a", Cluster: "kind"}}}, false},
		{"hal- prefix on hal-net", global.KindCluster{Name: "hal-demo", Nodes: []global.KindNode{{ID: "a", Cluster: "hal-demo", Networks: []string{"hal-net"}}}}, false},
		{"legacy hal-k8s name", global.KindCluster{Name: "hal-k8s", Nodes: []global.KindNode{{ID: "a", Cluster: "hal-k8s", Networks: []string{"hal-net"}}}}, false},
		{"no nodes", global.KindCluster{Name: "kind"}, false},
		{"empty", global.KindCluster{}, false},
	}
	for _, tc := range cases {
		if got := global.IsHALKindCluster(tc.cluster); got != tc.want {
			t.Errorf("%s: IsHALKindCluster = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestDeleteHALKindClustersSkipsForeignDefaultCluster(t *testing.T) {
	lab := globaltest.NewLab(t, []globaltest.Container{userDefault}, nil)

	res := global.DeleteHALKindClusters("docker")

	if len(res.Deleted) != 0 {
		t.Fatalf("deleted a cluster HAL did not create: %v", res.Deleted)
	}
	if !reflect.DeepEqual(res.Skipped, []string{"kind"}) {
		t.Fatalf("Skipped = %v, want [kind]", res.Skipped)
	}
	if got := lab.ContainerIDs(); !reflect.DeepEqual(got, []string{userDefault.ID}) {
		t.Fatalf("user's kind node was removed: containers left = %v", got)
	}
	assertNoDestructiveCall(t, lab)
}

func TestDeleteHALKindClustersDeletesOwnCluster(t *testing.T) {
	lab := globaltest.NewLab(t, []globaltest.Container{halNode}, nil)

	res := global.DeleteHALKindClusters("docker")

	if !reflect.DeepEqual(res.Deleted, []string{"kind"}) {
		t.Fatalf("Deleted = %v, want [kind]", res.Deleted)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", res.Warnings)
	}
	if got := lab.ContainerIDs(); len(got) != 0 {
		t.Fatalf("HAL's cluster survived: %v", got)
	}
	if !lab.Ran("kind delete cluster --name kind") {
		t.Fatalf("kind delete cluster --name kind not run; ran: %v", lab.Lines())
	}
}

func TestDeleteHALKindClustersMixedLabTouchesOnlyHALs(t *testing.T) {
	// Three clusters on one engine: HAL's, the user's default-named one on
	// kind's own network, and a user's named one. Only HAL's may go.
	lab := globaltest.NewLab(t, []globaltest.Container{userDefault, userNamed, userHalPrefixed}, nil)

	res := global.DeleteHALKindClusters("docker")

	if len(res.Deleted) != 0 {
		t.Fatalf("Deleted = %v, want none", res.Deleted)
	}
	if want := []string{"dev", "hal-demo", "kind"}; !reflect.DeepEqual(res.Skipped, want) {
		t.Fatalf("Skipped = %v, want %v", res.Skipped, want)
	}
	if got := lab.ContainerIDs(); len(got) != 3 {
		t.Fatalf("a foreign node was removed: left = %v", got)
	}
	assertNoDestructiveCall(t, lab)
}

func TestDeleteHALKindClustersKeepsForeignClusterWhenHALsIsRemoved(t *testing.T) {
	// A HAL cluster and a user cluster whose node shares the name prefix. The
	// engine fallback must remove HAL's node IDs only.
	user := globaltest.Container{ID: "usr0000000009", Cluster: "dev", Networks: []string{"kind"}}
	lab := globaltest.NewLab(t, []globaltest.Container{halNode, user}, nil)
	lab.KindCLIBroken = true

	res := global.DeleteHALKindClusters("docker")

	if !reflect.DeepEqual(res.Deleted, []string{"kind"}) || len(res.Warnings) != 0 {
		t.Fatalf("Deleted = %v, Warnings = %v", res.Deleted, res.Warnings)
	}
	if got := lab.ContainerIDs(); !reflect.DeepEqual(got, []string{user.ID}) {
		t.Fatalf("containers left = %v, want only the user's %s", got, user.ID)
	}
	if !lab.Ran("docker rm -f " + halNode.ID) {
		t.Fatalf("fallback did not remove HAL's node by ID; ran: %v", lab.Lines())
	}
	for _, line := range lab.Lines() {
		if strings.HasPrefix(line, "docker rm") && strings.Contains(line, user.ID) {
			t.Fatalf("fallback touched the user's node: %s", line)
		}
	}
}

func TestDeleteHALKindClustersFallsBackWhenKindCLIIsBroken(t *testing.T) {
	lab := globaltest.NewLab(t, []globaltest.Container{halNodeDual}, nil)
	lab.KindCLIBroken = true

	res := global.DeleteHALKindClusters("podman")

	if !reflect.DeepEqual(res.Deleted, []string{"kind"}) {
		t.Fatalf("Deleted = %v, want [kind]", res.Deleted)
	}
	if got := lab.ContainerIDs(); len(got) != 0 {
		t.Fatalf("HAL's node survived: %v", got)
	}
}

func TestDeleteHALKindClustersPinsKindProviderToTheEngine(t *testing.T) {
	lab := globaltest.NewLab(t, []globaltest.Container{halNode}, nil)

	global.DeleteHALKindClusters("podman")

	for _, c := range lab.Calls() {
		if c.Name == "kind" {
			if !reflect.DeepEqual(c.Env, []string{"KIND_EXPERIMENTAL_PROVIDER=podman"}) {
				t.Fatalf("kind env = %v", c.Env)
			}
			return
		}
	}
	t.Fatal("kind was never run")
}

func TestDeleteKindClustersRefusesForeignClusterHandedIn(t *testing.T) {
	lab := globaltest.NewLab(t, []globaltest.Container{userDefault}, nil)
	foreign := global.KindCluster{Name: "kind", Nodes: []global.KindNode{{ID: userDefault.ID, Cluster: "kind", Networks: []string{"kind"}}}}

	res := global.DeleteKindClusters("docker", []global.KindCluster{foreign})

	if len(res.Deleted) != 0 || !reflect.DeepEqual(res.Skipped, []string{"kind"}) {
		t.Fatalf("Deleted = %v, Skipped = %v", res.Deleted, res.Skipped)
	}
	assertNoDestructiveCall(t, lab)
}

func TestDeleteHALKindClustersKeepsKindNetworkWhileAnotherClusterUsesIt(t *testing.T) {
	lab := globaltest.NewLab(t, []globaltest.Container{halNodeDual, userNamed}, nil)

	global.DeleteHALKindClusters("docker")

	if lab.Ran("docker network rm kind") {
		t.Fatalf("removed the kind network while the user's cluster still uses it; ran: %v", lab.Lines())
	}
}

func TestDeleteHALKindClustersRemovesKindNetworkWhenUnused(t *testing.T) {
	lab := globaltest.NewLab(t, []globaltest.Container{halNodeDual}, nil)

	global.DeleteHALKindClusters("docker")

	if !lab.Ran("docker network rm kind") {
		t.Fatalf("left HAL's leftover kind network behind; ran: %v", lab.Lines())
	}
}

func TestRollbackNewKindClusterDeletesTheJustCreatedCluster(t *testing.T) {
	// Created moments ago and never attached to hal-net: it fails the ownership
	// test, so the rollback is the only way HAL can clean it up.
	lab := globaltest.NewLab(t, []globaltest.Container{userDefault}, nil)

	if err := global.RollbackNewKindCluster("docker"); err != nil {
		t.Fatal(err)
	}
	if got := lab.ContainerIDs(); len(got) != 0 {
		t.Fatalf("rollback left the cluster behind: %v", got)
	}
}

func TestHALKindClusterExistsIgnoresForeignClusters(t *testing.T) {
	globaltest.NewLab(t, []globaltest.Container{userDefault}, nil)
	if global.HALKindClusterExists("docker") {
		t.Fatal("a user's default kind cluster was reported as HAL's")
	}
}

func TestHALKindClusterExistsFalseWhenEngineFails(t *testing.T) {
	globaltest.Install(t, func(c globaltest.Call) ([]byte, error) { return globaltest.Errorf(c) })
	if global.HALKindClusterExists("docker") {
		t.Fatal("reported a HAL cluster although the engine could not be queried")
	}
}

func TestDeleteHALKindClustersDoesNothingWhenEngineFails(t *testing.T) {
	lab := globaltest.Install(t, func(c globaltest.Call) ([]byte, error) { return globaltest.Errorf(c) })

	res := global.DeleteHALKindClusters("docker")

	if len(res.Deleted) != 0 || len(res.Warnings) != 1 {
		t.Fatalf("Deleted = %v, Warnings = %v", res.Deleted, res.Warnings)
	}
	assertNoDestructiveCall(t, lab)
}

// assertNoDestructiveCall fails if the code under test removed or deleted
// anything on the fake host.
func assertNoDestructiveCall(t *testing.T, lab interface{ Lines() []string }) {
	t.Helper()
	for _, line := range lab.Lines() {
		if strings.HasPrefix(line, "kind delete") || strings.Contains(line, " rm ") || strings.Contains(line, " network rm") {
			t.Errorf("destructive call on a foreign cluster: %s", line)
		}
	}
}
