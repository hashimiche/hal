package global

import (
	"fmt"
	"os/exec"
	"strings"
)

// Notice the capital letters! In Go, variables must start with a capital letter
// to be "exported" and visible to other packages.
var (
	Debug  bool
	DryRun bool
)

func DetectEngine() (string, error) {
	if err := exec.Command("docker", "info").Run(); err == nil {
		return "docker", nil
	}
	if err := exec.Command("podman", "info").Run(); err == nil {
		return "podman", nil
	}
	return "", fmt.Errorf("no container engine found (make sure Docker or Podman is running)")
}

// EnsureNetwork creates the global grid if it doesn't exist.
func EnsureNetwork(engine string) {
	out, _ := exec.Command(engine, "network", "ls", "--format", "{{.Name}}").Output()
	if !strings.Contains(string(out), "hal-net") {
		if Debug {
			fmt.Println("[DEBUG] Creating global 'hal-net' Docker network...")
		}
		_ = exec.Command(engine, "network", "create", "hal-net").Run()
	}
}

// CleanNetworkIfEmpty acts as a garbage collector.
// Docker natively blocks deletion if containers are still attached!
func CleanNetworkIfEmpty(engine string) {
	if Debug {
		fmt.Println("[DEBUG] Attempting to clean up 'hal-net'...")
	}

	// We run the remove command. If it succeeds, the network was empty.
	// If it fails, Docker blocked it because an app is still using it.
	err := exec.Command(engine, "network", "rm", "hal-net").Run()

	if Debug {
		if err == nil {
			fmt.Println("[DEBUG] 'hal-net' was empty and has been removed.")
		} else {
			fmt.Println("[DEBUG] 'hal-net' is still in use by other containers. Leaving it active.")
		}
	}
}

// IsConsulRunning checks if the global hal-consul container is active.
func IsConsulRunning(engine string) bool {
	if Debug {
		fmt.Println("[DEBUG] Checking if global Consul control plane is active...")
	}
	out, _ := exec.Command(engine, "ps", "-q", "-f", "name=hal-consul$").Output()
	return strings.TrimSpace(string(out)) != ""
}

func IsContainerRunning(engine string, container string) bool {
	out, err := exec.Command(engine, "inspect", "-f", "{{.State.Running}}", container).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

func MultipassInstanceExists(name string) bool {
	err := exec.Command("multipass", "info", name).Run()
	return err == nil
}
