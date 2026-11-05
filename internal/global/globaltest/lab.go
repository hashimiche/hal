package globaltest

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// Container is a fake container: a kind node when Cluster is set, any other
// container otherwise.
type Container struct {
	ID       string
	Name     string   // optional; `inspect` and `network connect` also match on it
	Image    string   // optional; listed by `ps --format "{{.ID}} {{.Names}} {{.Image}}"`
	Cluster  string   // io.x-k8s.kind.cluster label value
	Networks []string // engine networks it is attached to
}

// Lab is a fake host: a container engine holding kind nodes, a kind CLI and
// Multipass. It is stateful, so a delete really removes what it targets and a
// test can assert on what is left.
type Lab struct {
	*Fake

	mu         sync.Mutex
	containers []Container
	vms        []string

	// KindCLIBroken makes `kind delete cluster` fail, as when the kind CLI is
	// missing or too old for the engine.
	KindCLIBroken bool

	// KindIgnoresNetworkEnv makes `kind create cluster` ignore the
	// KIND_EXPERIMENTAL_*_NETWORK variables, as newer kind releases may: the new
	// node lands on kind's own network instead of hal-net.
	KindIgnoresNetworkEnv bool

	// RefuseNetworkConnect makes `network connect` fail.
	RefuseNetworkConnect bool
}

func envHas(env []string, want string) bool {
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}

// NewLab installs a fake host holding the given kind nodes and Multipass VMs.
// Any Multipass command other than `list --format csv` and
// `delete --purge <vm>` fails the test: that is the invariant under test.
func NewLab(t testing.TB, containers []Container, vms []string) *Lab {
	t.Helper()
	l := &Lab{containers: append([]Container(nil), containers...), vms: append([]string(nil), vms...)}
	l.Fake = Install(t, func(c Call) ([]byte, error) { return l.handle(t, c) })
	return l
}

// ContainerIDs returns the IDs of the containers that still exist.
func (l *Lab) ContainerIDs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var ids []string
	for _, c := range l.containers {
		ids = append(ids, c.ID)
	}
	return ids
}

// VMs returns the Multipass VMs that still exist.
func (l *Lab) VMs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.vms...)
}

func (l *Lab) handle(t testing.TB, c Call) ([]byte, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	switch c.Name {
	case "docker", "podman":
		return l.engine(c)
	case "kind":
		return l.kind(c)
	case "multipass":
		return l.multipass(t, c)
	}
	return Errorf(c)
}

func (l *Lab) engine(c Call) ([]byte, error) {
	if len(c.Args) == 0 {
		return Errorf(c)
	}
	switch c.Args[0] {
	case "info":
		return nil, nil
	case "ps":
		var out strings.Builder
		if strings.Contains(strings.Join(c.Args, " "), "--filter") { // kind nodes, by label
			for _, ct := range l.containers {
				if ct.Cluster != "" {
					fmt.Fprintln(&out, ct.ID)
				}
			}
			return []byte(out.String()), nil
		}
		for _, ct := range l.containers { // every container: "<id> <name> <image>"
			name, image := ct.Name, ct.Image
			if name == "" {
				name = ct.ID
			}
			if image == "" {
				image = "fake/image"
			}
			fmt.Fprintf(&out, "%s %s %s\n", ct.ID, name, image)
		}
		return []byte(out.String()), nil
	case "inspect":
		var out strings.Builder
		for _, ref := range c.Args[3:] { // inspect --format <fmt> <ids or names...>
			ct := l.find(ref)
			if ct == nil {
				return nil, fmt.Errorf("no such container: %s", ref)
			}
			if strings.Contains(c.Args[2], ".Config.Labels") {
				fmt.Fprintf(&out, "%s|%s|%s \n", ct.ID, ct.Cluster, strings.Join(ct.Networks, " "))
			} else if strings.HasPrefix(c.Args[2], "{{.Id}}|{{range") { // "<id>|<network> <network> ..."
				fmt.Fprintf(&out, "%s|%s \n", ct.ID, strings.Join(ct.Networks, " "))
			} else if hasNetwork(ct.Networks, "hal-net") { // the hal-net IP lookup
				fmt.Fprintln(&out, "172.30.0.2")
			}
		}
		return []byte(out.String()), nil
	case "rm":
		l.remove(c.Args[2:]) // rm -f <ids...>
		return nil, nil
	case "network":
		if len(c.Args) == 4 && c.Args[1] == "connect" {
			if l.RefuseNetworkConnect {
				return nil, fmt.Errorf("network connect refused")
			}
			ct := l.find(c.Args[3])
			if ct == nil {
				return nil, fmt.Errorf("no such container: %s", c.Args[3])
			}
			if !hasNetwork(ct.Networks, c.Args[2]) {
				ct.Networks = append(ct.Networks, c.Args[2])
			}
		}
		return nil, nil
	}
	return Errorf(c)
}

func (l *Lab) find(ref string) *Container {
	for i := range l.containers {
		if l.containers[i].ID == ref || (l.containers[i].Name != "" && l.containers[i].Name == ref) {
			return &l.containers[i]
		}
	}
	return nil
}

func hasNetwork(networks []string, name string) bool {
	for _, n := range networks {
		if n == name {
			return true
		}
	}
	return false
}

func (l *Lab) kind(c Call) ([]byte, error) {
	if len(c.Args) >= 4 && c.Args[0] == "create" && c.Args[1] == "cluster" && c.Args[2] == "--name" {
		// Like kind, the new node joins the network named by the env, else kind's own.
		networks := []string{"kind"}
		if !l.KindIgnoresNetworkEnv && envHas(c.Env, "KIND_EXPERIMENTAL_DOCKER_NETWORK=hal-net") {
			networks = []string{"hal-net"}
		}
		l.containers = append(l.containers, Container{ID: "new" + c.Args[3], Name: c.Args[3] + "-control-plane", Cluster: c.Args[3], Networks: networks})
		return nil, nil
	}
	if len(c.Args) >= 2 && c.Args[0] == "get" && c.Args[1] == "nodes" {
		return nil, fmt.Errorf("kind get nodes unavailable") // callers fall back to <cluster>-control-plane
	}
	if len(c.Args) >= 4 && c.Args[0] == "delete" && c.Args[1] == "cluster" && c.Args[2] == "--name" {
		if l.KindCLIBroken {
			return nil, fmt.Errorf("kind: command failed")
		}
		var keep []Container
		for _, ct := range l.containers {
			if ct.Cluster != c.Args[3] {
				keep = append(keep, ct)
			}
		}
		l.containers = keep
		return nil, nil
	}
	return Errorf(c)
}

func (l *Lab) multipass(t testing.TB, c Call) ([]byte, error) {
	switch {
	case len(c.Args) == 3 && c.Args[0] == "list" && c.Args[1] == "--format" && c.Args[2] == "csv":
		out := "Name,State,IPv4,Image\n"
		for _, vm := range l.vms {
			out += vm + ",Running,10.0.0.2,Ubuntu\n"
		}
		return []byte(out), nil
	case len(c.Args) == 3 && c.Args[0] == "delete" && c.Args[1] == "--purge":
		var keep []string
		for _, vm := range l.vms {
			if vm != c.Args[2] {
				keep = append(keep, vm)
			}
		}
		l.vms = keep
		return nil, nil
	}
	t.Errorf("forbidden multipass call: %s", c)
	return Errorf(c)
}

func (l *Lab) remove(ids []string) {
	gone := map[string]bool{}
	for _, id := range ids {
		gone[id] = true
	}
	var keep []Container
	for _, ct := range l.containers {
		if !gone[ct.ID] {
			keep = append(keep, ct)
		}
	}
	l.containers = keep
}
