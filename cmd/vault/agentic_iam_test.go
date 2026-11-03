package vault

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"hal/internal/global"
	"hal/internal/integrations"
)

// ─── order of operations ─────────────────────────────────────────────────────

// recordingSteps records the enable and disable steps it is asked to run, and
// fails the ones listed in fail.
type recordingSteps struct {
	calls []string
	fail  map[string]bool
}

func (r *recordingSteps) step(name string) error {
	r.calls = append(r.calls, name)
	if r.fail[name] {
		return errors.New(name + " failed")
	}
	return nil
}

func (r *recordingSteps) checkPrerequisites() error { return r.step("prerequisites") }
func (r *recordingSteps) ensureAuthentik() error    { return r.step("authentik") }
func (r *recordingSteps) ensureMariaDB() error      { return r.step("mariadb") }
func (r *recordingSteps) configureAuthentik() error { return r.step("authentik-config") }
func (r *recordingSteps) configureVault() error     { return r.step("vault-config") }
func (r *recordingSteps) deployContainers(recreate bool) error {
	return r.step(fmt.Sprintf("containers(recreate=%v)", recreate))
}

func (r *recordingSteps) removeContainers() error       { return r.step("remove-containers") }
func (r *recordingSteps) teardownVault() error          { return r.step("teardown-vault") }
func (r *recordingSteps) releaseMariaDB() error         { return r.step("release-mariadb") }
func (r *recordingSteps) removeAuthentikObjects() error { return r.step("remove-authentik-objects") }
func (r *recordingSteps) releaseAuthentik() error       { return r.step("release-authentik") }

func TestEnableAgenticIAMRunsTheStepsInOrder(t *testing.T) {
	for _, recreate := range []bool{false, true} {
		r := &recordingSteps{}
		if err := enableAgenticIAM(r, recreate); err != nil {
			t.Fatalf("enableAgenticIAM: %v", err)
		}
		want := []string{"prerequisites", "authentik", "mariadb", "authentik-config", "vault-config", fmt.Sprintf("containers(recreate=%v)", recreate)}
		if !slices.Equal(r.calls, want) {
			t.Errorf("recreate=%v: steps %v, want %v", recreate, r.calls, want)
		}
	}
}

func TestEnableAgenticIAMRefusalChangesNothing(t *testing.T) {
	r := &recordingSteps{fail: map[string]bool{"prerequisites": true}}
	err := enableAgenticIAM(r, false)

	var stepErr *agenticIAMEnableError
	if !errors.As(err, &stepErr) {
		t.Fatalf("err = %v, want an *agenticIAMEnableError", err)
	}
	if stepErr.Changed || stepErr.Step != "prerequisites" {
		t.Errorf("err = %+v, want the prerequisites step and Changed=false", stepErr)
	}
	if !slices.Equal(r.calls, []string{"prerequisites"}) {
		t.Errorf("steps %v: a refused prerequisite must stop before any change", r.calls)
	}
}

func TestEnableAgenticIAMStopsAtTheFirstFailure(t *testing.T) {
	r := &recordingSteps{fail: map[string]bool{"mariadb": true}}
	err := enableAgenticIAM(r, false)

	var stepErr *agenticIAMEnableError
	if !errors.As(err, &stepErr) || !stepErr.Changed || stepErr.Step != "MariaDB" {
		t.Fatalf("err = %v, want a MariaDB step error with Changed=true", err)
	}
	if !slices.Equal(r.calls, []string{"prerequisites", "authentik", "mariadb"}) {
		t.Errorf("steps %v: nothing may run after a failed step", r.calls)
	}
}

func TestDisableAgenticIAMRunsEveryStepInReverseOrder(t *testing.T) {
	r := &recordingSteps{fail: map[string]bool{"remove-containers": true, "release-mariadb": true}}
	errs := disableAgenticIAM(r)

	want := []string{"remove-containers", "teardown-vault", "release-mariadb", "remove-authentik-objects", "release-authentik"}
	if !slices.Equal(r.calls, want) {
		t.Errorf("steps %v, want %v: disable carries on after a failure", r.calls, want)
	}
	if len(errs) != 2 {
		t.Fatalf("errs = %v, want the two failures", errs)
	}
	if !strings.HasPrefix(errs[0].Error(), "containers: ") || !strings.HasPrefix(errs[1].Error(), "MariaDB: ") {
		t.Errorf("errs = %v, want each failure named by its step", errs)
	}
}

// ─── shared services ─────────────────────────────────────────────────────────

func TestPlanAgenticIAMMariaDBRelease(t *testing.T) {
	self, db := global.VaultMariaDBConsumerAgenticIAM, global.VaultMariaDBConsumerDatabase
	cases := []struct {
		name      string
		consumers []string
		running   bool
		want      agenticIAMMariaDBRelease
	}{
		// Not a consumer: the container may belong to hal vault database alone,
		// even with an empty consumer list (started before counting).
		{"not a consumer, empty list", nil, true, agenticIAMMariaDBRelease{}},
		{"not a consumer", []string{db}, true, agenticIAMMariaDBRelease{}},
		{"last consumer", []string{self}, true, agenticIAMMariaDBRelease{Release: true}},
		{"shared, running", []string{db, self}, true, agenticIAMMariaDBRelease{Release: true, Cleanup: true}},
		{"shared, stopped", []string{self, db}, false, agenticIAMMariaDBRelease{Release: true, Stopped: true}},
	}
	for _, tc := range cases {
		if got := planAgenticIAMMariaDBRelease(tc.consumers, tc.running); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestAgenticIAMDeleteSharedPersonas(t *testing.T) {
	self := integrations.AgenticIAMAuthentikConsumer
	cases := []struct {
		consumers []string
		want      bool
	}{
		{[]string{self}, true},
		{[]string{self, oidcSharedServiceKey}, false},
		{[]string{global.AuthentikConsumerTFESAMLPrimary, self}, false},
		// Not registered: alice and bob may belong to whoever started Authentik.
		{nil, false},
		{[]string{oidcSharedServiceKey}, false},
	}
	for _, tc := range cases {
		if got := agenticIAMDeleteSharedPersonas(tc.consumers); got != tc.want {
			t.Errorf("agenticIAMDeleteSharedPersonas(%v) = %v, want %v", tc.consumers, got, tc.want)
		}
	}
}

func TestAgenticIAMFootprintEmpty(t *testing.T) {
	if !(agenticIAMFootprint{}).empty() {
		t.Error("a zero footprint must be empty")
	}
	for name, f := range map[string]agenticIAMFootprint{
		"container":          {Containers: []string{agenticIAMChatContainer}},
		"state dir":          {StateDir: true},
		"MariaDB consumer":   {MariaDBConsumer: true},
		"Authentik consumer": {AuthentikConsumer: true},
		"Vault object":       {VaultObjects: []string{agenticDBMount + "/"}},
		"Authentik app":      {AuthentikApps: []string{integrations.AgenticIAMChatSlug}},
	} {
		if f.empty() {
			t.Errorf("a footprint with a %s is not empty", name)
		}
	}
}

// ─── prerequisites ───────────────────────────────────────────────────────────

func TestAuthentikVersionFromImage(t *testing.T) {
	cases := []struct{ image, label, want string }{
		{"ghcr.io/goauthentik/server:2026.8.3", "2026.8.3", "2026.8.3"},
		{"ghcr.io/goauthentik/server:2026.5.6", "", "2026.5.6"},
		{"ghcr.io/goauthentik/server:latest", "2026.8.3", "2026.8.3"},
		{"registry.local:5000/goauthentik/server", "<no value>", ""},
		{"ghcr.io/goauthentik/server@sha256:abc", "2026.8.1", "2026.8.1"},
		{"ghcr.io/goauthentik/server:2026.8.3@sha256:abc", "", "2026.8.3"},
		{"", "", ""},
	}
	for _, tc := range cases {
		if got := authentikVersionFromImage(tc.image, tc.label); got != tc.want {
			t.Errorf("authentikVersionFromImage(%q, %q) = %q, want %q", tc.image, tc.label, got, tc.want)
		}
	}
}

func TestDefaultAuthentikTagIsRecentEnough(t *testing.T) {
	if !vaultVersionAtLeast(integrations.AuthentikDefaultTag, agenticIAMMinAuthentikVersion) {
		t.Errorf("AuthentikDefaultTag %s is older than %s: a fresh enable would start an Authentik the lab cannot use",
			integrations.AuthentikDefaultTag, agenticIAMMinAuthentikVersion)
	}
}

func TestAgenticIAMAuthentikTooOldErrorSaysHowToFixIt(t *testing.T) {
	err := agenticIAMAuthentikTooOldError("2026.5.6", []string{oidcSharedServiceKey, global.AuthentikConsumerTFESAMLTwin})
	if !errors.Is(err, errAgenticIAMAuthentikTooOld) {
		t.Fatalf("err = %v, want errAgenticIAMAuthentikTooOld", err)
	}
	msg := err.Error()
	for _, want := range []string{"2026.5.6", agenticIAMMinAuthentikVersion, "never restarts",
		"hal vault oidc disable", "hal tf saml disable --target twin", "hal vault agentic-iam enable"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not say %q:\n%s", want, msg)
		}
	}
}

func TestAgenticIAMVaultDownErrorSaysHowToFixIt(t *testing.T) {
	err := agenticIAMVaultDownError(errors.New("Vault is unreachable. Is it running?"))
	if !errors.Is(err, errAgenticIAMVaultDown) {
		t.Fatalf("err = %v, want errAgenticIAMVaultDown", err)
	}
	for _, want := range []string{"unreachable", "hal vault create --edition ent", "VAULT_LICENSE_PATH", "hal vault agentic-iam enable"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q:\n%v", want, err)
		}
	}
}

// ─── chat URL and port ───────────────────────────────────────────────────────

func TestAgenticIAMChatURLUsesTheReservedPort(t *testing.T) {
	u, err := url.Parse(agenticIAMChatPublicURL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "http" || !strings.HasSuffix(u.Hostname(), ".localhost") || u.Port() != fmt.Sprint(agenticIAMChatHostPort) {
		t.Errorf("chat URL %s: want http://<name>.localhost:%d", agenticIAMChatPublicURL, agenticIAMChatHostPort)
	}
	if got := agenticIAMAuthentikConfig().ChatPublicURL; got != agenticIAMChatPublicURL {
		t.Errorf("Authentik redirect base %s, want the chat URL %s", got, agenticIAMChatPublicURL)
	}
}

func TestAgenticIAMChatPortIsNotAKindMapping(t *testing.T) {
	path, err := writeHALKindConfig()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(path) }()
	cfg, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg), fmt.Sprintf("hostPort: %d\n", agenticIAMChatHostPort)) {
		t.Errorf("host port %d is both the chat's and a KinD mapping", agenticIAMChatHostPort)
	}
	if agenticIAMChatHostPort == dbVSOHostPort || agenticIAMChatHostPort == vaultHTTPPort {
		t.Errorf("host port %d is already taken by another Vault lab", agenticIAMChatHostPort)
	}
}

// ─── containers and env files ────────────────────────────────────────────────

func testAgenticIAMAuthentik() *integrations.AgenticIAMAuthentik {
	app := func(slug string) integrations.AgenticIAMAuthentikApp {
		return integrations.AgenticIAMAuthentikApp{Slug: slug, AuthentikOAuth2Client: integrations.AuthentikOAuth2Client{
			ClientID: slug + "-id", ClientSecret: slug + "-SECRET",
		}}
	}
	return &integrations.AgenticIAMAuthentik{
		Chat:               app(integrations.AgenticIAMChatSlug),
		DemoAgent:          app(integrations.AgenticIAMDemoAgentSlug),
		VaultAgentic:       app(integrations.AgenticIAMVaultSlug),
		ActorUsername:      integrations.AgenticIAMActorUsername,
		ActorAppPassword:   "actor-APP-PASSWORD",
		TokenEndpoint:      integrations.AuthentikOAuth2TokenEndpoint(),
		ChatIssuer:         integrations.AuthentikOIDCIssuer(integrations.AgenticIAMChatSlug),
		VaultAgenticIssuer: integrations.AuthentikOIDCIssuer(integrations.AgenticIAMVaultSlug),
	}
}

func envMap(env []agenticIAMEnvVar) map[string]string {
	m := map[string]string{}
	for _, v := range env {
		m[v.Key] = v.Value
	}
	return m
}

func containersByCommand(t *testing.T, containers []agenticIAMContainer) (chat, agent agenticIAMContainer) {
	t.Helper()
	if len(containers) != 2 || containers[0].Command != "agent" || containers[1].Command != "chat" {
		t.Fatalf("containers %+v: want the demo agent, then the chat", containers)
	}
	return containers[1], containers[0]
}

func TestAgenticIAMContainersEnv(t *testing.T) {
	addr, ca := agenticIAMVaultAddr(false, "")
	chat, agent := containersByCommand(t, agenticIAMContainers(testAgenticIAMAuthentik(), "/state", addr, ca))

	wantChat := map[string]string{
		"LISTEN_PORT":        "8080",
		"PUBLIC_URL":         "http://agentic.localhost:8092",
		"OIDC_ISSUER":        "http://authentik.localhost:9100/application/o/hal-chat/",
		"OIDC_CLIENT_ID":     "hal-chat-id",
		"OIDC_CLIENT_SECRET": "hal-chat-SECRET",
		"AGENT_URL":          "http://hal-agentic-iam-agent:8081",
	}
	if got := envMap(chat.Env); !maps.Equal(got, wantChat) {
		t.Errorf("chat env:\n%v\nwant:\n%v", got, wantChat)
	}

	wantAgent := map[string]string{
		"LISTEN_PORT":               "8081",
		"TOKEN_ENDPOINT":            "http://authentik.localhost:9100/application/o/token/",
		"EXCHANGE_CLIENT_ID":        "vault-agentic-id",
		"EXCHANGE_CLIENT_SECRET":    "vault-agentic-SECRET",
		"ACTOR_CLIENT_ID":           "hal-demo-agent-id",
		"ACTOR_CLIENT_SECRET":       "hal-demo-agent-SECRET",
		"ACTOR_USERNAME":            "finance-agent",
		"ACTOR_APP_PASSWORD":        "actor-APP-PASSWORD",
		"VAULT_ADDR":                "http://hal-vault:8200",
		"DB_MOUNT":                  "agentic-db",
		"DB_ROLE_QUARTERLY_RESULTS": "quarterly-results",
		"DB_ROLE_FORECASTS":         "forecasts",
		"DB_ROLE_PAYROLL":           "payroll",
		"DB_HOST":                   "hal-vault-mariadb",
		"DB_PORT":                   "3306",
		"DB_NAME":                   "acme",
	}
	if got := envMap(agent.Env); !maps.Equal(got, wantAgent) {
		t.Errorf("agent env:\n%v\nwant:\n%v", got, wantAgent)
	}

	if chat.EnvFile != filepath.Join("/state", "chat.env") || agent.EnvFile != filepath.Join("/state", "agent.env") {
		t.Errorf("env files %s, %s: want one per container under the state dir", chat.EnvFile, agent.EnvFile)
	}
	if len(agent.Volumes) != 0 {
		t.Errorf("agent volumes %v: a dev Vault needs no CA", agent.Volumes)
	}
}

func TestAgenticIAMContainersTrustTheProdVaultCA(t *testing.T) {
	addr, ca := agenticIAMVaultAddr(true, "/home/u/.hal/vault-prod/certs/cert.pem")
	_, agent := containersByCommand(t, agenticIAMContainers(testAgenticIAMAuthentik(), "/state", addr, ca))
	env := envMap(agent.Env)
	if env["VAULT_ADDR"] != "https://hal-vault:8200" || env["SSL_CERT_FILE"] != agenticIAMVaultCAPath {
		t.Errorf("prod agent env: VAULT_ADDR=%q SSL_CERT_FILE=%q", env["VAULT_ADDR"], env["SSL_CERT_FILE"])
	}
	if want := []string{"/home/u/.hal/vault-prod/certs/cert.pem:" + agenticIAMVaultCAPath + ":ro"}; !slices.Equal(agent.Volumes, want) {
		t.Errorf("prod agent volumes %v, want %v", agent.Volumes, want)
	}
}

// The env-var contract lives in internal/agenticiam/app/README.md: every
// variable HAL sets is documented there for that command, and every required
// one is set.
func TestAgenticIAMContainersFollowTheImageContract(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("..", "..", "internal", "agenticiam", "app", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	documented := map[string]map[string]bool{} // command -> variable -> required
	command := ""
	section := regexp.MustCompile("^### `(chat|agent)`")
	row := regexp.MustCompile("^\\| `([A-Z_]+)` \\| (yes)? *\\|")
	for _, line := range strings.Split(string(readme), "\n") {
		if m := section.FindStringSubmatch(line); m != nil {
			command = m[1]
			documented[command] = map[string]bool{}
			continue
		}
		if strings.HasPrefix(line, "## ") {
			command = ""
		}
		if m := row.FindStringSubmatch(line); m != nil && command != "" {
			documented[command][m[1]] = m[2] == "yes"
		}
	}
	if len(documented["chat"]) == 0 || len(documented["agent"]) == 0 {
		t.Fatalf("could not read the contract tables of the README: %v", documented)
	}

	addr, ca := agenticIAMVaultAddr(true, "/ca.pem") // prod sets every variable
	for _, c := range agenticIAMContainers(testAgenticIAMAuthentik(), "/state", addr, ca) {
		env := envMap(c.Env)
		for key := range env {
			if _, ok := documented[c.Command][key]; !ok {
				t.Errorf("%s: %s is not in the README contract", c.Command, key)
			}
		}
		for key, required := range documented[c.Command] {
			if _, set := env[key]; required && !set {
				t.Errorf("%s: required %s is not set", c.Command, key)
			}
		}
	}
}

func TestAgenticIAMContainersKeepEachSecretToItsContainer(t *testing.T) {
	addr, ca := agenticIAMVaultAddr(false, "")
	chat, agent := containersByCommand(t, agenticIAMContainers(testAgenticIAMAuthentik(), "/state", addr, ca))
	chatFile, err := chat.envFile()
	if err != nil {
		t.Fatal(err)
	}
	agentFile, err := agent.envFile()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"vault-agentic-SECRET", "hal-demo-agent-SECRET", "actor-APP-PASSWORD"} {
		if strings.Contains(chatFile, s) {
			t.Errorf("the chat's env file holds the demo agent's secret %s", s)
		}
	}
	if strings.Contains(agentFile, "hal-chat-SECRET") {
		t.Error("the demo agent's env file holds the chat's client secret")
	}
}

func TestAgenticIAMRunArgs(t *testing.T) {
	addr, ca := agenticIAMVaultAddr(false, "")
	chat, agent := containersByCommand(t, agenticIAMContainers(testAgenticIAMAuthentik(), "/state", addr, ca))
	image := "localhost/hal-agentic-iam:0123456789ab"

	chatArgs := chat.runArgs(image, "cafe")
	agentArgs := agent.runArgs(image, "cafe")
	for name, args := range map[string][]string{"chat": chatArgs, "agent": agentArgs} {
		joined := " " + strings.Join(args, " ") + " "
		for _, want := range []string{" --network hal-net ", " --env-file /state/" + name + ".env ", " --label hal.agentic-iam.config=cafe "} {
			if !strings.Contains(joined, want) {
				t.Errorf("%s run args %v lack %q", name, args, strings.TrimSpace(want))
			}
		}
		if !slices.Equal(args[len(args)-2:], []string{image, name}) {
			t.Errorf("%s run args %v: want the image then the command last", name, args)
		}
		for _, secret := range []string{"SECRET", "APP-PASSWORD", " -e "} {
			if strings.Contains(joined, secret) {
				t.Errorf("%s run args carry %q on the command line: %v", name, secret, args)
			}
		}
	}
	if !strings.Contains(strings.Join(chatArgs, " "), "-p 8092:8080") {
		t.Errorf("chat run args %v: want it published on 8092", chatArgs)
	}
	if slices.Contains(agentArgs, "-p") {
		t.Errorf("agent run args %v: the demo agent must stay on hal-net only", agentArgs)
	}
	if slices.Contains(chat.runArgs(image, ""), "--label") {
		t.Error("an empty config label must not be passed")
	}
}

func TestAgenticIAMEnvFile(t *testing.T) {
	c := agenticIAMContainer{Name: "x", Env: []agenticIAMEnvVar{{"A", "1"}, {"B", "two=2"}}}
	got, err := c.envFile()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if !strings.HasPrefix(lines[0], "#") || !slices.Equal(lines[1:], []string{"A=1", "B=two=2"}) {
		t.Errorf("env file:\n%s", got)
	}

	for name, v := range map[string]string{"empty": " ", "line break": "a\nB=b", "carriage return": "a\rb"} {
		bad := agenticIAMContainer{Name: "x", Env: []agenticIAMEnvVar{{"A", v}}}
		if _, err := bad.envFile(); err == nil {
			t.Errorf("%s value accepted", name)
		}
	}
}

func TestWriteAgenticIAMEnvFileIsPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agentic-iam")
	path := filepath.Join(dir, "chat.env")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeAgenticIAMEnvFile(path, "A=1\n"); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]os.FileMode{dir: 0o700, path: 0o600} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s has mode %o, want %o", p, info.Mode().Perm(), want)
		}
	}
	if body, _ := os.ReadFile(path); string(body) != "A=1\n" {
		t.Errorf("content %q", body)
	}
}

func TestAgenticIAMConfigHash(t *testing.T) {
	c := agenticIAMContainer{Name: "x", Command: "chat", EnvFile: "/s/chat.env", Port: 8080, HostPort: 8092}
	base := c.configHash("img:1", "A=1\n")
	if base != c.configHash("img:1", "A=1\n") {
		t.Error("the same configuration must hash the same")
	}
	other := c
	other.HostPort = 8093
	for name, h := range map[string]string{
		"image":    c.configHash("img:2", "A=1\n"),
		"env file": c.configHash("img:1", "A=2\n"),
		"run args": other.configHash("img:1", "A=1\n"),
	} {
		if h == base {
			t.Errorf("a different %s must change the hash", name)
		}
	}
}
