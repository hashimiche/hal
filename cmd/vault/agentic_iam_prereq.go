package vault

// agentic_iam_prereq.go holds the read-only prerequisite probe of the Agentic
// IAM lab (ADR 0004, decision 1): Vault must be Enterprise >= 2.1.0 and its
// license must include the Agentic IAM terms. The probe changes nothing.

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	vault "github.com/hashicorp/vault/api"
)

const (
	// agenticIAMMinVaultVersion is the first Vault Enterprise release with the
	// OAuth resource server and the Agent Registry.
	agenticIAMMinVaultVersion = "2.1.0"
	// agenticIAMFallbackEntTag is suggested when the package default
	// Enterprise tag is older than agenticIAMMinVaultVersion.
	agenticIAMFallbackEntTag = "2.1.1-ent"

	oauthResourceServerConfigPath = "sys/config/oauth-resource-server"
	agentRegistryMount            = "agent-registry"
	agentRegistryMountType        = "agent_registry"
)

// Prerequisite failures. They are wrapped by *agenticIAMPrereqError, so callers
// can tell them apart with errors.Is.
var (
	errAgenticIAMNotEnterprise = errors.New("vault is not Enterprise")
	errAgenticIAMVaultTooOld   = errors.New("vault is older than " + agenticIAMMinVaultVersion)
	errAgenticIAMNotLicensed   = errors.New("the Vault license does not include Agentic IAM")
	errAgentRegistryMissing    = errors.New(agentRegistryMount + "/ is not mounted")
)

// agenticIAMPrereqError is a refused prerequisite: what is wrong, and the
// commands that fix it. Error() renders both, ready to print after "❌ ".
type agenticIAMPrereqError struct {
	kind    error
	problem string
	fixes   []string
}

func (e *agenticIAMPrereqError) Error() string {
	var b strings.Builder
	b.WriteString(e.problem)
	for i, fix := range e.fixes {
		if i == 0 {
			b.WriteString("\n   💡 ")
		} else {
			b.WriteString("\n      ")
		}
		b.WriteString(fix)
	}
	return b.String()
}

func (e *agenticIAMPrereqError) Unwrap() error { return e.kind }

// checkAgenticIAMPrerequisites refuses a Vault that cannot run the Agentic IAM
// lab, with an actionable message. It only reads: sys/health, sys/mounts and a
// LIST on sys/config/oauth-resource-server. prod is vaultProdActive(), passed
// in so that the remediation keeps the deployment mode and tests do not depend
// on the host's HAL state.
func checkAgenticIAMPrerequisites(client *vault.Client, prod bool) error {
	health, err := client.Sys().Health()
	if err != nil {
		return fmt.Errorf("read Vault health: %w", err)
	}
	if err := checkAgenticIAMVaultVersion(health.Version, prod); err != nil {
		return err
	}

	mounts, err := client.Sys().ListMounts()
	if err != nil {
		return fmt.Errorf("list Vault mounts: %w", err)
	}
	if m, ok := mounts[agentRegistryMount+"/"]; !ok || m == nil || m.Type != agentRegistryMountType {
		return &agenticIAMPrereqError{
			kind: errAgentRegistryMissing,
			problem: fmt.Sprintf("Vault %s has no %s/ mount (type %s). Vault Enterprise mounts it by default, so it was disabled.",
				health.Version, agentRegistryMount, agentRegistryMountType),
			fixes: agenticIAMRedeployFixes("Recreate Vault, then re-enable the lab:", health.Version, prod, false),
		}
	}

	return probeOAuthResourceServerFeature(client, health.Version, prod)
}

// checkAgenticIAMVaultVersion checks the edition and version reported by
// sys/health (e.g. "2.1.1+ent"). The edition test is the same substring check
// as isVaultEnterprise.
func checkAgenticIAMVaultVersion(version string, prod bool) error {
	fixes := agenticIAMRedeployFixes("Redeploy Vault Enterprise with a license that includes the Agentic IAM terms:", version, prod, true)

	if !strings.Contains(version, "ent") {
		return &agenticIAMPrereqError{
			kind:    errAgenticIAMNotEnterprise,
			problem: fmt.Sprintf("The Agentic IAM lab needs Vault Enterprise %s or later, but Vault %s is Community Edition.", agenticIAMMinVaultVersion, version),
			fixes:   fixes,
		}
	}
	if !vaultVersionAtLeast(version, agenticIAMMinVaultVersion) {
		return &agenticIAMPrereqError{
			kind:    errAgenticIAMVaultTooOld,
			problem: fmt.Sprintf("The Agentic IAM lab needs Vault Enterprise %s or later, but Vault is %s.", agenticIAMMinVaultVersion, version),
			fixes:   fixes,
		}
	}
	return nil
}

// probeOAuthResourceServerFeature tells whether the license includes Agentic
// IAM. A LIST on sys/config/oauth-resource-server answers 404 (no profile) or
// the profile names when it does, and HTTP 401 "Feature Not Enabled" when it
// does not. Vault 2.1.0 added "Agentic IAM terms" to the license model, so a
// valid 2.1.x Enterprise license can still lack them.
func probeOAuthResourceServerFeature(client *vault.Client, version string, prod bool) error {
	_, err := client.Logical().List(oauthResourceServerConfigPath)
	return classifyOAuthResourceServerProbe(err, version, prod)
}

// classifyOAuthResourceServerProbe maps the probe's error to nil (licensed),
// a license prerequisite error, or a plain error for anything unexpected.
func classifyOAuthResourceServerProbe(err error, version string, prod bool) error {
	if err == nil {
		return nil
	}
	if isFeatureNotEnabled(err) {
		return &agenticIAMPrereqError{
			kind: errAgenticIAMNotLicensed,
			problem: fmt.Sprintf("Vault %s runs, but its license does not include Agentic IAM (%s answered HTTP 401 \"Feature Not Enabled\").",
				version, oauthResourceServerConfigPath),
			fixes: agenticIAMRedeployFixes("Get a Vault Enterprise license with the Agentic IAM terms (license model of Vault 2.1.0+), then:", version, prod, true),
		}
	}
	return fmt.Errorf("probe %s: %w", oauthResourceServerConfigPath, err)
}

// isFeatureNotEnabled reports Vault's answer to an endpoint the license does
// not cover: HTTP 401 with "Feature Not Enabled" in the error list.
func isFeatureNotEnabled(err error) bool {
	var respErr *vault.ResponseError
	if !errors.As(err, &respErr) || respErr.StatusCode != http.StatusUnauthorized {
		return false
	}
	for _, msg := range respErr.Errors {
		if strings.Contains(strings.ToLower(msg), "feature not enabled") {
			return true
		}
	}
	return false
}

// agenticIAMRedeployFixes lists the remediation steps after intro: set the
// license (when it is part of the fix), redeploy Vault, re-enable the lab.
func agenticIAMRedeployFixes(intro, runningVersion string, prod, license bool) []string {
	fixes := []string{intro}
	if license {
		fixes = append(fixes, "export VAULT_LICENSE_PATH=/path/to/vault.hclic   (unset VAULT_LICENSE: it takes precedence)")
	}
	fixes = append(fixes, agenticIAMRedeployCommand(runningVersion, prod), "hal vault agentic-iam enable")
	if !prod {
		fixes = append(fixes, "(hal vault update recreates the dev Vault: re-enable your other Vault labs afterwards)")
	}
	return fixes
}

// agenticIAMRedeployCommand returns the hal command that redeploys Vault
// Enterprise on a version that has Agentic IAM, keeping the current mode.
func agenticIAMRedeployCommand(runningVersion string, prod bool) string {
	cmd := "hal vault update --edition ent"
	if prod {
		cmd = "hal vault update --mode prod"
	}
	return cmd + " --vault-tag " + agenticIAMSuggestedEntTag(runningVersion)
}

// agenticIAMSuggestedEntTag picks the image tag to suggest: the running one
// when it is already a recent enough Enterprise build (so a license fix does
// not change the version), else the package default, else a known-good tag.
func agenticIAMSuggestedEntTag(runningVersion string) string {
	if strings.Contains(runningVersion, "+ent") && vaultVersionAtLeast(runningVersion, agenticIAMMinVaultVersion) {
		return strings.Replace(runningVersion, "+ent", "-ent", 1)
	}
	if vaultVersionAtLeast(defaultVaultEntTag, agenticIAMMinVaultVersion) {
		return defaultVaultEntTag
	}
	return agenticIAMFallbackEntTag
}

// parseVaultVersion extracts major.minor.patch from a Vault version string
// such as "2.1.1+ent", "2.1.1-ent", "v2.0.4" or "2.1.0-rc1+ent". A missing
// patch number counts as 0.
func parseVaultVersion(version string) ([3]int, bool) {
	var out [3]int
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if end := strings.IndexFunc(v, func(r rune) bool { return !unicode.IsDigit(r) && r != '.' }); end >= 0 {
		v = v[:end]
	}
	parts := strings.Split(v, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// vaultVersionAtLeast compares the numeric parts of two Vault versions. An
// unparsable version is never "at least" anything.
func vaultVersionAtLeast(version, minimum string) bool {
	got, ok := parseVaultVersion(version)
	if !ok {
		return false
	}
	want, ok := parseVaultVersion(minimum)
	if !ok {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return got[i] > want[i]
		}
	}
	return true
}
