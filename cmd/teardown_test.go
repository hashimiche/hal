package cmd

import (
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"hal/internal/global"
	"hal/internal/global/globaltest"
)

// captureStdout runs fn and returns what it printed.
func captureStdout(t *testing.T, fn func()) string {
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

func TestTeardownKindClustersSkipsForeignCluster(t *testing.T) {
	user := globaltest.Container{ID: "usr0000000001", Cluster: "kind", Networks: []string{"kind"}}
	lab := globaltest.NewLab(t, []globaltest.Container{user}, nil)

	deleted, skipped, warnings := teardownKindClusters([]string{"docker"})

	if deleted != 0 {
		t.Fatalf("deleted = %d, want 0: HAL deleted a cluster it did not create", deleted)
	}
	if !reflect.DeepEqual(skipped, []string{"kind (docker)"}) {
		t.Fatalf("skipped = %v", skipped)
	}
	if len(warnings) != 0 {
		t.Fatalf("a skipped foreign cluster is not a warning: %v", warnings)
	}
	if got := lab.ContainerIDs(); !reflect.DeepEqual(got, []string{user.ID}) {
		t.Fatalf("user's cluster was touched: %v", got)
	}
}

func TestTeardownKindClustersDeletesHALClusterAndKeepsTheUsers(t *testing.T) {
	halNode := globaltest.Container{ID: "hal0000000001", Cluster: "kind", Networks: []string{"hal-net"}}
	user := globaltest.Container{ID: "usr0000000002", Cluster: "dev", Networks: []string{"kind"}}
	lab := globaltest.NewLab(t, []globaltest.Container{halNode, user}, nil)

	deleted, skipped, warnings := teardownKindClusters([]string{"docker"})

	if deleted != 1 || len(warnings) != 0 {
		t.Fatalf("deleted = %d, warnings = %v", deleted, warnings)
	}
	if !reflect.DeepEqual(skipped, []string{"dev (docker)"}) {
		t.Fatalf("skipped = %v", skipped)
	}
	if got := lab.ContainerIDs(); !reflect.DeepEqual(got, []string{user.ID}) {
		t.Fatalf("containers left = %v, want only the user's", got)
	}
}

func TestTeardownMultipassVMsOnlyDeletePurgesHALVMs(t *testing.T) {
	lab := globaltest.NewLab(t, nil, []string{"hal-nomad", "hal-boundary-ssh", "my-own-vm"})

	deleted, warnings := teardownMultipassVMs()

	if deleted != 2 || len(warnings) != 0 {
		t.Fatalf("deleted = %d, warnings = %v", deleted, warnings)
	}
	if got := lab.VMs(); !reflect.DeepEqual(got, []string{"my-own-vm"}) {
		t.Fatalf("VMs left = %v, want only the user's", got)
	}
	want := []string{
		"multipass list --format csv",
		"multipass delete --purge hal-nomad",
		"multipass delete --purge hal-boundary-ssh",
	}
	if !reflect.DeepEqual(lab.Lines(), want) {
		t.Fatalf("multipass commands = %v, want %v", lab.Lines(), want)
	}
}

func TestTeardownMultipassVMsReportsFailureWithTheRealCommand(t *testing.T) {
	globaltest.Install(t, func(c globaltest.Call) ([]byte, error) {
		if c.String() == "multipass list --format csv" {
			return []byte("Name,State\nhal-nomad,Running\n"), nil
		}
		return nil, io.ErrUnexpectedEOF
	})

	deleted, warnings := teardownMultipassVMs()

	if deleted != 0 || len(warnings) != 1 || !strings.Contains(warnings[0], "multipass delete --purge hal-nomad") {
		t.Fatalf("deleted = %d, warnings = %v", deleted, warnings)
	}
}

func TestDeleteDryRunDescribesTheScopedTeardown(t *testing.T) {
	old := global.DryRun
	global.DryRun = true
	t.Cleanup(func() { global.DryRun = old })

	out := captureStdout(t, func() { destroyCmd.Run(destroyCmd, nil) })

	for _, want := range []string{"multipass delete --purge <vm>", "attached to hal-net", "other KinD clusters are left alone"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "multipass purge") {
		t.Errorf("dry-run mentions a global purge:\n%s", out)
	}
}
