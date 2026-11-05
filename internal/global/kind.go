package global

import (
	"fmt"
	"sort"
	"strings"
)

const (
	// KindClusterLabel is the label kind puts on every node container. It names
	// the cluster the node belongs to and proves nothing about who created that
	// cluster: a user's own `kind create cluster` sets it too.
	KindClusterLabel = "io.x-k8s.kind.cluster"

	// KindClusterName is the only name HAL gives its cluster. It is also kind's
	// default, so it is the name of every user's plain `kind create cluster`.
	// HAL passes it to `kind create cluster --name` explicitly so a stray
	// KIND_CLUSTER_NAME in the environment cannot rename the cluster.
	KindClusterName = "kind"

	// KindControlPlaneNode is the node container kind names for cluster "kind".
	KindControlPlaneNode = "kind-control-plane"

	// kindDefaultNetwork is the engine network kind attaches nodes to when it
	// does not honor KIND_EXPERIMENTAL_*_NETWORK.
	kindDefaultNetwork = "kind"
)

// KindNode is one kind node container as the container engine reports it.
type KindNode struct {
	ID       string
	Cluster  string   // value of the io.x-k8s.kind.cluster label
	Networks []string // engine networks the container is attached to
}

func (n KindNode) onNetwork(name string) bool {
	for _, network := range n.Networks {
		if network == name {
			return true
		}
	}
	return false
}

// KindCluster groups the node containers of one kind cluster.
type KindCluster struct {
	Name  string
	Nodes []KindNode
}

func (c KindCluster) nodeIDs() []string {
	ids := make([]string, 0, len(c.Nodes))
	for _, n := range c.Nodes {
		ids = append(ids, n.ID)
	}
	return ids
}

func (c KindCluster) usedKindNetwork() bool {
	for _, n := range c.Nodes {
		if n.onNetwork(kindDefaultNetwork) {
			return true
		}
	}
	return false
}

// IsHALKindCluster is the ownership test for a kind cluster: HAL created it if
// and only if it carries HAL's cluster name AND at least one of its nodes is
// attached to hal-net.
//
// The kind label and the cluster name alone cannot tell HAL's cluster from a
// user's default `kind` cluster. hal-net can: ensureHALKindCluster creates the
// cluster on hal-net (KIND_EXPERIMENTAL_*_NETWORK) and then always connects
// every node to it, and a plain `kind create cluster` does neither.
//
// The test errs towards leaving a cluster alone. A HAL cluster that someone
// detached from hal-net is no longer recognized and has to be removed with
// `kind delete cluster` by hand.
func IsHALKindCluster(c KindCluster) bool {
	if c.Name != KindClusterName {
		return false
	}
	for _, n := range c.Nodes {
		if n.onNetwork(HalNetName) {
			return true
		}
	}
	return false
}

// kindNodeInspectFormat prints "<id>|<cluster>|<network> <network> ..." per
// container. `inspect` is used instead of `ps --format` because Podman 6 stores
// ps Labels as a slice, which breaks `{{index .Labels ...}}` (it is the error
// older kind CLIs hit in `kind get clusters`); the inspect Config.Labels map
// works on Docker and Podman alike. Networks are listed for stopped containers
// too, so a stopped HAL cluster is still recognized.
const kindNodeInspectFormat = `{{.Id}}|{{index .Config.Labels "` + KindClusterLabel + `"}}|{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{end}}`

// InspectKindNodes lists every kind node container on engine, running or not,
// whoever created it.
func InspectKindNodes(engine string) ([]KindNode, error) {
	out, err := runner(nil, engine, "ps", "-a", "--filter", "label="+KindClusterLabel, "--format", "{{.ID}}")
	if err != nil {
		return nil, fmt.Errorf("%s ps: %w", engine, err)
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return nil, nil
	}

	args := append([]string{"inspect", "--format", kindNodeInspectFormat}, ids...)
	out, err = runner(nil, engine, args...)
	nodes := parseKindNodes(string(out))
	if err != nil && len(nodes) == 0 {
		return nil, fmt.Errorf("%s inspect: %w", engine, err)
	}
	// With some output, the error is a container that vanished between ps and
	// inspect (a cluster being deleted); the survivors are still accurate.
	return nodes, nil
}

func parseKindNodes(out string) []KindNode {
	var nodes []KindNode
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "|", 3)
		if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
			continue
		}
		nodes = append(nodes, KindNode{ID: parts[0], Cluster: parts[1], Networks: strings.Fields(parts[2])})
	}
	return nodes
}

// GroupKindClusters groups nodes by cluster name and splits the clusters into
// the ones HAL created and the ones it did not, each sorted by name.
func GroupKindClusters(nodes []KindNode) (hal, foreign []KindCluster) {
	byName := map[string]*KindCluster{}
	var names []string
	for _, n := range nodes {
		c, ok := byName[n.Cluster]
		if !ok {
			c = &KindCluster{Name: n.Cluster}
			byName[n.Cluster] = c
			names = append(names, n.Cluster)
		}
		c.Nodes = append(c.Nodes, n)
	}
	sort.Strings(names)
	for _, name := range names {
		if IsHALKindCluster(*byName[name]) {
			hal = append(hal, *byName[name])
		} else {
			foreign = append(foreign, *byName[name])
		}
	}
	return hal, foreign
}

// DiscoverKindClusters returns the kind clusters on engine, split by ownership.
func DiscoverKindClusters(engine string) (hal, foreign []KindCluster, err error) {
	nodes, err := InspectKindNodes(engine)
	if err != nil {
		return nil, nil, err
	}
	hal, foreign = GroupKindClusters(nodes)
	return hal, foreign, nil
}

// HALKindClusterExists reports whether engine hosts a kind cluster HAL created.
// It is false when the engine cannot be queried, so a caller that is about to
// delete something never acts on a guess.
func HALKindClusterExists(engine string) bool {
	hal, _, err := DiscoverKindClusters(engine)
	return err == nil && len(hal) > 0
}

// KindDeleteResult is the outcome of deleting kind clusters.
type KindDeleteResult struct {
	Deleted  []string // HAL clusters that were removed
	Skipped  []string // clusters left alone because HAL did not create them
	Warnings []string
}

// DeleteHALKindClusters deletes the kind clusters HAL created on engine and
// nothing else. Every other cluster is reported in Skipped and never touched.
func DeleteHALKindClusters(engine string) KindDeleteResult {
	hal, foreign, err := DiscoverKindClusters(engine)
	if err != nil {
		return KindDeleteResult{Warnings: []string{fmt.Sprintf("kind discovery via %s failed: %v", engine, err)}}
	}
	res := DeleteKindClusters(engine, hal)
	for _, c := range foreign {
		res.Skipped = append(res.Skipped, c.Name)
	}
	return res
}

// DeleteKindClusters deletes the given clusters, and only those that pass
// IsHALKindCluster: a foreign cluster handed in by mistake lands in Skipped.
// This is the one place HAL deletes a kind cluster.
func DeleteKindClusters(engine string, clusters []KindCluster) KindDeleteResult {
	var res KindDeleteResult
	usedKindNetwork := false
	for _, c := range clusters {
		if !IsHALKindCluster(c) {
			res.Skipped = append(res.Skipped, c.Name)
			continue
		}
		if err := deleteKindCluster(engine, c); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("kind cluster %q: %v", c.Name, err))
			continue
		}
		res.Deleted = append(res.Deleted, c.Name)
		usedKindNetwork = usedKindNetwork || c.usedKindNetwork()
	}
	if usedKindNetwork {
		removeKindNetworkIfUnused(engine)
	}
	return res
}

func deleteKindCluster(engine string, c KindCluster) error {
	_, cliErr := runner(kindProviderEnv(engine), "kind", "delete", "cluster", "--name", c.Name)

	left, verifyErr := survivingNodeIDs(engine, c)
	switch {
	case verifyErr != nil && cliErr == nil:
		return nil // kind reported success; the re-check is only a second opinion
	case verifyErr != nil:
		return fmt.Errorf("kind delete failed (%v) and the result could not be verified (%v)", cliErr, verifyErr)
	case len(left) == 0:
		return nil
	}

	// The kind CLI is missing, too old for this engine (Podman 6 breaks older
	// kind CLIs), or left nodes behind. Remove exactly the nodes of this
	// cluster; their IDs came from the ownership check.
	if _, err := runner(nil, engine, append([]string{"rm", "-f"}, left...)...); err != nil {
		if cliErr != nil {
			return fmt.Errorf("kind delete failed (%v) and removing its nodes via %s failed too (%v)", cliErr, engine, err)
		}
		return fmt.Errorf("removing leftover nodes via %s failed: %v", engine, err)
	}
	return nil
}

// survivingNodeIDs returns the IDs of c's nodes that still exist on engine.
func survivingNodeIDs(engine string, c KindCluster) ([]string, error) {
	nodes, err := InspectKindNodes(engine)
	if err != nil {
		return nil, err
	}
	mine := map[string]bool{}
	for _, id := range c.nodeIDs() {
		mine[id] = true
	}
	var left []string
	for _, n := range nodes {
		if mine[n.ID] {
			left = append(left, n.ID)
		}
	}
	return left, nil
}

// RollbackNewKindCluster removes the "kind" cluster the caller created moments
// ago and could not attach to hal-net.
//
// A cluster that is not on hal-net does not pass IsHALKindCluster, so without
// this it would be stranded: HAL would refuse to reuse it (it looks foreign) and
// refuse to delete it. The caller must have checked that no cluster named
// KindClusterName existed before it ran `kind create cluster`; this is the only
// path that deletes a kind cluster without the hal-net ownership test.
func RollbackNewKindCluster(engine string) error {
	nodes, err := InspectKindNodes(engine)
	if err != nil {
		return err
	}
	c := KindCluster{Name: KindClusterName}
	for _, n := range nodes {
		if n.Cluster == KindClusterName {
			c.Nodes = append(c.Nodes, n)
		}
	}
	if len(c.Nodes) == 0 {
		return nil
	}
	return deleteKindCluster(engine, c)
}

// removeKindNetworkIfUnused removes kind's own network, which a HAL node lands
// on when the kind CLI ignores KIND_EXPERIMENTAL_*_NETWORK. It stays whenever
// any kind node, even a stopped one of another cluster, still references it:
// removing it would leave that cluster unable to start.
func removeKindNetworkIfUnused(engine string) {
	nodes, err := InspectKindNodes(engine)
	if err != nil {
		return
	}
	for _, n := range nodes {
		if n.onNetwork(kindDefaultNetwork) {
			return
		}
	}
	_, _ = runner(nil, engine, "network", "rm", kindDefaultNetwork)
}

// kindProviderEnv pins the kind provider to the engine being cleaned, so a
// KIND_EXPERIMENTAL_PROVIDER set in the user's shell cannot redirect the delete
// to another engine.
func kindProviderEnv(engine string) []string {
	if strings.Contains(engine, "podman") {
		return []string{"KIND_EXPERIMENTAL_PROVIDER=podman"}
	}
	return []string{"KIND_EXPERIMENTAL_PROVIDER=docker"}
}
