package global_test

import (
	"reflect"
	"strings"
	"testing"

	"hal/internal/global"
	"hal/internal/global/globaltest"
)

func TestDeleteMultipassVMUsesDeletePurgeForThatVMOnly(t *testing.T) {
	lab := globaltest.NewLab(t, nil, []string{"hal-nomad", "my-own-vm"})

	if err := global.DeleteMultipassVM("hal-nomad"); err != nil {
		t.Fatal(err)
	}

	if want := []string{"multipass delete --purge hal-nomad"}; !reflect.DeepEqual(lab.Lines(), want) {
		t.Fatalf("commands = %v, want %v", lab.Lines(), want)
	}
	if got := lab.VMs(); !reflect.DeepEqual(got, []string{"my-own-vm"}) {
		t.Fatalf("VMs left = %v, want only the user's", got)
	}
}

func TestDeleteMultipassVMRefusesNonHALName(t *testing.T) {
	lab := globaltest.NewLab(t, nil, []string{"my-own-vm"})

	if err := global.DeleteMultipassVM("my-own-vm"); err == nil {
		t.Fatal("deleted a VM that is not a HAL VM")
	}
	if len(lab.Lines()) != 0 {
		t.Fatalf("ran commands for a non-HAL VM: %v", lab.Lines())
	}
}

func TestMultipassDeleteCommandMatchesWhatRuns(t *testing.T) {
	// Dry-run messages print MultipassDeleteCommand; it must be the real call.
	lab := globaltest.NewLab(t, nil, []string{"hal-vault-os"})

	if err := global.DeleteMultipassVM("hal-vault-os"); err != nil {
		t.Fatal(err)
	}
	if got, want := lab.Lines()[0], global.MultipassDeleteCommand("hal-vault-os"); got != want {
		t.Fatalf("ran %q but dry-run would say %q", got, want)
	}
}

func TestListHALMultipassVMsOnlyReturnsHALVMs(t *testing.T) {
	globaltest.NewLab(t, nil, []string{"hal-nomad", "primary", "hal-boundary-ssh", "halo"})

	got, err := global.ListHALMultipassVMs()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"hal-nomad", "hal-boundary-ssh"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ListHALMultipassVMs = %v, want %v", got, want)
	}
}

func TestMultipassNeverRunsBarePurge(t *testing.T) {
	// The fake lab fails the test on any multipass command other than the
	// listing and `delete --purge <vm>`, so a bare `multipass purge` cannot hide.
	lab := globaltest.NewLab(t, nil, []string{"hal-nomad", "hal-vault-os"})

	vms, err := global.ListHALMultipassVMs()
	if err != nil {
		t.Fatal(err)
	}
	for _, vm := range vms {
		if err := global.DeleteMultipassVM(vm); err != nil {
			t.Fatal(err)
		}
	}
	for _, line := range lab.Lines() {
		if line == "multipass purge" || strings.HasPrefix(line, "multipass purge ") {
			t.Fatalf("bare purge ran: %s", line)
		}
	}
}
