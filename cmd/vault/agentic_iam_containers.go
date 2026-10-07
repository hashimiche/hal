package vault

// agentic_iam_containers.go runs the two containers of the Agentic IAM lab
// (ADR 0004, decision 8) from the image of internal/agenticiam: the chat (web
// page and BFF), published on the host, and the demo agent, on hal-net only.
//
// Both are configured by environment variables only (contract:
// internal/agenticiam/app/README.md). The secrets among them (client secrets,
// the actor's app password) never appear on a command line: each container
// reads its own env file, written with mode 0600 under ~/.hal/agentic-iam/,
// and gets only the variables it needs. The chat never sees the token exchange
// credentials, and the demo agent never sees the chat's client secret.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"hal/internal/global"
	"hal/internal/integrations"
)

const (
	// agenticIAMConfigLabel carries a hash of a container's whole configuration
	// (image, run arguments and env file). enable keeps a running container
	// whose label matches, and recreates it otherwise; update always recreates.
	agenticIAMConfigLabel = "hal.agentic-iam.config"

	// agenticIAMVaultCAPath is where the demo agent finds the CA of a prod
	// (TLS) Vault. Python's default TLS context honours SSL_CERT_FILE.
	agenticIAMVaultCAPath = "/etc/hal-agentic-iam/vault-ca.pem"

	agenticIAMHealthTimeout = 60 * time.Second
)

// agenticIAMEnvVar is one line of an env file.
type agenticIAMEnvVar struct {
	Key, Value string
}

// agenticIAMContainer is one container of the lab, ready to run.
type agenticIAMContainer struct {
	Name    string
	Command string // "chat" or "agent": the image's sub-command
	Port    int    // port inside the container
	// HostPort is the published host port, 0 for a container on hal-net only.
	HostPort int
	EnvFile  string // absolute path of its env file
	Env      []agenticIAMEnvVar
	// Volumes are -v values, e.g. the prod Vault CA, read-only.
	Volumes []string
}

// agenticIAMStateDir returns ~/.hal/agentic-iam, which holds the env files.
func agenticIAMStateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find the home directory: %w", err)
	}
	return filepath.Join(home, ".hal", agenticIAMStateDirName), nil
}

// agenticIAMVaultAddr is the Vault address the demo agent uses on hal-net, and
// the CA it must trust: a prod Vault serves TLS with HAL's self-signed cert,
// which names hal-vault.
func agenticIAMVaultAddr(prod bool, prodCertPath string) (addr, caCert string) {
	if prod {
		return fmt.Sprintf("https://%s:%d", vaultContainer, vaultHTTPPort), prodCertPath
	}
	return fmt.Sprintf("http://%s:%d", vaultContainer, vaultHTTPPort), ""
}

// agenticIAMContainers returns the demo agent then the chat, configured from
// what Authentik returned. dir is the env files' directory; vaultCACert is the
// host path of the prod Vault CA, or "" for a dev Vault on plain HTTP.
// Every variable is set, defaults included, so that the Go constants stay the
// single source of truth for the mount, roles, schema and hosts.
func agenticIAMContainers(ak *integrations.AgenticIAMAuthentik, dir, vaultAddr, vaultCACert string) []agenticIAMContainer {
	agentEnv := []agenticIAMEnvVar{
		{"LISTEN_PORT", strconv.Itoa(agenticIAMAgentPort)},
		{"TOKEN_ENDPOINT", ak.TokenEndpoint},
		{"EXCHANGE_CLIENT_ID", ak.VaultAgentic.ClientID},
		{"EXCHANGE_CLIENT_SECRET", ak.VaultAgentic.ClientSecret},
		{"ACTOR_CLIENT_ID", ak.DemoAgent.ClientID},
		{"ACTOR_CLIENT_SECRET", ak.DemoAgent.ClientSecret},
		{"ACTOR_USERNAME", ak.ActorUsername},
		{"ACTOR_APP_PASSWORD", ak.ActorAppPassword},
		{"VAULT_ADDR", vaultAddr},
		{"DB_MOUNT", agenticDBMount},
		{"DB_ROLE_QUARTERLY_RESULTS", agenticDBQuarterlyResults.Name},
		{"DB_ROLE_FORECASTS", agenticDBForecasts.Name},
		{"DB_ROLE_PAYROLL", agenticDBPayroll.Name},
		{"DB_HOST", vaultMariaDBContainer},
		{"DB_PORT", strconv.Itoa(vaultMariaDBPort)},
		{"DB_NAME", agenticIAMSchema},
	}
	var agentVolumes []string
	if vaultCACert != "" {
		agentEnv = append(agentEnv, agenticIAMEnvVar{"SSL_CERT_FILE", agenticIAMVaultCAPath})
		agentVolumes = append(agentVolumes, vaultCACert+":"+agenticIAMVaultCAPath+":ro")
	}

	return []agenticIAMContainer{
		{
			Name:    agenticIAMAgentContainer,
			Command: "agent",
			Port:    agenticIAMAgentPort,
			EnvFile: filepath.Join(dir, "agent.env"),
			Env:     agentEnv,
			Volumes: agentVolumes,
		},
		{
			Name:     agenticIAMChatContainer,
			Command:  "chat",
			Port:     agenticIAMChatPort,
			HostPort: agenticIAMChatHostPort,
			EnvFile:  filepath.Join(dir, "chat.env"),
			Env: []agenticIAMEnvVar{
				{"LISTEN_PORT", strconv.Itoa(agenticIAMChatPort)},
				{"PUBLIC_URL", agenticIAMChatPublicURL},
				{"OIDC_ISSUER", ak.ChatIssuer},
				{"OIDC_CLIENT_ID", ak.Chat.ClientID},
				{"OIDC_CLIENT_SECRET", ak.Chat.ClientSecret},
				{"AGENT_URL", agenticIAMAgentURL()},
			},
		},
	}
}

// agenticIAMAgentURL is the demo agent's address on hal-net, for the chat.
func agenticIAMAgentURL() string {
	return fmt.Sprintf("http://%s:%d", agenticIAMAgentContainer, agenticIAMAgentPort)
}

// envFile renders the container's env file. docker and podman read each line
// as KEY=VALUE, literally: no quoting, no escaping, so a value must hold no line
// break. Every value is required.
func (c agenticIAMContainer) envFile() (string, error) {
	var b strings.Builder
	b.WriteString("# Written by hal vault agentic-iam for " + c.Name + ". Holds secrets: mode 0600.\n")
	for _, v := range c.Env {
		switch {
		case strings.TrimSpace(v.Value) == "":
			return "", fmt.Errorf("%s: %s is empty", c.Name, v.Key)
		case strings.ContainsAny(v.Value, "\r\n"):
			return "", fmt.Errorf("%s: %s holds a line break, which an env file cannot carry", c.Name, v.Key)
		}
		b.WriteString(v.Key + "=" + v.Value + "\n")
	}
	return b.String(), nil
}

// runArgs returns the `run` arguments of the container: hal-net, the published
// port if any, --env-file, the config label when not empty, then the image and
// its sub-command. No value of the env file appears here.
func (c agenticIAMContainer) runArgs(image, configLabel string) []string {
	args := []string{
		"run", "-d",
		"--name", c.Name,
		"--network", global.HalNetName,
		"--restart", "unless-stopped",
		"--env-file", c.EnvFile,
	}
	if c.HostPort != 0 {
		args = append(args, "-p", fmt.Sprintf("%d:%d", c.HostPort, c.Port))
	}
	for _, v := range c.Volumes {
		args = append(args, "-v", v)
	}
	if configLabel != "" {
		args = append(args, "--label", agenticIAMConfigLabel+"="+configLabel)
	}
	return append(args, image, c.Command)
}

// configHash identifies the container's whole configuration: the run
// arguments (which include the image) and the env file's content.
func (c agenticIAMContainer) configHash(image, envFile string) string {
	h := sha256.New()
	h.Write([]byte(strings.Join(c.runArgs(image, ""), "\x00")))
	h.Write([]byte{0})
	h.Write([]byte(envFile))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// writeAgenticIAMEnvFile writes an env file with mode 0600 in a 0700
// directory. An existing file keeps no wider mode than 0600.
func writeAgenticIAMEnvFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("restrict %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return os.Chmod(path, 0o600)
}

// agenticIAMContainerCurrent reports whether the container runs with exactly
// this configuration.
func agenticIAMContainerCurrent(engine, name, hash string) bool {
	out, err := exec.Command(engine, "inspect", "-f",
		`{{.State.Running}} {{index .Config.Labels "`+agenticIAMConfigLabel+`"}}`, name).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true "+hash
}

// agenticIAMContainerExists reports whether the container exists, in any state.
func agenticIAMContainerExists(engine, name string) bool {
	return exec.Command(engine, "inspect", "--format", "{{.Name}}", name).Run() == nil
}

// startAgenticIAMContainers writes both env files and runs both containers
// from image, the demo agent first. With recreate false, a container already
// running with the same configuration is kept. Then it waits for both
// /healthz.
func startAgenticIAMContainers(engine, image string, containers []agenticIAMContainer, recreate bool, step func(string, ...any)) error {
	global.EnsureNetwork(engine)
	for _, c := range containers {
		content, err := c.envFile()
		if err != nil {
			return err
		}
		if err := writeAgenticIAMEnvFile(c.EnvFile, content); err != nil {
			return err
		}
		hash := c.configHash(image, content)
		if !recreate && agenticIAMContainerCurrent(engine, c.Name, hash) {
			step("%s already runs this configuration — kept", c.Name)
			continue
		}

		step("Starting %s", c.Name)
		_ = exec.Command(engine, "rm", "-f", c.Name).Run()
		if out, err := exec.Command(engine, c.runArgs(image, hash)...).CombinedOutput(); err != nil {
			return fmt.Errorf("start %s: %v\n%s", c.Name, err, strings.TrimSpace(string(out)))
		}
	}

	step("Waiting for the chat and the demo agent to answer /healthz")
	return waitAgenticIAMHealthy(engine)
}

// waitAgenticIAMHealthy waits for the chat's /healthz from the host, through
// the published port, then for the demo agent's /healthz from inside the chat,
// over hal-net, which is the path the chat uses.
func waitAgenticIAMHealthy(engine string) error {
	httpClient := &http.Client{Timeout: 3 * time.Second}
	chatHealth := fmt.Sprintf("http://127.0.0.1:%d/healthz", agenticIAMChatHostPort)
	err := waitAgenticIAMCondition(engine, agenticIAMChatContainer, func() bool {
		resp, err := httpClient.Get(chatHealth)
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
	if err != nil {
		return fmt.Errorf("the chat did not answer %s: %w", chatHealth, err)
	}

	agentHealth := agenticIAMAgentURL() + "/healthz"
	probe := "import sys, urllib.request; urllib.request.urlopen(sys.argv[1], timeout=3)"
	err = waitAgenticIAMCondition(engine, agenticIAMAgentContainer, func() bool {
		return exec.Command(engine, "exec", agenticIAMChatContainer, "python", "-c", probe, agentHealth).Run() == nil
	})
	if err != nil {
		return fmt.Errorf("the demo agent did not answer %s from the chat: %w", agentHealth, err)
	}
	return nil
}

// waitAgenticIAMCondition polls ready until it holds, the container stops, or
// the timeout. A stopped container's last log lines are part of the error: a
// configuration error stops either container at start (exit code 2).
func waitAgenticIAMCondition(engine, container string, ready func() bool) error {
	deadline := time.Now().Add(agenticIAMHealthTimeout)
	for {
		if ready() {
			return nil
		}
		if !global.IsContainerRunning(engine, container) {
			logs, _ := exec.Command(engine, "logs", "--tail", "15", container).CombinedOutput()
			return fmt.Errorf("%s is not running. Last log lines:\n%s", container, strings.TrimSpace(string(logs)))
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("no answer within %s (check: %s logs %s)", agenticIAMHealthTimeout, engine, container)
		}
		time.Sleep(2 * time.Second)
	}
}

// removeAgenticIAMContainers removes both containers. Missing ones are fine.
func removeAgenticIAMContainers(engine string) error {
	var errs []error
	for _, name := range []string{agenticIAMChatContainer, agenticIAMAgentContainer} {
		if !agenticIAMContainerExists(engine, name) {
			continue
		}
		if out, err := exec.Command(engine, "rm", "-f", name).CombinedOutput(); err != nil {
			errs = append(errs, fmt.Errorf("remove %s: %v: %s", name, err, strings.TrimSpace(string(out))))
		}
	}
	return errors.Join(errs...)
}

// removeAgenticIAMStateDir deletes ~/.hal/agentic-iam and the env files in it.
// Their secrets are worthless once the lab's Authentik objects are gone.
func removeAgenticIAMStateDir() error {
	dir, err := agenticIAMStateDir()
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// agenticIAMHostPortInUse reports whether something already answers on the
// chat's host port. Run it only while the chat container does not exist.
func agenticIAMHostPortInUse() bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", agenticIAMChatHostPort), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
