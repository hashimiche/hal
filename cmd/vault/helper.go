package vault

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
	config := "kind: Cluster\napiVersion: kind.x-k8s.io/v1alpha4\nnodes:\n- role: control-plane\n  extraPortMappings:\n    - containerPort: 30080\n      hostPort: 8088\n      protocol: TCP\n    - containerPort: 30082\n      hostPort: 8089\n      protocol: TCP\n    - containerPort: 30083\n      hostPort: 8090\n      protocol: TCP\n    - containerPort: 30084\n      hostPort: 8091\n      protocol: TCP\n"
	if _, err := f.WriteString(config); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}

const kindControlPlaneName = "kind-control-plane"

// kindClusterEnvExtras returns the environment overrides KinD needs so the
// node joins hal-net. Docker reads KIND_EXPERIMENTAL_DOCKER_NETWORK; Podman
// reads KIND_EXPERIMENTAL_PODMAN_NETWORK (KIND_EXPERIMENTAL_DOCKER_NETWORK is
// ignored by the Podman provider). Both are always set so whichever provider
// the installed kind CLI uses, the cluster lands on the HAL network.
func kindClusterEnvExtras(engine string) []string {
	extras := []string{
		"KIND_EXPERIMENTAL_DOCKER_NETWORK=" + global.HalNetName,
		"KIND_EXPERIMENTAL_PODMAN_NETWORK=" + global.HalNetName,
	}
	if strings.Contains(engine, "podman") {
		extras = append(extras, "KIND_EXPERIMENTAL_PROVIDER=podman")
	}
	return extras
}

func kindCreateEnv(engine string) []string {
	return append(os.Environ(), kindClusterEnvExtras(engine)...)
}

func halKindClusterRunning() bool {
	out, err := exec.Command("kind", "get", "clusters").Output()
	return err == nil && strings.Contains(string(out), "kind")
}

func halNetIPInspectFormat() string {
	return fmt.Sprintf(`{{range $k,$v := .NetworkSettings.Networks}}{{if eq $k "%s"}}{{$v.IPAddress}}{{end}}{{end}}`, global.HalNetName)
}

// containerIPOnHalNet returns the container's IPv4 address on hal-net, or "".
// Inspecting every attached network concatenates IPs when the node is dual-homed
// (kind + hal-net), which newer kind CLIs do when they ignore the experimental
// network env and HAL then connects the node as a second NIC.
func containerIPOnHalNet(engine, name string) string {
	out, err := exec.Command(engine, "inspect", "-f", halNetIPInspectFormat(), name).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func containerIPOnHalNetOrName(engine, name string) string {
	if ip := containerIPOnHalNet(engine, name); ip != "" {
		return ip
	}
	return name
}

func isAlreadyConnectedNetworkError(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "already") && strings.Contains(lower, "network")
}

func attachContainerToHalNet(engine, name string) error {
	if containerIPOnHalNet(engine, name) != "" {
		return nil
	}
	fmt.Printf("⚙️  Attaching %s to %s...\n", name, global.HalNetName)
	out, err := exec.Command(engine, "network", "connect", global.HalNetName, name).CombinedOutput()
	if err != nil {
		combined := string(out) + err.Error()
		if isAlreadyConnectedNetworkError(combined) {
			return nil
		}
		return fmt.Errorf("failed to attach %s to %s: %v (%s)", name, global.HalNetName, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func kindNodeNames() []string {
	out, err := exec.Command("kind", "get", "nodes").Output()
	if err == nil {
		var nodes []string
		for _, n := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if n = strings.TrimSpace(n); n != "" {
				nodes = append(nodes, n)
			}
		}
		if len(nodes) > 0 {
			return nodes
		}
	}
	return []string{kindControlPlaneName}
}

func attachKindNodesToHalNet(engine string) error {
	for _, node := range kindNodeNames() {
		if err := attachContainerToHalNet(engine, node); err != nil {
			return err
		}
	}
	return nil
}

// ensureHALKindCluster creates the shared HAL KinD cluster if it is missing
// and always attaches its node(s) to hal-net. Newer kind releases (needed for
// kindest/node v1.36+) may ignore KIND_EXPERIMENTAL_DOCKER_NETWORK or, on
// Podman, only honor KIND_EXPERIMENTAL_PODMAN_NETWORK — the explicit connect
// is the fallback so Vault on hal-net can still reach the API server.
func ensureHALKindCluster(engine, nodeImage string) error {
	global.EnsureNetwork(engine)

	if halKindClusterRunning() {
		fmt.Println("⚡ KinD cluster already running, skipping boot sequence...")
	} else {
		fmt.Println("🚀 Booting KinD cluster (attached to hal-net)...")
		kindConfigPath, err := writeHALKindConfig()
		if err != nil {
			return fmt.Errorf("failed to prepare KinD config: %w", err)
		}
		defer func() { _ = os.Remove(kindConfigPath) }()

		startCmd := exec.Command("kind", "create", "cluster", "--config", kindConfigPath)
		if strings.TrimSpace(nodeImage) != "" {
			startCmd.Args = append(startCmd.Args, "--image", nodeImage)
		}
		startCmd.Env = kindCreateEnv(engine)
		startCmd.Stdout = os.Stdout
		startCmd.Stderr = os.Stderr
		if err := startCmd.Run(); err != nil {
			return fmt.Errorf("failed to start KinD: %w", err)
		}
	}

	return attachKindNodesToHalNet(engine)
}
