package global

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const CryptoAgilityOwner = "hal-vault-crypto-agility"
const CryptoAgilityNamespace = "hal-crypto-agility"

func CryptoAgilityStateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".hal", "crypto-agility"), nil
}

func RemoveCryptoAgilityState() error {
	dir, err := CryptoAgilityStateDir()
	if err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(dir, "owner"))
	if os.IsNotExist(err) {
		if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
			return nil
		}
		return fmt.Errorf("refusing to remove unowned crypto agility state: %s", dir)
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(b)) != CryptoAgilityOwner {
		return fmt.Errorf("refusing to remove unowned crypto agility state: %s", dir)
	}
	return os.RemoveAll(dir)
}

// CryptoAgilityReadiness reports Kubernetes deployment readiness only.
// Certificate validation and TLS negotiation are observed separately.
func CryptoAgilityReadiness() string {
	out, err := exec.Command("kubectl", "--context", "kind-kind", "--request-timeout=3s",
		"get", "deployment", "app", "-n", CryptoAgilityNamespace, "-o", "json").Output()
	if err != nil {
		return "down"
	}
	var deployment struct {
		Status struct {
			ReadyReplicas int `json:"readyReplicas"`
		} `json:"status"`
	}
	if json.Unmarshal(out, &deployment) != nil {
		return "down"
	}
	if deployment.Status.ReadyReplicas < 1 {
		return "degraded"
	}
	return "up"
}
