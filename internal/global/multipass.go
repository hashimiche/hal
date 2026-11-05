package global

import (
	"fmt"
	"strings"
)

// multipassVMPrefix is the name prefix every HAL Multipass VM carries
// (hal-nomad, hal-boundary-ssh, hal-vault-os).
const multipassVMPrefix = "hal-"

// MultipassDeleteCommand returns the command DeleteMultipassVM runs for name.
// Dry-run messages print it so they cannot drift from the real call.
func MultipassDeleteCommand(name string) string {
	return "multipass delete --purge " + name
}

// DeleteMultipassVM deletes one HAL Multipass VM and purges only that VM.
//
// It never runs a bare `multipass purge`: that command permanently removes
// every instance in the "Deleted" state, including VMs the user deleted
// themselves and may still want to `multipass recover`. `multipass delete
// --purge` scopes the purge to the instance being deleted.
//
// The name must be a HAL VM name (hal-*). That is a guard rail rather than the
// ownership test (callers pass names from HAL's own constants or from the hal-*
// listing): it stops a future caller handing this helper an arbitrary instance.
func DeleteMultipassVM(name string) error {
	if !strings.HasPrefix(name, multipassVMPrefix) {
		return fmt.Errorf("refusing to delete Multipass VM %q: not a HAL VM (expected a %q name)", name, multipassVMPrefix+"*")
	}
	_, err := runner(nil, "multipass", "delete", "--purge", name)
	return err
}

// ListHALMultipassVMs returns the Multipass VMs whose name starts with hal-,
// including ones already in the "Deleted" state.
func ListHALMultipassVMs() ([]string, error) {
	out, err := runner(nil, "multipass", "list", "--format", "csv")
	if err != nil {
		return nil, err
	}
	return parseHALMultipassVMs(string(out)), nil
}

func parseHALMultipassVMs(csv string) []string {
	vms := []string{}
	for i, line := range strings.Split(strings.TrimSpace(csv), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if i == 0 && strings.HasPrefix(strings.ToLower(line), "name,") {
			continue
		}
		name := strings.TrimSpace(strings.Split(line, ",")[0])
		if strings.HasPrefix(name, multipassVMPrefix) {
			vms = append(vms, name)
		}
	}
	return vms
}
