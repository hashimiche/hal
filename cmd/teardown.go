package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"hal/cmd/mcp"
	"hal/internal/global"
)

// halNamedVolumes is the exhaustive list of named Docker/Podman volumes created
// by HAL product commands. These are NOT removed by "docker rm -f <container>" —
// they must be deleted explicitly. hal delete calls this list so a full teardown
// leaves no leftover state that would cause stale-data problems on the next
// "hal vault create / hal vault oidc enable --scim" cycle.
var halNamedVolumes = []string{
	"hal-authentik-db",   // Authentik PostgreSQL data (cmd/vault/oidc + integrations/authentik.go)
	"hal-vault-logs",     // Vault audit logs shared with Promtail (cmd/vault/create.go)
	"hal-vault-plugins",  // Vault external plugins, e.g. OS secrets engine (cmd/vault/create.go)
	"hal-vault-data",     // Vault file storage /vault/file VOLUME directive (cmd/vault/create.go)
	"hal-tfe-db-data",    // TFE PostgreSQL data (cmd/terraform/create.go)
	"hal-tfe-redis-data", // TFE Redis cache (cmd/terraform/create.go)
	"hal-tfe-minio-data", // TFE MinIO object storage (cmd/terraform/create.go)
}

const tfeCLIHelperImage = "hal-tfe-cli:latest"

// CleanStatus represents the outcome of a cleanup step.
type CleanStatus string

const (
	StatusCleaned     CleanStatus = "cleaned"
	StatusNotDeployed CleanStatus = "not deployed"
	StatusFailed      CleanStatus = "clean failed"
)

type globalTeardownResult struct {
	DockerContainersRemoved int
	KindClustersDeleted     int
	MultipassVMsDeleted     int
	ObsStatus               CleanStatus
	MCPStatus               CleanStatus
	NetworkStatus           CleanStatus
	NetworkBlockers         []string
	Warnings                []string
}

func runGlobalTeardown() globalTeardownResult {
	result := globalTeardownResult{}
	containerEngines := detectContainerEngines()
	if len(containerEngines) == 0 {
		containerEngines = []string{detectContainerEngine()}
	}

	kindClusters, err := listKindClusters()
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("kind discovery failed: %v", err))
	} else {
		for _, cluster := range kindClusters {
			if !isHALKindCluster(cluster) {
				continue
			}
			if err := kindDeleteCluster(cluster); err != nil {
				if isKindPodmanLabelTemplateError(err) {
					continue
				}
				result.Warnings = append(result.Warnings, fmt.Sprintf("kind delete cluster %q failed: %v", cluster, err))
			} else {
				result.KindClustersDeleted++
			}
		}
	}

	// KinD node containers are named kind-control-plane (not hal-*), so they
	// survive the HAL container sweep if the kind CLI is missing or delete fails.
	// Always sweep by cluster label and well-known names after the CLI attempt.
	kindNodesRemoved := 0
	for _, containerEngine := range containerEngines {
		removed, warnings := removeLeftoverKindNodes(containerEngine)
		kindNodesRemoved += removed
		result.DockerContainersRemoved += removed
		result.Warnings = append(result.Warnings, warnings...)
		_ = exec.Command(containerEngine, "network", "rm", "kind").Run()
	}
	if result.KindClustersDeleted == 0 && kindNodesRemoved > 0 {
		result.KindClustersDeleted = 1
	}

	for _, containerEngine := range containerEngines {
		dockerIDs, err := listHALContainerIDs(containerEngine)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s discovery failed: %v", containerEngine, err))
		} else if len(dockerIDs) > 0 {
			args := append([]string{"rm", "-f"}, dockerIDs...)
			if err := exec.Command(containerEngine, args...).Run(); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s container removal failed: %v", containerEngine, err))
			} else {
				result.DockerContainersRemoved += len(dockerIDs)
			}
		}

		tfeAgentIDs, err := global.ListTFEAgentContainerIDs(containerEngine)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s TFE agent discovery failed: %v", containerEngine, err))
		} else if len(tfeAgentIDs) > 0 {
			args := append([]string{"rm", "-f"}, tfeAgentIDs...)
			if err := exec.Command(containerEngine, args...).Run(); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s TFE agent removal failed: %v", containerEngine, err))
			} else {
				result.DockerContainersRemoved += len(tfeAgentIDs)
			}
		}

		// Best effort: remove Terraform CLI helper image if present.
		if out, err := exec.Command(containerEngine, "image", "rm", "-f", tfeCLIHelperImage).CombinedOutput(); err != nil {
			msg := strings.ToLower(strings.TrimSpace(string(out)))
			if !strings.Contains(msg, "no such image") && !strings.Contains(msg, "image not known") {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s helper image removal failed: %s", containerEngine, strings.TrimSpace(string(out))))
			}
		}
	}

	multipassVMs, err := listHALMultipassVMs()
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("multipass discovery failed: %v", err))
	} else {
		for _, vm := range multipassVMs {
			if err := exec.Command("multipass", "delete", vm).Run(); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("multipass delete %q failed: %v", vm, err))
				continue
			}
			result.MultipassVMsDeleted++
		}
		if result.MultipassVMsDeleted > 0 {
			if err := exec.Command("multipass", "purge").Run(); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("multipass purge failed: %v", err))
			}
		}
	}

	removed, existed, err := global.RemoveObsState()
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("observability state cleanup failed: %v", err))
		result.ObsStatus = StatusFailed
	} else if existed || removed {
		result.ObsStatus = StatusCleaned
	} else {
		result.ObsStatus = StatusNotDeployed
	}

	mcpCleanup := mcp.CleanupArtifacts()
	result.Warnings = append(result.Warnings, mcpCleanup.Warnings...)
	if len(mcpCleanup.Warnings) > 0 {
		result.MCPStatus = StatusFailed
	} else if mcpCleanup.ConfigRemoved || mcpCleanup.BinaryRemoved || mcpCleanup.PIDRemoved || mcpCleanup.ProcessStopped {
		result.MCPStatus = StatusCleaned
	} else {
		result.MCPStatus = StatusNotDeployed
	}

	// Remove hal-net now that all containers are gone.
	for _, containerEngine := range containerEngines {
		existed, removed, blockers := global.CleanNetworkIfEmpty(containerEngine)
		if !existed {
			result.NetworkStatus = StatusNotDeployed
		} else if removed {
			result.NetworkStatus = StatusCleaned
		} else {
			result.NetworkStatus = StatusFailed
			result.NetworkBlockers = append(result.NetworkBlockers, blockers...)
		}
	}

	// Remove named Docker/Podman volumes. "docker rm -f" only removes containers;
	// named volumes persist and will be re-mounted with stale data on the next
	// "hal vault create" or "hal vault oidc enable --scim" cycle.
	for _, containerEngine := range containerEngines {
		for _, vol := range halNamedVolumes {
			_ = exec.Command(containerEngine, "volume", "rm", "-f", vol).Run()
		}
	}

	// Reset the shared-services registry so stale consumer entries (e.g.
	// "vault-oidc" under "authentik-idp") don't survive into the next cycle.
	global.ResetSharedServicesFile()

	return result
}

const kindClusterLabelKey = "io.x-k8s.kind.cluster"

func listKindClusters() ([]string, error) {
	viaCLI, cliErr := listKindClustersViaCLI()
	if cliErr == nil {
		return viaCLI, nil
	}

	var viaEngine []string
	engines := detectContainerEngines()
	if len(engines) == 0 {
		engines = []string{detectContainerEngine()}
	}
	for _, engine := range engines {
		found, err := listKindClustersViaEngine(engine)
		if err != nil {
			continue
		}
		viaEngine = append(viaEngine, found...)
	}
	viaEngine = uniqueStrings(viaEngine)
	if len(viaEngine) > 0 {
		return viaEngine, nil
	}

	// Podman 6 stores Labels as a slice, so older kind CLIs fail
	// `kind get clusters` with {{index .Labels ...}}. Engine discovery is the
	// source of truth; if it found nothing, there is nothing left to delete.
	if isKindPodmanLabelTemplateError(cliErr) {
		return nil, nil
	}
	return nil, cliErr
}

func listKindClustersViaCLI() ([]string, error) {
	out, err := kindCommand("get", "clusters").CombinedOutput()
	if err != nil {
		if strings.Contains(strings.ToLower(string(out)), "no kind clusters") {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return parseKindClustersOutput(string(out)), nil
}

func listKindClustersViaEngine(engine string) ([]string, error) {
	formats := []string{
		`{{.Label "` + kindClusterLabelKey + `"}}`,
		`{{index .Labels "` + kindClusterLabelKey + `"}}`,
	}
	var lastErr error
	for _, format := range formats {
		out, err := exec.Command(engine, "ps", "-a",
			"--filter", "label="+kindClusterLabelKey,
			"--format", format).CombinedOutput()
		if err != nil {
			lastErr = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
			if isKindPodmanLabelTemplateError(lastErr) {
				continue
			}
			return nil, lastErr
		}
		return parseKindClustersOutput(string(out)), nil
	}
	return nil, lastErr
}

func isKindPodmanLabelTemplateError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "cannot index slice/array with type string") ||
		(strings.Contains(msg, "index .labels") && strings.Contains(msg, "io.x-k8s.kind.cluster"))
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func parseKindClustersOutput(out string) []string {
	clusters := []string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		cluster := strings.TrimSpace(line)
		if cluster == "" {
			continue
		}
		clusters = append(clusters, cluster)
	}
	return clusters
}

func isHALKindCluster(name string) bool {
	// "kind" is the default cluster name used in local labs when no explicit name is provided.
	return name == "kind" || name == "hal-k8s" || strings.HasPrefix(name, "hal-")
}

func kindCommand(args ...string) *exec.Cmd {
	cmd := exec.Command("kind", args...)
	env := os.Environ()
	if detectContainerEngine() == "podman" {
		env = append(env, "KIND_EXPERIMENTAL_PROVIDER=podman")
	}
	cmd.Env = env
	return cmd
}

func kindDeleteCluster(name string) error {
	out, err := kindCommand("delete", "cluster", "--name", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// removeLeftoverKindNodes force-removes KinD node containers. These are labeled
// io.x-k8s.kind.cluster and named like kind-control-plane — never hal-* — so the
// HAL name sweep cannot see them.
func removeLeftoverKindNodes(engine string) (int, []string) {
	var warnings []string
	ids, err := listKindNodeContainerIDs(engine)
	if err != nil {
		return 0, []string{fmt.Sprintf("kind node discovery via %s failed: %v", engine, err)}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	args := append([]string{"rm", "-f"}, ids...)
	if err := exec.Command(engine, args...).Run(); err != nil {
		warnings = append(warnings, fmt.Sprintf("kind node removal via %s failed: %v", engine, err))
		return 0, warnings
	}
	return len(ids), nil
}

func listKindNodeContainerIDs(engine string) ([]string, error) {
	seen := map[string]bool{}
	var ids []string

	add := func(raw []byte, err error) error {
		if err != nil {
			return err
		}
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			id := strings.TrimSpace(line)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
		return nil
	}

	labeled, err := exec.Command(engine, "ps", "-a",
		"--filter", "label="+kindClusterLabelKey,
		"--format", "{{.ID}}").CombinedOutput()
	if err := add(labeled, err); err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(labeled)))
	}

	named, err := exec.Command(engine, "ps", "-a",
		"--filter", "name=kind-control-plane",
		"--format", "{{.ID}}").CombinedOutput()
	if err := add(named, err); err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(named)))
	}

	return ids, nil
}

func detectContainerEngine() string {
	if engine, err := global.DetectEngine(); err == nil {
		if strings.Contains(engine, "podman") {
			return "podman"
		}
		return "docker"
	}

	if _, err := exec.LookPath("podman"); err == nil {
		return "podman"
	}

	return "docker"
}

func detectContainerEngines() []string {
	engines := []string{}
	if err := exec.Command("docker", "info").Run(); err == nil {
		engines = append(engines, "docker")
	}
	if err := exec.Command("podman", "info").Run(); err == nil {
		engines = append(engines, "podman")
	}
	return engines
}

func listHALContainerIDs(engine string) ([]string, error) {
	out, err := exec.Command(engine, "ps", "-a", "--format", "{{.ID}} {{.Names}}").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}

	ids := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		id := parts[0]
		name := parts[1]
		if strings.HasPrefix(name, "hal-") {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
func listHALMultipassVMs() ([]string, error) {
	out, err := exec.Command("multipass", "list", "--format", "csv").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}

	vms := []string{}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if i == 0 && strings.HasPrefix(strings.ToLower(line), "name,") {
			continue
		}
		name := strings.Split(line, ",")[0]
		name = strings.TrimSpace(name)
		if strings.HasPrefix(name, "hal-") {
			vms = append(vms, name)
		}
	}
	return vms, nil
}
