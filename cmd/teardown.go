package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"hal/internal/global"
)

const tfeCLIHelperImage = "hal-tfe-cli:latest"

type globalTeardownResult struct {
	DockerContainersRemoved int
	KindClustersDeleted     int
	MultipassVMsDeleted     int
	ObsStateCleaned         bool
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
			if err := exec.Command("kind", "delete", "cluster", "--name", cluster).Run(); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("kind delete cluster %q failed: %v", cluster, err))
			} else {
				result.KindClustersDeleted++
			}

			// Best effort: if kind deletion leaves node containers behind, remove them by cluster label.
			for _, containerEngine := range containerEngines {
				leftoverNodeIDs, err := listContainerIDsByLabel(containerEngine, "io.x-k8s.kind.cluster", cluster)
				if err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("leftover kind container discovery for %q via %s failed: %v", cluster, containerEngine, err))
					continue
				}
				if len(leftoverNodeIDs) == 0 {
					continue
				}
				args := append([]string{"rm", "-f"}, leftoverNodeIDs...)
				if err := exec.Command(containerEngine, args...).Run(); err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("leftover kind container removal for %q via %s failed: %v", cluster, containerEngine, err))
				}
			}
		}
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

	if err := global.RemoveObsState(); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("observability state cleanup failed: %v", err))
	} else {
		result.ObsStateCleaned = true
	}

	return result
}

func listKindClusters() ([]string, error) {
	out, err := exec.Command("kind", "get", "clusters").CombinedOutput()
	if err != nil {
		if strings.Contains(strings.ToLower(string(out)), "no kind clusters") {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}

	clusters := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		cluster := strings.TrimSpace(line)
		if cluster == "" {
			continue
		}
		clusters = append(clusters, cluster)
	}
	return clusters, nil
}

func isHALKindCluster(name string) bool {
	// "kind" is the default cluster name used in local labs when no explicit name is provided.
	return name == "kind" || name == "hal-k8s" || strings.HasPrefix(name, "hal-")
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

func listContainerIDsByLabel(engine, labelKey string, labelValue string) ([]string, error) {
	filter := fmt.Sprintf("label=%s=%s", labelKey, labelValue)
	out, err := exec.Command(engine, "ps", "-a", "--filter", filter, "--format", "{{.ID}}").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}

	ids := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		id := strings.TrimSpace(line)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
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
