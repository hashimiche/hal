package vault

import (
	"fmt"
	"os"

	vault "github.com/hashicorp/vault/api"
)

// GetHealthyClient initializes the Vault client, sets the token,
// and acts as a load-balancer style pre-flight check.
func GetHealthyClient() (*vault.Client, error) {
	config := vault.DefaultConfig()
	if os.Getenv("VAULT_ADDR") == "" {
		config.Address = "http://127.0.0.1:8200"
	}

	client, err := vault.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Vault client: %w", err)
	}

	if os.Getenv("VAULT_TOKEN") == "" {
		client.SetToken("root")
	}

	// The LB-Style Pre-Flight Health Check
	health, err := client.Sys().Health()
	if err != nil {
		return nil, fmt.Errorf("Vault is unreachable. Is it running? (Hint: run 'hal vault create')")
	}
	if !health.Initialized || health.Sealed {
		return nil, fmt.Errorf("Vault is running but is either sealed or uninitialized")
	}

	return client, nil
}

// writeHALKindConfig writes a shared KinD cluster config with all HAL port mappings:
//   - 30080 → 8088  (hal vault k8s   — VSO web demo)
//   - 30082 → 8089  (hal vault pki --k8s   — cert-manager / nginx demo)
//   - 30083 → 8090  (hal vault pki --acme  — ACME / Caddy demo)
//
// All three ports are declared upfront so any combination of demos can be
// enabled on the same cluster without recreating it.
func writeHALKindConfig() (string, error) {
	f, err := os.CreateTemp("", "hal-kind-*.yaml")
	if err != nil {
		return "", err
	}
	config := `kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraPortMappings:
    - containerPort: 30080
      hostPort: 8088
      protocol: TCP
    - containerPort: 30082
      hostPort: 8089
      protocol: TCP
    - containerPort: 30083
      hostPort: 8090
      protocol: TCP
`
	if _, err := f.WriteString(config); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}
