package vault

// agentic_iam.go is `hal vault agentic-iam` (alias agentic), the Agentic IAM
// lab of ADR 0004. It owns the order in which the lab's building blocks run:
//
//  1. the prerequisites, all checked before anything changes: Vault Enterprise
//     with Agentic IAM (agentic_iam_prereq.go), a recent enough Authentik, and
//     a free host port for the chat;
//  2. the shared Authentik, started if absent (internal/integrations);
//  3. the shared hal-vault-mariadb and the lab's seed (database-mariadb.go,
//     agentic_iam_seed.go);
//  4. the lab's Authentik objects (authentik_agentic_iam.go);
//  5. the Vault side (agentic_iam_vault.go);
//  6. the image and the two containers (internal/agenticiam,
//     agentic_iam_containers.go).
//
// disable undoes them in reverse order, carries on after a failure, and
// reports every failure at the end. The image stays: it is a build cache.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"hal/internal/agenticiam"
	"hal/internal/global"
	"hal/internal/integrations"
	"hal/internal/ui"

	vault "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
)

var (
	agenticIAMEnable         bool
	agenticIAMDisable        bool
	agenticIAMUpdate         bool
	agenticIAMAuthentikImage string
	agenticIAMAuthentikTag   string
	agenticIAMMariaDBImage   string
	agenticIAMMariaDBTag     string
)

// agenticIAMMinAuthentikVersion is the first Authentik release whose token
// exchange names the actor in an act claim (ADR 0004, decision 3).
const agenticIAMMinAuthentikVersion = "2026.8.0"

var (
	errAgenticIAMVaultDown       = errors.New("vault is not reachable")
	errAgenticIAMAuthentikTooOld = errors.New("authentik is older than " + agenticIAMMinAuthentikVersion)
	errAgenticIAMPortInUse       = errors.New("the chat's host port is in use")
)

// agenticIAMCase is one case of the scenario (ADR 0004, decision 11).
type agenticIAMCase struct {
	Persona, Prompt, Outcome string
}

var agenticIAMScenario = []agenticIAMCase{
	{"alice", "Q3 results", "✅ results: every decision point allows"},
	{"charlie", "Q3 results", "❌ the IdP: the demo agent may not act for charlie"},
	{"bob", "Q3 results", "❌ the persona's own rights"},
	{"alice", "payroll", "❌ the ceiling"},
	{"alice", "Q2 results", "✅ results, ❌ forecasts: the poisoned row asks for them, outside the task scope"},
	{"alice", "Q3 results and forecasts", "✅ results, ✅ forecasts: both are in the task scope"},
}

var vaultAgenticIAMCmd = &cobra.Command{
	Use:     "agentic-iam [status|enable|disable|update]",
	Aliases: []string{"agentic"},
	Short:   "[Vault Enterprise] Deploy the Agentic IAM lab: a demo agent acting on behalf of personas",
	Long: `Deploy the Agentic IAM lab (ADR 0004). A persona logs in to a chat through
Authentik. The demo agent acts on their behalf with an OBO token obtained by
token exchange, and the IdP and Vault Enterprise decide each access it makes
to the lab's data in MariaDB.

Prerequisites (enable refuses, and changes nothing, without them):
  - Vault Enterprise 2.1.0 or later, licensed with the Agentic IAM terms
  - Authentik 2026.8.0 or later, if it is already running
  - host port 8092 free for the chat

Authentik and hal-vault-mariadb are shared with other labs: enable reuses them
when they run, and disable stops them only when no other lab uses them.

Actions:
  status   Show the containers, Vault, Authentik and MariaDB state of the lab (default)
  enable   Check the prerequisites, then deploy Authentik, MariaDB, Vault and both containers
  update   Re-apply the whole lab and recreate both containers (picks up a new image)
  disable  Remove the lab; keep its image, a local build cache

Chat: http://agentic.localhost:8092

Personas (password: password):
  alice    finance      may delegate; reads results, forecasts and payroll
  bob      engineering  may delegate; no rights on the lab's data
  charlie  sales        may not delegate to the demo agent

Authentik admin: run 'hal creds status' or 'hal vault agentic-iam status'.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := parseLifecycleAction(args, &agenticIAMEnable, &agenticIAMDisable, &agenticIAMUpdate); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		client, vaultErr := GetHealthyClient()
		lab := &agenticIAMLab{
			engine:         engine,
			client:         client,
			vaultErr:       vaultErr,
			prod:           vaultProdActive(),
			authentikImage: agenticIAMAuthentikImage,
			authentikTag:   agenticIAMAuthentikTag,
			mariadbImage:   fmt.Sprintf("%s:%s", agenticIAMMariaDBImage, agenticIAMMariaDBTag),
		}

		switch {
		case agenticIAMDisable:
			runAgenticIAMDisable(lab)
		case agenticIAMEnable || agenticIAMUpdate:
			runAgenticIAMEnable(lab, agenticIAMUpdate)
		default:
			runAgenticIAMStatus(lab)
		}
	},
}

// agenticIAMLab is one run of the command: the engine, Vault, the image
// overrides, and what the enable steps hand on to the next ones.
type agenticIAMLab struct {
	engine   string
	client   *vault.Client
	vaultErr error
	prod     bool

	authentikImage string
	authentikTag   string
	mariadbImage   string // image:tag, used only if hal-vault-mariadb is started fresh

	akClient *integrations.AuthentikClient
	ak       *integrations.AgenticIAMAuthentik
}

// ─── enable ───────────────────────────────────────────────────────────────────

// agenticIAMEnableSteps is the enable sequence. It is an interface so that
// tests can check the order and the stop-at-first-failure rule without an
// engine, Vault or Authentik.
type agenticIAMEnableSteps interface {
	checkPrerequisites() error
	ensureAuthentik() error
	ensureMariaDB() error
	configureAuthentik() error
	configureVault() error
	deployContainers(recreate bool) error
}

// agenticIAMEnableError is an enable that stopped at one step. Changed is
// false when it stopped at the prerequisites: then nothing was changed.
type agenticIAMEnableError struct {
	Step    string
	Changed bool
	Err     error
}

func (e *agenticIAMEnableError) Error() string { return e.Step + ": " + e.Err.Error() }
func (e *agenticIAMEnableError) Unwrap() error { return e.Err }

// enableAgenticIAM checks every prerequisite, then runs the steps in order and
// stops at the first failure. Every step converges when re-run, so a failed
// enable resumes when run again. recreate (update) recreates both containers
// even when they already run the same configuration.
func enableAgenticIAM(s agenticIAMEnableSteps, recreate bool) error {
	if err := s.checkPrerequisites(); err != nil {
		return &agenticIAMEnableError{Step: "prerequisites", Err: err}
	}
	steps := []struct {
		name string
		run  func() error
	}{
		{"Authentik", s.ensureAuthentik},
		{"MariaDB", s.ensureMariaDB},
		{"Authentik configuration", s.configureAuthentik},
		{"Vault configuration", s.configureVault},
		{"containers", func() error { return s.deployContainers(recreate) }},
	}
	for _, step := range steps {
		if err := step.run(); err != nil {
			return &agenticIAMEnableError{Step: step.name, Changed: true, Err: err}
		}
	}
	return nil
}

func runAgenticIAMEnable(lab *agenticIAMLab, update bool) {
	if global.DryRun {
		printAgenticIAMEnablePlan(lab, update)
		return
	}

	if update {
		ui.Title("hal vault agentic-iam update — re-applying the Agentic IAM lab")
	} else {
		ui.Title("hal vault agentic-iam — deploying the Agentic IAM lab")
	}

	err := enableAgenticIAM(lab, update)
	var stepErr *agenticIAMEnableError
	if errors.As(err, &stepErr) {
		if !stepErr.Changed {
			printAgenticIAMRefusals(stepErr.Err)
			fmt.Println("   Nothing was changed.")
			return
		}
		fmt.Printf("❌ %s failed: %v\n", stepErr.Step, stepErr.Err)
		fmt.Println("   The lab is partly deployed. Fix the cause and run 'hal vault agentic-iam enable' again:")
		fmt.Println("   it resumes where it stopped. Or remove it with 'hal vault agentic-iam disable'.")
		global.RefreshHalHealth(lab.engine)
		return
	}

	global.RefreshHalHealth(lab.engine)
	printAgenticIAMSuccess()
}

// checkPrerequisites runs every prerequisite check, even after a refusal, so
// that one run reports everything to fix. They only read.
func (l *agenticIAMLab) checkPrerequisites() error {
	ui.Step("Checking prerequisites")
	var errs []error
	if l.vaultErr != nil || l.client == nil {
		errs = append(errs, agenticIAMVaultDownError(l.vaultErr))
	} else if err := checkAgenticIAMPrerequisites(l.client, l.prod); err != nil {
		errs = append(errs, err)
	}
	if err := checkAgenticIAMAuthentik(l.engine, l.authentikTag); err != nil {
		errs = append(errs, err)
	}
	if err := checkAgenticIAMChatPort(l.engine); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// printAgenticIAMRefusals prints each refused prerequisite on its own.
func printAgenticIAMRefusals(err error) {
	errs := []error{err}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		errs = joined.Unwrap()
	}
	for _, e := range errs {
		fmt.Printf("❌ %v\n", e)
	}
}

func (l *agenticIAMLab) ensureAuthentik() error {
	secrets, err := integrations.LoadOrCreateAuthentikSecrets()
	if err != nil {
		return err
	}
	// Registered before the start, so that a disable after a failed start still
	// stops a stack no other lab uses.
	if err := global.AddSharedServiceConsumer(integrations.AuthentikSharedServiceKey, integrations.AgenticIAMAuthentikConsumer); err != nil {
		ui.Step("⚠️  Could not register the shared service consumer: %v", err)
	}

	firstBoot := false
	if integrations.IsAuthentikRunning(l.engine) {
		ui.Step("Authentik already running — reusing it")
	} else {
		firstBoot = true
		if err := integrations.StartAuthentikStack(l.engine, l.authentikImage, l.authentikTag, secrets); err != nil {
			return err
		}
		if err := integrations.WaitAuthentikHealthy(); err != nil {
			return err
		}
	}
	if err := integrations.WaitAuthentikTokenReady(secrets.BootstrapToken); err != nil {
		return err
	}
	if firstBoot {
		// Authentik seeds its default scope mappings and flows after the token
		// becomes usable on a first boot.
		if err := integrations.WaitAuthentikScopesReady(secrets.BootstrapToken); err != nil {
			return err
		}
		if err := integrations.WaitAuthentikFlowsReady(secrets.BootstrapToken); err != nil {
			return err
		}
	}
	l.akClient = integrations.NewAuthentikClient(secrets.BootstrapToken)
	return nil
}

// ensureMariaDB starts or reuses the shared hal-vault-mariadb, then seeds the
// acme schema and the lab's own broker as root. Vault never connects as root:
// configureVault gives it the broker, then rotates the broker's password.
func (l *agenticIAMLab) ensureMariaDB() error {
	if _, err := ensureVaultMariaDB(l.engine, l.mariadbImage, global.VaultMariaDBConsumerAgenticIAM); err != nil {
		return err
	}
	sql, err := agenticIAMSeedSQL(agenticIAMDefaultBrokerUser, agenticIAMDefaultBrokerPassword)
	if err != nil {
		return err
	}
	ui.Step("Seeding the %s schema and the %s broker", agenticIAMSchema, agenticIAMDefaultBrokerUser)
	if err := runVaultMariaDBSQL(l.engine, sql); err != nil {
		return fmt.Errorf("seed the %s schema: %w", agenticIAMSchema, err)
	}
	return nil
}

// agenticIAMAuthentikConfig ties the Authentik side to the chat's URL and the
// lab's Vault database roles.
func agenticIAMAuthentikConfig() integrations.AgenticIAMAuthentikConfig {
	return integrations.AgenticIAMAuthentikConfig{
		ChatPublicURL:      agenticIAMChatPublicURL,
		DBMount:            agenticDBMount,
		FinanceReportsRole: agenticDBQuarterlyResults.Name,
		ForecastsRole:      agenticDBForecasts.Name,
		PayrollRole:        agenticDBPayroll.Name,
	}
}

func (l *agenticIAMLab) configureAuthentik() error {
	ui.Step("Configuring Authentik: personas, %s, %s, %s and the actor %s",
		integrations.AgenticIAMChatSlug, integrations.AgenticIAMDemoAgentSlug, integrations.AgenticIAMVaultSlug, integrations.AgenticIAMActorUsername)
	ak, err := integrations.EnsureAgenticIAMAuthentik(l.engine, l.akClient, agenticIAMAuthentikConfig())
	if err != nil {
		return err
	}
	l.ak = ak
	return nil
}

func (l *agenticIAMLab) configureVault() error {
	ui.Step("Checking that Vault reaches the %s issuer", integrations.AgenticIAMVaultSlug)
	if err := integrations.WaitVaultVisibleAuthentik(l.engine, l.ak.VaultAgenticIssuer); err != nil {
		return err
	}
	ui.Step("Configuring Vault: OAuth resource server profile, personas, Agent Registry record, %s/", agenticDBMount)
	_, err := configureAgenticIAMVault(l.client, agenticIAMVaultConfig{
		Issuer:         l.ak.VaultAgenticIssuer,
		ClientID:       l.ak.VaultAgentic.ClientID,
		BrokerUser:     agenticIAMDefaultBrokerUser,
		BrokerPassword: agenticIAMDefaultBrokerPassword,
	})
	return err
}

func (l *agenticIAMLab) deployContainers(recreate bool) error {
	image := agenticiam.ImageRef()
	ui.Step("Preparing image %s (built from the sources embedded in hal on first use)", image)
	if err := agenticiam.EnsureImage(l.engine); err != nil {
		return err
	}
	dir, err := agenticIAMStateDir()
	if err != nil {
		return err
	}
	addr, caCert := agenticIAMVaultAddr(l.prod, vaultProdCertPath())
	if err := startAgenticIAMContainers(l.engine, image, agenticIAMContainers(l.ak, dir, addr, caCert), recreate, ui.Step); err != nil {
		return err
	}
	removeStaleAgenticIAMImages(l.engine)
	return nil
}

// removeStaleAgenticIAMImages removes the images built from earlier sources,
// now that both containers run the current one. A failure is only a warning:
// the lab is up, and the next enable tries again.
func removeStaleAgenticIAMImages(engine string) {
	removed, err := agenticiam.RemoveStaleImages(engine)
	for _, ref := range removed {
		ui.Step("Removed %s, built from earlier sources", ref)
	}
	if err != nil {
		ui.Step("⚠️  Could not remove the images built from earlier sources: %v", err)
	}
}

// agenticIAMVaultDownError is the refusal when Vault cannot be reached.
func agenticIAMVaultDownError(err error) error {
	reason := "Vault is not reachable"
	if err != nil {
		reason = err.Error()
	}
	return &agenticIAMPrereqError{
		kind:    errAgenticIAMVaultDown,
		problem: "The Agentic IAM lab needs a running Vault Enterprise: " + reason,
		fixes: []string{
			"Deploy Vault Enterprise with a license that includes the Agentic IAM terms:",
			"export VAULT_LICENSE_PATH=/path/to/vault.hclic   (unset VAULT_LICENSE: it takes precedence)",
			"hal vault create --edition ent",
			"hal vault agentic-iam enable",
		},
	}
}

// checkAgenticIAMAuthentik refuses an Authentik that cannot issue the OBO
// token. A running Authentik is shared, and HAL never restarts it under other
// labs: an old one must be recreated by the user. A missing one is started by
// enable with tag, which must then be recent enough.
func checkAgenticIAMAuthentik(engine, tag string) error {
	if !integrations.IsAuthentikRunning(engine) {
		if _, ok := parseVaultVersion(tag); ok && !vaultVersionAtLeast(tag, agenticIAMMinAuthentikVersion) {
			return &agenticIAMPrereqError{
				kind: errAgenticIAMAuthentikTooOld,
				problem: fmt.Sprintf("--authentik-tag %s is older than %s, the first Authentik whose token exchange names the actor.",
					tag, agenticIAMMinAuthentikVersion),
				fixes: []string{fmt.Sprintf("hal vault agentic-iam enable --authentik-tag %s   (or leave --authentik-tag out)", integrations.AuthentikDefaultTag)},
			}
		}
		return nil
	}

	version := runningAuthentikVersion(engine)
	if version == "" {
		ui.Step("⚠️  Could not read the running Authentik's version: the lab needs %s or later", agenticIAMMinAuthentikVersion)
		return nil
	}
	if vaultVersionAtLeast(version, agenticIAMMinAuthentikVersion) {
		return nil
	}
	return agenticIAMAuthentikTooOldError(version,
		agenticIAMOthers(global.GetSharedServiceConsumers(integrations.AuthentikSharedServiceKey), integrations.AgenticIAMAuthentikConsumer))
}

// agenticIAMAuthentikTooOldError is the refusal for a running Authentik older
// than agenticIAMMinAuthentikVersion. consumers are the other labs using it.
func agenticIAMAuthentikTooOldError(version string, consumers []string) error {
	users := "no registered lab"
	if len(consumers) > 0 {
		users = strings.Join(consumers, ", ")
	}
	fixes := []string{"Recreate Authentik on a recent version. Disable every lab that uses it:"}
	for _, c := range consumers {
		fixes = append(fixes, authentikConsumerDisableCommand(c))
	}
	fixes = append(fixes,
		"hal vault agentic-iam enable   (then starts Authentik "+integrations.AuthentikDefaultTag+")",
		"then enable the other labs again")
	return &agenticIAMPrereqError{
		kind: errAgenticIAMAuthentikTooOld,
		problem: fmt.Sprintf("The running Authentik is %s, and the lab needs %s or later. It is shared (in use by: %s), and HAL never restarts it under other labs.",
			version, agenticIAMMinAuthentikVersion, users),
		fixes: fixes,
	}
}

// authentikConsumerDisableCommand returns the command that removes a consumer
// of the shared Authentik.
func authentikConsumerDisableCommand(consumer string) string {
	switch consumer {
	case oidcSharedServiceKey:
		return "hal vault oidc disable"
	case global.AuthentikConsumerTFESAMLPrimary:
		return "hal tf saml disable"
	case global.AuthentikConsumerTFESAMLTwin:
		return "hal tf saml disable --target twin"
	default:
		return "the disable command of " + consumer
	}
}

// runningAuthentikVersion returns the version of the running Authentik
// server, or "" if it cannot be read.
func runningAuthentikVersion(engine string) string {
	out, err := exec.Command(engine, "inspect", "-f",
		`{{.Config.Image}}|{{index .Config.Labels "org.opencontainers.image.version"}}`,
		integrations.AuthentikServerContainer).Output()
	if err != nil {
		return ""
	}
	image, label, _ := strings.Cut(strings.TrimSpace(string(out)), "|")
	return authentikVersionFromImage(image, label)
}

// authentikVersionFromImage returns an Authentik version from a container's
// image reference (ghcr.io/goauthentik/server:2026.8.3) when its tag is a
// version, else from the image's OCI version label, else "".
func authentikVersionFromImage(image, label string) string {
	ref, _, _ := strings.Cut(strings.TrimSpace(image), "@")
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		if tag := ref[i+1:]; isVersionString(tag) {
			return tag
		}
	}
	if label = strings.TrimSpace(label); isVersionString(label) {
		return label
	}
	return ""
}

// isVersionString reports a major.minor[.patch] version, as parseVaultVersion
// reads it.
func isVersionString(s string) bool {
	_, ok := parseVaultVersion(s)
	return ok
}

// checkAgenticIAMChatPort refuses a chat host port that another program
// holds. A port held by the lab's own chat is fine: enable may recreate it.
func checkAgenticIAMChatPort(engine string) error {
	if agenticIAMContainerExists(engine, agenticIAMChatContainer) || !agenticIAMHostPortInUse() {
		return nil
	}
	return &agenticIAMPrereqError{
		kind:    errAgenticIAMPortInUse,
		problem: fmt.Sprintf("Host port %d, where the chat is published, is already in use.", agenticIAMChatHostPort),
		fixes: []string{
			fmt.Sprintf("Find the program with: lsof -nP -iTCP:%d -sTCP:LISTEN", agenticIAMChatHostPort),
			"Stop it, then: hal vault agentic-iam enable",
		},
	}
}

// printAgenticIAMEnablePlan is the dry run of enable and update. The
// prerequisites only read, so they run for real and their verdict is shown.
func printAgenticIAMEnablePlan(lab *agenticIAMLab, update bool) {
	verb := "enable"
	if update {
		verb = "update"
	}
	fmt.Printf("[DRY RUN] hal vault agentic-iam %s\n", verb)
	if err := lab.checkPrerequisites(); err != nil {
		fmt.Println("[DRY RUN] Prerequisites:")
		printAgenticIAMRefusals(err)
		fmt.Println("[DRY RUN] A real run would stop here and change nothing. Plan once they pass:")
	} else {
		fmt.Println("[DRY RUN] Prerequisites: ✅ Vault Enterprise with Agentic IAM, Authentik version, chat port")
	}

	if integrations.IsAuthentikRunning(lab.engine) {
		fmt.Printf("[DRY RUN] Would reuse the running Authentik and register %s as its consumer\n", integrations.AgenticIAMAuthentikConsumer)
	} else {
		fmt.Printf("[DRY RUN] Would start the shared Authentik %s:%s and register %s as its consumer\n",
			lab.authentikImage, lab.authentikTag, integrations.AgenticIAMAuthentikConsumer)
	}
	if global.IsContainerRunning(lab.engine, vaultMariaDBContainer) {
		fmt.Printf("[DRY RUN] Would reuse the running %s and register %s as its consumer\n", vaultMariaDBContainer, global.VaultMariaDBConsumerAgenticIAM)
	} else {
		fmt.Printf("[DRY RUN] Would start the shared %s from %s and register %s as its consumer\n",
			vaultMariaDBContainer, lab.mariadbImage, global.VaultMariaDBConsumerAgenticIAM)
	}
	fmt.Printf("[DRY RUN] Would seed, as root, the %s schema (quarterly_results, forecasts, payroll) and the lab's broker %s\n",
		agenticIAMSchema, agenticIAMDefaultBrokerUser)
	fmt.Printf("[DRY RUN] Would configure Authentik: personas alice, bob, charlie; applications %s, %s, %s; actor %s; delegation bound to finance and engineering\n",
		integrations.AgenticIAMChatSlug, integrations.AgenticIAMDemoAgentSlug, integrations.AgenticIAMVaultSlug, integrations.AgenticIAMActorUsername)
	fmt.Printf("[DRY RUN] Would configure Vault: profile %s, entities %s and %s, groups, policies, Agent Registry record %s, %s/ with 3 roles (broker password rotated)\n",
		agenticIAMProfileName, agenticIAMPersonas[0].EntityName(), agenticIAMPersonas[1].EntityName(), agenticIAMAgentName, agenticDBMount)
	fmt.Printf("[DRY RUN] Would build %s if it is not in the local image store\n", agenticiam.ImageRef())
	fmt.Printf("[DRY RUN] Would remove the images of %s built from earlier sources, once both containers run the current one\n", agenticiam.ImageRepository)
	if update {
		fmt.Printf("[DRY RUN] Would recreate %s (hal-net only) and %s (published on %d)\n", agenticIAMAgentContainer, agenticIAMChatContainer, agenticIAMChatHostPort)
	} else {
		fmt.Printf("[DRY RUN] Would run %s (hal-net only) and %s (published on %d), keeping any that already run this configuration\n",
			agenticIAMAgentContainer, agenticIAMChatContainer, agenticIAMChatHostPort)
	}
	if dir, err := agenticIAMStateDir(); err == nil {
		fmt.Printf("[DRY RUN] Would pass their secrets in %s/{agent,chat}.env (mode 0600), not on the command line\n", dir)
	}
	fmt.Printf("[DRY RUN] Chat: %s\n", agenticIAMChatPublicURL)
}

func printAgenticIAMSuccess() {
	ui.Success("Agentic IAM lab ready!")

	ui.Section("Open the chat")
	ui.Field("URL", agenticIAMChatPublicURL)
	ui.Field("Login", "through Authentik ("+integrations.AuthentikAdminURL()+")")

	ui.Section("Personas (password: %s)", integrations.AuthentikPersonaPassword)
	ui.Item("alice   / %s  →  finance: may delegate; reads results, forecasts and payroll", integrations.AuthentikPersonaPassword)
	ui.Item("bob     / %s  →  engineering: may delegate; no rights on the lab's data", integrations.AuthentikPersonaPassword)
	ui.Item("charlie / %s  →  sales: may not delegate to the demo agent", integrations.AuthentikPersonaPassword)
	ui.Item("(alice and bob are shared with hal vault oidc and hal tf saml, with the same password)")

	ui.Section("The six cases: who decides")
	for i, c := range agenticIAMScenario {
		ui.Item("%d. %-8s %-28s →  %s", i+1, c.Persona, fmt.Sprintf("%q", c.Prompt), c.Outcome)
	}

	ui.Hint("Authentik admin credentials: hal creds status  (or: hal vault agentic-iam status)")
}

// ─── disable ──────────────────────────────────────────────────────────────────

// agenticIAMDisableSteps is the disable sequence, in reverse order of enable.
type agenticIAMDisableSteps interface {
	removeContainers() error
	teardownVault() error
	releaseMariaDB() error
	removeAuthentikObjects() error
	releaseAuthentik() error
}

// disableAgenticIAM runs every disable step, even after a failure, so that a
// partly deployed lab is cleaned up too. It returns every failure.
func disableAgenticIAM(s agenticIAMDisableSteps) []error {
	steps := []struct {
		name string
		run  func() error
	}{
		{"containers", s.removeContainers},
		{"Vault", s.teardownVault},
		{"MariaDB", s.releaseMariaDB},
		{"Authentik objects", s.removeAuthentikObjects},
		{"Authentik", s.releaseAuthentik},
	}
	var errs []error
	for _, step := range steps {
		if err := step.run(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", step.name, err))
		}
	}
	return errs
}

func runAgenticIAMDisable(lab *agenticIAMLab) {
	fp := lab.footprint()
	if global.DryRun {
		printAgenticIAMDisablePlan(fp)
		return
	}
	if fp.empty() {
		fmt.Println("ℹ️  The Agentic IAM lab is not enabled: nothing to remove.")
		return
	}

	fmt.Println("🛑 Tearing down the Agentic IAM lab...")
	errs := disableAgenticIAM(lab)
	fmt.Printf("  ℹ️  Image %s kept: it is a local build cache, reused by the next enable.\n", agenticiam.ImageRef())
	global.RefreshHalHealth(lab.engine)

	if len(errs) > 0 {
		fmt.Printf("⚠️  Agentic IAM lab removed, with %d problem(s):\n", len(errs))
		for _, err := range errs {
			fmt.Printf("   - %s\n", strings.ReplaceAll(err.Error(), "\n", "\n     "))
		}
		fmt.Println("   Fix them and run 'hal vault agentic-iam disable' again, or 'hal vault delete' for a full teardown.")
		return
	}
	fmt.Println("✅ Agentic IAM lab removed")
}

func (l *agenticIAMLab) removeContainers() error {
	fmt.Printf("⚙️  Removing %s and %s...\n", agenticIAMChatContainer, agenticIAMAgentContainer)
	return errors.Join(removeAgenticIAMContainers(l.engine), removeAgenticIAMStateDir())
}

func (l *agenticIAMLab) teardownVault() error {
	if l.vaultErr != nil || l.client == nil {
		return errors.New("offline, so its lab objects were left in place: run disable again once Vault is up (hal vault delete removes them with Vault)")
	}
	fmt.Printf("⚙️  Removing the lab from Vault (%s/ and its leases, Agent Registry record, profile, entities, groups, policies)...\n", agenticDBMount)
	return teardownAgenticIAMVault(l.client)
}

// agenticIAMMariaDBRelease is what disable does to the shared MariaDB, from
// its consumer list and whether the container runs.
type agenticIAMMariaDBRelease struct {
	Release bool // the lab is a consumer: deregister it, and remove the container if it was the last
	Cleanup bool // drop the lab's schema and broker first: the container stays for other consumers
	Stopped bool // other consumers remain, but the container is stopped: the cleanup cannot run
}

func planAgenticIAMMariaDBRelease(consumers []string, running bool) agenticIAMMariaDBRelease {
	if !slices.Contains(consumers, global.VaultMariaDBConsumerAgenticIAM) {
		return agenticIAMMariaDBRelease{}
	}
	others := len(agenticIAMOthers(consumers, global.VaultMariaDBConsumerAgenticIAM)) > 0
	return agenticIAMMariaDBRelease{Release: true, Cleanup: others && running, Stopped: others && !running}
}

// releaseMariaDB drops the lab's schema and broker when hal-vault-mariadb
// stays up for another consumer, then releases it. A lab that is not a
// consumer leaves the container alone: it may belong to `hal vault database`
// alone, even with an empty consumer list.
func (l *agenticIAMLab) releaseMariaDB() error {
	plan := planAgenticIAMMariaDBRelease(
		global.GetSharedServiceConsumers(global.SharedVaultMariaDBServiceKey),
		global.IsContainerRunning(l.engine, vaultMariaDBContainer))
	if !plan.Release {
		return nil
	}

	var err error
	switch {
	case plan.Cleanup:
		fmt.Printf("⚙️  Dropping the %s schema and the %s broker from %s...\n", agenticIAMSchema, agenticIAMDefaultBrokerUser, vaultMariaDBContainer)
		sql, sqlErr := agenticIAMCleanupSQL(agenticIAMDefaultBrokerUser)
		if sqlErr == nil {
			sqlErr = runVaultMariaDBSQL(l.engine, sql)
		}
		if sqlErr != nil {
			err = fmt.Errorf("drop the %s schema and the %s broker: %w", agenticIAMSchema, agenticIAMDefaultBrokerUser, sqlErr)
		}
	case plan.Stopped:
		err = fmt.Errorf("%s is stopped, so the %s schema and the %s broker were not dropped", vaultMariaDBContainer, agenticIAMSchema, agenticIAMDefaultBrokerUser)
	}
	releaseVaultMariaDB(l.engine, global.VaultMariaDBConsumerAgenticIAM)
	return err
}

// agenticIAMDeleteSharedPersonas tells whether disable may delete alice and
// bob: only when the lab is a consumer of Authentik and no other lab is.
func agenticIAMDeleteSharedPersonas(consumers []string) bool {
	return slices.Contains(consumers, integrations.AgenticIAMAuthentikConsumer) &&
		!integrations.AgenticIAMSharedPersonasInUse(consumers)
}

// removeAuthentikObjects deletes the lab's objects from a running Authentik,
// whether or not the lab is registered, so that leftovers go too. Missing
// objects are skipped.
func (l *agenticIAMLab) removeAuthentikObjects() error {
	if !integrations.IsAuthentikRunning(l.engine) {
		return nil
	}
	c, err := agenticIAMAuthentikClient()
	if err != nil {
		return err
	}
	consumers := global.GetSharedServiceConsumers(integrations.AuthentikSharedServiceKey)
	opts := integrations.AgenticIAMRemoveOptions{DeleteSharedPersonas: agenticIAMDeleteSharedPersonas(consumers)}
	fmt.Print("⚙️  Removing the lab's Authentik objects (applications, actor, scope mappings, charlie, groups")
	if opts.DeleteSharedPersonas {
		fmt.Print(", alice and bob")
	}
	fmt.Println(")...")
	return integrations.RemoveAgenticIAMAuthentik(c, opts)
}

// releaseAuthentik deregisters the lab from the shared Authentik, and stops the
// stack when no other lab uses it, like hal vault oidc disable.
func (l *agenticIAMLab) releaseAuthentik() error {
	if !slices.Contains(global.GetSharedServiceConsumers(integrations.AuthentikSharedServiceKey), integrations.AgenticIAMAuthentikConsumer) {
		return nil
	}
	remaining, err := global.RemoveSharedServiceConsumer(integrations.AuthentikSharedServiceKey, integrations.AgenticIAMAuthentikConsumer)
	if err != nil {
		return fmt.Errorf("update the shared service registry: %w", err)
	}
	if len(remaining) > 0 {
		fmt.Printf("  ℹ️  Authentik still in use by: %s — stack left running\n", strings.Join(remaining, ", "))
		return nil
	}
	fmt.Println("  No other lab depends on Authentik — stopping the stack...")
	if err := integrations.StopAuthentikStack(l.engine, true); err != nil {
		return fmt.Errorf("stop the Authentik stack: %w", err)
	}
	fmt.Println("  ✅ Authentik stack stopped and volumes removed")
	return nil
}

// agenticIAMFootprint is what exists of the lab, for disable's no-op check
// and its dry run.
type agenticIAMFootprint struct {
	Containers        []string // existing lab containers, in any state
	StateDir          bool     // ~/.hal/agentic-iam
	MariaDBConsumer   bool
	AuthentikConsumer bool
	VaultObjects      []string
	AuthentikApps     []string
}

func (f agenticIAMFootprint) empty() bool {
	return len(f.Containers) == 0 && !f.StateDir && !f.MariaDBConsumer && !f.AuthentikConsumer &&
		len(f.VaultObjects) == 0 && len(f.AuthentikApps) == 0
}

// footprint looks for every trace of the lab. It only reads.
func (l *agenticIAMLab) footprint() agenticIAMFootprint {
	var f agenticIAMFootprint
	for _, name := range []string{agenticIAMChatContainer, agenticIAMAgentContainer} {
		if agenticIAMContainerExists(l.engine, name) {
			f.Containers = append(f.Containers, name)
		}
	}
	if dir, err := agenticIAMStateDir(); err == nil {
		if _, err := os.Stat(dir); err == nil {
			f.StateDir = true
		}
	}
	f.MariaDBConsumer = slices.Contains(global.GetSharedServiceConsumers(global.SharedVaultMariaDBServiceKey), global.VaultMariaDBConsumerAgenticIAM)
	f.AuthentikConsumer = slices.Contains(global.GetSharedServiceConsumers(integrations.AuthentikSharedServiceKey), integrations.AgenticIAMAuthentikConsumer)
	if l.vaultErr == nil && l.client != nil {
		f.VaultObjects = agenticIAMVaultObjects(l.client)
	}
	if integrations.IsAuthentikRunning(l.engine) {
		f.AuthentikApps, _ = agenticIAMAuthentikApps()
	}
	return f
}

func printAgenticIAMDisablePlan(f agenticIAMFootprint) {
	fmt.Println("[DRY RUN] hal vault agentic-iam disable")
	if f.empty() {
		fmt.Println("[DRY RUN] The Agentic IAM lab is not enabled: nothing would be removed")
		return
	}
	fmt.Printf("[DRY RUN] Would remove the containers %s and the env files in ~/.hal/%s\n", listOrNone(f.Containers), agenticIAMStateDirName)
	fmt.Printf("[DRY RUN] Would remove the lab's Vault objects: %s\n", listOrNone(f.VaultObjects))
	if f.MariaDBConsumer {
		fmt.Printf("[DRY RUN] Would drop the %s schema and the %s broker if %s stays up for another consumer, then deregister %s and remove the container if it was the last\n",
			agenticIAMSchema, agenticIAMDefaultBrokerUser, vaultMariaDBContainer, global.VaultMariaDBConsumerAgenticIAM)
	}
	if len(f.AuthentikApps) > 0 || f.AuthentikConsumer {
		fmt.Printf("[DRY RUN] Would remove the lab's Authentik objects: applications %s, the actor, scope mappings, charlie and the lab's groups\n", listOrNone(f.AuthentikApps))
	}
	if f.AuthentikConsumer {
		fmt.Printf("[DRY RUN] Would deregister %s from Authentik and stop the stack if no other lab uses it\n", integrations.AgenticIAMAuthentikConsumer)
	}
	fmt.Printf("[DRY RUN] Would keep the image %s (local build cache)\n", agenticiam.ImageRef())
}

func listOrNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}

// agenticIAMOthers returns the consumers other than self.
func agenticIAMOthers(consumers []string, self string) []string {
	var others []string
	for _, c := range consumers {
		if c != self {
			others = append(others, c)
		}
	}
	return others
}

// ─── hal vault delete ─────────────────────────────────────────────────────────

// releaseAgenticIAMOnVaultDelete is hal vault delete's part for the lab, once
// both containers are gone with the rest of the ecosystem: it removes the
// env files and, while Authentik stays up for another lab, the lab's Authentik
// objects. The caller deregisters the Authentik consumer and stops Authentik
// when nobody is left; hal-vault-mariadb is removed with the ecosystem.
func releaseAgenticIAMOnVaultDelete(engine string, wasConsumer bool, remainingAuthentik []string) {
	if err := removeAgenticIAMStateDir(); err != nil {
		fmt.Printf("⚠️  Could not remove ~/.hal/%s: %v\n", agenticIAMStateDirName, err)
	}
	if !wasConsumer || len(remainingAuthentik) == 0 || !integrations.IsAuthentikRunning(engine) {
		return
	}
	c, err := agenticIAMAuthentikClient()
	if err == nil {
		err = integrations.RemoveAgenticIAMAuthentik(c, integrations.AgenticIAMRemoveOptions{})
	}
	if err != nil {
		fmt.Printf("⚠️  Could not remove the Agentic IAM lab's Authentik objects: %v\n", err)
		return
	}
	fmt.Println("  ✅ Agentic IAM lab objects removed from the shared Authentik")
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// agenticIAMAuthentikClient returns an API client for the running Authentik,
// with the bootstrap token HAL saved when it started the stack. Unlike enable,
// it never creates that secrets file.
func agenticIAMAuthentikClient() (*integrations.AuthentikClient, error) {
	if _, err := os.Stat(integrations.AuthentikEnvPath()); err != nil {
		return nil, fmt.Errorf("no Authentik secrets at %s: %w", integrations.AuthentikEnvPath(), err)
	}
	secrets, err := integrations.LoadOrCreateAuthentikSecrets()
	if err != nil {
		return nil, err
	}
	return integrations.NewAuthentikClient(secrets.BootstrapToken), nil
}

// agenticIAMAuthentikApps returns the lab's applications that exist in the
// running Authentik.
func agenticIAMAuthentikApps() ([]string, error) {
	c, err := agenticIAMAuthentikClient()
	if err != nil {
		return nil, err
	}
	var found []string
	for _, slug := range []string{integrations.AgenticIAMChatSlug, integrations.AgenticIAMDemoAgentSlug, integrations.AgenticIAMVaultSlug} {
		ok, err := c.ApplicationExists(slug)
		if err != nil {
			return found, err
		}
		if ok {
			found = append(found, slug)
		}
	}
	return found, nil
}

// agenticIAMVaultObjects returns the lab's objects that exist in Vault. It
// only reads; read errors (e.g. no Agentic IAM license) count as absent.
func agenticIAMVaultObjects(client *vault.Client) []string {
	var found []string
	if mounts, err := client.Sys().ListMounts(); err == nil {
		if m, ok := mounts[agenticDBMount+"/"]; ok && m != nil {
			found = append(found, agenticDBMount+"/")
		}
	}
	if rec, err := readIfExists(client, agentRegistryMount+"/registration/display-name/"+agenticIAMAgentName); err == nil && rec != nil &&
		stringField(rec.Data, "owner") == agenticIAMOwner {
		found = append(found, "Agent Registry record "+agenticIAMAgentName)
	}
	if p, err := readIfExists(client, oauthResourceServerPath()); err == nil && p != nil {
		found = append(found, "profile "+agenticIAMProfileName)
	}
	for _, name := range []string{agenticIAMAgentName, agenticIAMPersonas[0].EntityName(), agenticIAMPersonas[1].EntityName()} {
		if e, err := readIfExists(client, "identity/entity/name/"+name); err == nil && e != nil && hasAgenticIAMMarker(e.Data) {
			found = append(found, "entity "+name)
		}
	}
	for _, g := range agenticIAMGroups {
		if grp, err := readIfExists(client, "identity/group/name/"+g.Name); err == nil && grp != nil && hasAgenticIAMMarker(grp.Data) {
			found = append(found, "group "+g.Name)
		}
	}
	for _, p := range []string{agenticIAMFinancePolicy, agenticIAMCeilingPolicy} {
		if body, err := client.Sys().GetPolicy(p); err == nil && body != "" {
			found = append(found, "policy "+p)
		}
	}
	return found
}

func init() {
	vaultAgenticIAMCmd.Flags().BoolVarP(&agenticIAMEnable, "enable", "e", false, "Deploy the Agentic IAM lab")
	vaultAgenticIAMCmd.Flags().BoolVarP(&agenticIAMDisable, "disable", "d", false, "Remove the Agentic IAM lab")
	vaultAgenticIAMCmd.Flags().BoolVarP(&agenticIAMUpdate, "update", "u", false, "Re-apply the Agentic IAM lab and recreate its containers")
	_ = vaultAgenticIAMCmd.Flags().MarkHidden("enable")
	_ = vaultAgenticIAMCmd.Flags().MarkHidden("disable")
	_ = vaultAgenticIAMCmd.Flags().MarkHidden("update")

	vaultAgenticIAMCmd.Flags().StringVar(&agenticIAMAuthentikImage, "authentik-image", integrations.AuthentikDefaultImage,
		"Authentik container image (only used when Authentik is not running yet)")
	vaultAgenticIAMCmd.Flags().StringVar(&agenticIAMAuthentikTag, "authentik-tag", integrations.AuthentikDefaultTag,
		"Authentik image tag, "+agenticIAMMinAuthentikVersion+" or later (only used when Authentik is not running yet)")
	vaultAgenticIAMCmd.Flags().StringVar(&agenticIAMMariaDBImage, "vault-mariadb-image", defaultVaultMariaDBImage,
		"MariaDB container image name (ignored when reusing the running shared hal-vault-mariadb)")
	vaultAgenticIAMCmd.Flags().StringVar(&agenticIAMMariaDBTag, "vault-mariadb-tag", defaultVaultMariaDBTag,
		"MariaDB container image tag (ignored when reusing the running shared hal-vault-mariadb)")

	Cmd.AddCommand(vaultAgenticIAMCmd)
}
