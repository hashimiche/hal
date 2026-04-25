package global

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// HalStatusImage is the container image used for hal-status.
// Reuses the hal-mcp image since it already embeds the hal binary.
const HalStatusImage = "hashimiche/hal-mcp:latest"

// ProductFeature represents a single feature of a product.
type ProductFeature struct {
	Feature string `json:"feature"`
	State   string `json:"state"`
	Health  string `json:"health"`
	Reason  string `json:"reason"`
}

// ProductStatus is the status of a single product in the HAL ecosystem.
type ProductStatus struct {
	Product    string           `json:"product"`
	State      string           `json:"state"`
	Health     string           `json:"health"`
	Reason     string           `json:"reason"`
	Endpoint   string           `json:"endpoint"`
	Containers []string         `json:"containers"`
	Features   []ProductFeature `json:"features"`
}

// StatusSnapshot is the full runtime snapshot written into hal-status.
type StatusSnapshot struct {
	Timestamp string          `json:"timestamp"`
	Engine    string          `json:"engine"`
	Products  []ProductStatus `json:"products"`
}

// BuildStatusSnapshot inspects the live container engine and returns a JSON-encoded snapshot.
// Must be called on the host (has engine socket access).
func BuildStatusSnapshot(engine string) ([]byte, error) {
	products := []ProductStatus{
		buildProductStatus(engine, "consul",
			[]string{"hal-consul"},
			map[string]string{"core": BoolState(CheckContainer(engine, "hal-consul"))},
			"http://consul.localhost:8500"),

		buildProductStatus(engine, "vault",
			[]string{"hal-vault"},
			map[string]string{
				"audit":    resolveVaultAudit(engine),
				"k8s":      BoolState(CheckContainer(engine, "kind-control-plane")),
				"jwt":      BoolState(CheckContainer(engine, "hal-gitlab")),
				"ldap":     BoolState(CheckContainer(engine, "hal-openldap")),
				"database": BoolState(CheckContainer(engine, "hal-vault-mariadb")),
				"oidc":     BoolState(CheckContainer(engine, "hal-keycloak")),
			},
			"http://vault.localhost:8200"),

		buildProductStatus(engine, "nomad",
			[]string{"hal-nomad"},
			map[string]string{"job": BoolState(CheckMultipass("hal-nomad"))},
			"multipass://hal-nomad"),

		buildProductStatus(engine, "boundary",
			[]string{"hal-boundary"},
			map[string]string{
				"mariadb": BoolState(CheckContainer(engine, "hal-boundary-target-mariadb")),
				"ssh":     BoolState(CheckMultipass("hal-boundary-ssh")),
			},
			"http://boundary.localhost:9200"),

		buildProductStatus(engine, "terraform",
			[]string{"hal-tfe", "hal-tfe-db", "hal-tfe-redis", "hal-tfe-minio", "hal-tfe-proxy"},
			map[string]string{"workspace": BoolState(CheckContainer(engine, "hal-tfe") && CheckContainer(engine, "hal-gitlab"))},
			"https://tfe.localhost:8443"),

		buildProductStatus(engine, "obs",
			[]string{"hal-grafana", "hal-prometheus", "hal-loki"},
			map[string]string{
				"grafana":    BoolState(CheckContainer(engine, "hal-grafana")),
				"prometheus": BoolState(CheckContainer(engine, "hal-prometheus")),
				"loki":       BoolState(CheckContainer(engine, "hal-loki")),
			},
			"http://grafana.localhost:3000"),
	}

	snap := StatusSnapshot{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Engine:    engine,
		Products:  products,
	}
	return json.Marshal(snap)
}

// RefreshHalStatus (re)creates the hal-status container with a fresh snapshot.
// It is safe to call from any product create/update/delete — it is a no-op when
// hal-net does not exist yet (hal-plus has not been deployed) and silently skips
// when the hal-mcp image is not present locally.
func RefreshHalStatus(engine string) {
	// Only run when hal-net exists — avoids creating it just for the status container.
	netOut, _ := exec.Command(engine, "network", "ls", "--format", "{{.Name}}").Output()
	if !strings.Contains(string(netOut), "hal-net") {
		return
	}

	// Only run when the image is available — hal-status is optional.
	imgOut, _ := exec.Command(engine, "images", "-q", HalStatusImage).Output()
	if strings.TrimSpace(string(imgOut)) == "" {
		return
	}

	data, err := BuildStatusSnapshot(engine)
	if err != nil {
		if Debug {
			fmt.Printf("[DEBUG] hal-status: snapshot build failed: %v\n", err)
		}
		return
	}

	// Remove old container (ignore errors) and start fresh with the new snapshot.
	_ = exec.Command(engine, "rm", "-f", HalStatusContainerName).Run()

	args := []string{
		"run", "-d",
		"--name", HalStatusContainerName,
		"--network", "hal-net",
		"--entrypoint", "/usr/local/bin/hal",
		"-e", fmt.Sprintf("HAL_STATUS_DATA=%s", string(data)),
		"-e", fmt.Sprintf("HAL_STATUS_PORT=%d", HalStatusPort),
		HalStatusImage,
		"health", "_serve",
	}
	if out, err := exec.Command(engine, args...).CombinedOutput(); err != nil {
		if Debug {
			fmt.Printf("[DEBUG] hal-status: container start failed: %v\n%s\n", err, string(out))
		}
	} else if Debug {
		fmt.Printf("[DEBUG] hal-status: container refreshed (snapshot ts=%s)\n",
			time.Now().UTC().Format(time.RFC3339))
	}
}

// RemoveHalStatus stops and removes the hal-status container.
func RemoveHalStatus(engine string) {
	_ = exec.Command(engine, "rm", "-f", HalStatusContainerName).Run()
}

func buildProductStatus(engine, product string, containers []string, features map[string]string, endpoint string) ProductStatus {
	runningCount := 0
	for _, c := range containers {
		if c == "hal-nomad" {
			if CheckMultipass("hal-nomad") {
				runningCount++
			}
			continue
		}
		if CheckContainer(engine, c) {
			runningCount++
		}
	}

	state := "not_deployed"
	health := "down"
	reason := "required resources are not running"
	switch {
	case runningCount == len(containers):
		state, health, reason = "running", "healthy", "all required resources are running"
	case runningCount > 0:
		state, health, reason = "partial", "degraded", "some resources are running"
	}
	// single-container products: running when that one container is up
	if len(containers) == 1 && runningCount == 1 {
		state, health, reason = "running", "healthy", "primary resource is running"
	}

	featureRows := make([]ProductFeature, 0, len(features))
	for k, v := range features {
		fh, fr := "down", "feature is disabled"
		if v == "enabled" {
			fh, fr = "healthy", "feature is enabled"
		}
		featureRows = append(featureRows, ProductFeature{Feature: k, State: v, Health: fh, Reason: fr})
	}
	sort.Slice(featureRows, func(i, j int) bool { return featureRows[i].Feature < featureRows[j].Feature })

	return ProductStatus{
		Product:    product,
		State:      state,
		Health:     health,
		Reason:     reason,
		Endpoint:   endpoint,
		Containers: containers,
		Features:   featureRows,
	}
}

func resolveVaultAudit(engine string) string {
	if !CheckContainer(engine, "hal-vault") {
		return "disabled"
	}
	out, err := exec.Command(
		engine, "exec",
		"-e", "VAULT_ADDR=http://127.0.0.1:8200",
		"-e", "VAULT_TOKEN=root",
		"hal-vault",
		"vault", "audit", "list", "-format=json",
	).Output()
	if err != nil {
		return "unknown"
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "{}" || trimmed == "" {
		return "disabled"
	}
	return "enabled"
}
