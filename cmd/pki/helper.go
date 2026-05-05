package pki

import (
	"fmt"
	"os"

	vault "github.com/hashicorp/vault/api"
)

// getVaultClient returns a healthy Vault client using local lab defaults.
// VAULT_ADDR defaults to http://127.0.0.1:8200; VAULT_TOKEN defaults to "root".
func getVaultClient() (*vault.Client, error) {
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
	health, err := client.Sys().Health()
	if err != nil {
		return nil, fmt.Errorf("vault is unreachable — run 'hal vault create' first")
	}
	if !health.Initialized || health.Sealed {
		return nil, fmt.Errorf("vault is sealed or uninitialized")
	}
	return client, nil
}

// parsePKILifecycleAction parses the positional action argument (enable/disable/update/status).
func parsePKILifecycleAction(args []string, enable, disable, update *bool) error {
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "status":
		// default smart status
	case "enable":
		*enable = true
	case "disable":
		*disable = true
	case "update":
		*update = true
	default:
		return fmt.Errorf("unknown action %q (expected: status, enable, disable, update)", args[0])
	}
	return nil
}
