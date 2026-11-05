package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"hal/cmd/mcp"
	"hal/internal/agenticiam"
	"hal/internal/global"
	"hal/internal/labsecrets"
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
	"hal-tfe-s3-data",    // TFE S3 object storage (cmd/terraform/create.go)
	// legacy: pre-VersityGW MinIO volume, kept so `hal teardown` still reclaims
	// it on machines that ran the old stack.
	"hal-tfe-minio-data",
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
	KindClustersSkipped     []string // kind clusters left alone because HAL did not create them
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

	// Only kind clusters HAL created are deleted (ADR 0003, decision 6): a user's
	// own cluster, including a default one named "kind", is never touched.
	kindDeleted, kindSkipped, kindWarnings := teardownKindClusters(containerEngines)
	result.KindClustersDeleted = kindDeleted
	result.KindClustersSkipped = kindSkipped
	result.Warnings = append(result.Warnings, kindWarnings...)

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

		// Legacy custom PQC image caches are removed globally. Stock NGINX
		// distribution images are shared and are not removed here.
		if out, err := exec.Command(containerEngine, "images", "--filter", "reference=localhost/hal-pqc-pki:*", "-q").Output(); err == nil {
			if ids := strings.Fields(string(out)); len(ids) > 0 {
				if err := exec.Command(containerEngine, append([]string{"image", "rm"}, ids...)...).Run(); err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("PQC image cleanup failed: %v", err))
				}
			}
		}
		// The Agentic IAM lab's images: every hal-* container is gone, so none
		// is in use any more.
		if _, err := agenticiam.RemoveImages(containerEngine); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s Agentic IAM image removal failed: %v", containerEngine, err))
		}
	}

	multipassDeleted, multipassWarnings := teardownMultipassVMs()
	result.MultipassVMsDeleted = multipassDeleted
	result.Warnings = append(result.Warnings, multipassWarnings...)

	if err := global.RemovePQCPKIState(); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("PQC PKI state cleanup failed: %v", err))
	}

	// Every lab is gone, so the credentials they generated go with them.
	if err := labsecrets.RemoveStore(); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("lab credential store cleanup failed: %v", err))
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

// teardownKindClusters deletes the kind clusters HAL created on every engine
// and leaves all others alone. The skipped clusters are returned (as
// "name (engine)") so the command can tell the user why they survived; that is
// the lab invariant working, not a failure, so they are not warnings.
func teardownKindClusters(engines []string) (deleted int, skipped, warnings []string) {
	for _, engine := range engines {
		res := global.DeleteHALKindClusters(engine)
		deleted += len(res.Deleted)
		warnings = append(warnings, res.Warnings...)
		for _, name := range res.Skipped {
			skipped = append(skipped, fmt.Sprintf("%s (%s)", name, engine))
		}
	}
	return deleted, skipped, warnings
}

// teardownMultipassVMs deletes every HAL (hal-*) Multipass VM, purging each one
// individually. It never runs a bare `multipass purge`, which would also destroy
// VMs the user deleted themselves.
func teardownMultipassVMs() (deleted int, warnings []string) {
	vms, err := global.ListHALMultipassVMs()
	if err != nil {
		return 0, []string{fmt.Sprintf("multipass discovery failed: %v", err)}
	}
	for _, vm := range vms {
		if err := global.DeleteMultipassVM(vm); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s failed: %v", global.MultipassDeleteCommand(vm), err))
			continue
		}
		deleted++
	}
	return deleted, warnings
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
