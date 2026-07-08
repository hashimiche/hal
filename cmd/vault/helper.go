package vault

import (
	"fmt"
	"os"
	"path/filepath"

	"hal/internal/global"

	vault "github.com/hashicorp/vault/api"
)

// vaultProdCertPath returns the host path to the prod TLS cert, or "" if the
// home directory is unavailable.
func vaultProdCertPath() string {
	dir := global.VaultProdStateDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, vaultProdCertsDirName, vaultProdCertFileName)
}

// vaultProdActive reports whether a production-mode instance has been stood up,
// detected by the presence of its forged TLS certificate. Host-side clients use
// this to pick the HTTPS scheme + CA and the cached root token.
func vaultProdActive() bool {
	certPath := vaultProdCertPath()
	if certPath == "" {
		return false
	}
	_, err := os.Stat(certPath)
	return err == nil
}

// GetHealthyClient initializes the Vault client, sets the token,
// and acts as a load-balancer style pre-flight check.
func GetHealthyClient() (*vault.Client, error) {
	config := vault.DefaultConfig()
	prod := vaultProdActive()

	if os.Getenv("VAULT_ADDR") == "" {
		if prod {
			config.Address = vaultProdLocalAPIURL
			// Trust the forged self-signed cert for the prod HTTPS listener.
			if certPath := vaultProdCertPath(); certPath != "" {
				_ = config.ConfigureTLS(&vault.TLSConfig{CACert: certPath})
			}
		} else {
			config.Address = vaultLocalAPIURL
		}
	}

	client, err := vault.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Vault client: %w", err)
	}

	if os.Getenv("VAULT_TOKEN") == "" {
		token := vaultRootToken
		if prod {
			if vi, err := global.LoadCachedVaultInit(); err == nil && vi.RootToken != "" {
				token = vi.RootToken
			}
		}
		client.SetToken(token)
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
