package global

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const PQCPKIOwner = "hal-vault-pqc-pki"
const PQCPKINamespace = "hal-pqc"

func PQCPKIStateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".hal", "pqc-pki"), nil
}

// RemovePQCPKIState never adopts a directory without its feature owner marker.
func RemovePQCPKIState() error {
	dir, err := PQCPKIStateDir()
	if err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(dir, "owner"))
	if os.IsNotExist(err) {
		if _, e := os.Stat(dir); os.IsNotExist(e) {
			return nil
		}
		return fmt.Errorf("refusing to remove PQC state without an owner marker: %s", dir)
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(b)) != PQCPKIOwner {
		return fmt.Errorf("refusing to remove unowned PQC state: %s", dir)
	}
	return os.RemoveAll(dir)
}

// PQCPKIReadiness is deployment readiness, never a cryptographic attestation.
func PQCPKIReadiness() string {
	out, err := exec.Command("kubectl", "--context", "kind-kind", "--request-timeout=3s", "get", "deployments", "-n", PQCPKINamespace, "-l", "app.kubernetes.io/managed-by="+PQCPKIOwner, "-o", "json").Output()
	if err != nil {
		return "down"
	}
	var d struct {
		Items []struct {
			Status struct {
				ReadyReplicas int `json:"readyReplicas"`
			}
		}
	}
	if json.Unmarshal(out, &d) != nil || len(d.Items) != 2 {
		return "down"
	}
	for _, item := range d.Items {
		if item.Status.ReadyReplicas < 1 {
			return "degraded"
		}
	}
	return "up"
}
