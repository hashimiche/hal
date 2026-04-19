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
