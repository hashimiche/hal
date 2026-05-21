package global

import (
	"encoding/json"
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

const (
	HalHealthContainerName = "hal-health"
	HalHealthPort          = 9001

	HalNetName = "hal-net"
)

// HalNetSubnet is the optional subnet passed via --network-subnet.
// Empty string means let the engine pick (default behaviour).
var HalNetSubnet string

func DetectEngine() (string, error) {
	if err := exec.Command("docker", "info").Run(); err == nil {
		return "docker", nil
	}
	if err := exec.Command("podman", "info").Run(); err == nil {
		return "podman", nil
	}
	return "", fmt.Errorf("no container engine found (make sure Docker or Podman is running)")
}

// CheckContainer reports whether a container is currently running.
func CheckContainer(engine, name string) bool {
	out, err := exec.Command(engine, "ps", "-q", "-f", fmt.Sprintf("name=^%s$", name)).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

// CheckMultipass reports whether a Multipass VM is running.
func CheckMultipass(name string) bool {
	out, err := exec.Command("multipass", "info", name, "--format", "csv").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Running")
}

// BoolState converts a boolean to the "enabled"/"disabled" string used in status snapshots.
func BoolState(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

// EnsureNetwork creates the global hal-net if it doesn't exist.
// When HalNetSubnet is set (via --network-subnet) the network is created with
// that explicit subnet so static IPs derived by HalNetStaticIP are predictable.
func EnsureNetwork(engine string) {
	out, _ := exec.Command(engine, "network", "ls", "--format", "{{.Name}}").Output()
	if !strings.Contains(string(out), HalNetName) {
		args := []string{"network", "create"}
		if HalNetSubnet != "" {
			args = append(args, "--subnet", HalNetSubnet)
		}
		args = append(args, HalNetName)
		if Debug {
			fmt.Printf("[DEBUG] Creating '%s' Docker network (subnet: %q)...\n", HalNetName, HalNetSubnet)
		}
		_ = exec.Command(engine, args...).Run()
	}
}

// HalNetStaticIP returns an IP in the hal-net subnet with the given last octet.
// It inspects the live network so it works regardless of which subnet the engine
// assigned (e.g. 172.18.0.250, 10.89.3.250, ...).
// Parses raw JSON to handle both Docker (IPAM.Config[].Subnet) and Podman
// (subnets[].subnet) inspect formats. Falls back to 172.18.0.<hostNum>.
func HalNetStaticIP(engine string, hostNum int) string {
	out, err := exec.Command(engine, "network", "inspect", HalNetName).Output()
	if err == nil {
		subnet := extractSubnetFromInspect(out)
		if subnet != "" {
			// subnet looks like "172.18.0.0/16" or "10.89.3.0/24"
			// Strip the mask and replace the last octet.
			if slash := strings.Index(subnet, "/"); slash > 0 {
				host := subnet[:slash]
				if dot := strings.LastIndex(host, "."); dot > 0 {
					return fmt.Sprintf("%s.%d", host[:dot], hostNum)
				}
			}
		}
	}
	return fmt.Sprintf("172.18.0.%d", hostNum)
}

// extractSubnetFromInspect parses the raw JSON output of "docker/podman network inspect"
// and returns the first subnet CIDR string. Handles both:
//   - Docker: [{"IPAM":{"Config":[{"Subnet":"172.18.0.0/16"}]}}]
//   - Podman: [{"subnets":[{"subnet":"10.89.3.0/24"}]}]
func extractSubnetFromInspect(data []byte) string {
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || len(raw) == 0 {
		return ""
	}
	net := raw[0]

	// Docker format: IPAM.Config[].Subnet
	if ipamRaw, ok := net["IPAM"]; ok {
		var ipam struct {
			Config []struct {
				Subnet string `json:"Subnet"`
			} `json:"Config"`
		}
		if err := json.Unmarshal(ipamRaw, &ipam); err == nil {
			for _, c := range ipam.Config {
				if c.Subnet != "" {
					return c.Subnet
				}
			}
		}
	}

	// Podman format: subnets[].subnet
	if subnetsRaw, ok := net["subnets"]; ok {
		var subnets []struct {
			Subnet string `json:"subnet"`
		}
		if err := json.Unmarshal(subnetsRaw, &subnets); err == nil {
			for _, s := range subnets {
				if s.Subnet != "" {
					return s.Subnet
				}
			}
		}
	}

	return ""
}

// CleanNetworkIfEmpty acts as a garbage collector.
// Docker natively blocks deletion if containers are still attached.
// Returns (existed, removed bool, blockers []string) so callers can distinguish
// "not deployed" from "cleaned" from "blocked by containers".
func CleanNetworkIfEmpty(engine string) (existed, removed bool, blockers []string) {
	if Debug {
		fmt.Println("[DEBUG] Attempting to clean up 'hal-net'...")
	}

	// If the network doesn't exist there's nothing to do.
	out, _ := exec.Command(engine, "network", "ls", "--format", "{{.Name}}").Output()
	if !strings.Contains(string(out), HalNetName) {
		if Debug {
			fmt.Println("[DEBUG] 'hal-net' does not exist, nothing to remove.")
		}
		return false, false, nil
	}

	err := exec.Command(engine, "network", "rm", HalNetName).Run()
	if err == nil {
		if Debug {
			fmt.Println("[DEBUG] 'hal-net' was empty and has been removed.")
		}
		return true, true, nil
	}

	// Network still in use — find which containers are blocking.
	inspectOut, inspectErr := exec.Command(engine, "network", "inspect", HalNetName,
		"--format", "{{range $k, $v := .Containers}}{{$v.Name}}\n{{end}}").Output()
	if inspectErr == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(inspectOut)), "\n") {
			if name := strings.TrimSpace(line); name != "" {
				blockers = append(blockers, name)
			}
		}
	}
	if Debug {
		fmt.Printf("[DEBUG] 'hal-net' is still in use by: %v\n", blockers)
	}
	return true, false, blockers
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
