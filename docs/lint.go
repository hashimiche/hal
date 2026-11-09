package topics

import (
	"fmt"
	"regexp"
	"strings"
)

// An operator login is the lab secret a person types to reach a lab (CONTEXT.md):
// a username and password, a Vault token, an unseal key, an API token. ADR 0006
// keeps them out of topic files: the file is sent to a hosted model and served as
// an MCP resource, and a login written there goes stale when the lab changes it.
// A topic that needs one lists `hal creds status` under read instead.
//
// OperatorLogins is the definition. A topic file carries an operator login when
// any line of it, header or prose, matches one of these rules:
//
//  1. VAULT_TOKEN assignment: VAULT_TOKEN= with any value, so also
//     export VAULT_TOKEN='...' and VAULT_TOKEN=$(...). The ADR names this one.
//  2. vault login with a literal token: `vault login root`. A flag first, as in
//     `vault login -method=oidc`, carries no secret and is not matched.
//  3. Vault token literal: an hvs., hvb. or hvr. token, or a legacy s. token.
//  4. Secret assignment: a name ending in password, passwd, pwd, secret,
//     secret_id, token or api_key (any prefix: root_password, VAULT_TOKEN), then
//     "=", then a literal value. token_policies= or token_ttl= are not secrets.
//  5. Token header: X-Vault-Token: followed by a literal value.
//  6. Secret flag: --password, --passwd or --token followed by a literal value.
//  7. Printed login line, as `hal creds status` renders it: login, root token,
//     unseal key, api token, password or passwd, then ":", then a value.
//  8. Login pair: "something / password", "/ admin" or "/ root" with spaces on
//     both sides, which is how the credential tree prints a user and secret.
//  9. Known lab secret: any value the caller passes in, anywhere in a line. The
//     tests pass the distinctive passwords of internal/labsecrets.
//
// A literal value is anything but a placeholder. A placeholder, after its
// quotes and backticks are trimmed, is empty, starts with $ < { [ ( % * . or an
// ellipsis, or is the word hal (a pointer to a command, as in
// "password: `hal creds status`"). So `--token $TOKEN` and `password=<password>`
// are fine, and `password=hunter2` is not.
//
// A bare username is not a login: "root" in root/secret-zero is a project path,
// and the user is half of what a person types, never the part that goes stale.
// Ordinary words that the catalog also uses as secrets (root, admin, password)
// are not matched on their own for the same reason; rules 4, 7 and 8 catch them
// in the places they act as a secret.
//
// The rules are lexical on purpose. They aim at the shapes the lab and the old
// runbooks actually print, not at every possible leak, so a failing rule is a
// reason to reword the sentence, never to add an exception.

// Finding is a line of a topic file that holds an operator login. It carries the
// line number and the rules that matched, never the matched text, so a test log
// does not echo a secret.
type Finding struct {
	Line  int
	Rules []string
}

type loginRule struct {
	name string
	re   *regexp.Regexp
	// valueGroup, when above zero, is the capture group holding the value, which
	// makes the match a finding only if the value is not a placeholder.
	valueGroup int
}

var loginRules = []loginRule{
	{name: "VAULT_TOKEN assignment", re: regexp.MustCompile(`\bVAULT_TOKEN\s*=`)},
	{
		name:       "vault login with a token",
		re:         regexp.MustCompile(`\bvault login\s+([^\s-]\S*)`),
		valueGroup: 1,
	},
	{name: "Vault token literal", re: regexp.MustCompile(`\b(?:hv[sbr]\.[A-Za-z0-9_-]{8,}|s\.[A-Za-z0-9]{24})\b`)},
	{
		name:       "secret assignment",
		re:         regexp.MustCompile(`(?i)(?:password|passwd|pwd|secret|secret[_-]?id|token|api[_-]?key)\s*=\s*(\S+)`),
		valueGroup: 1,
	},
	{
		name:       "token header",
		re:         regexp.MustCompile(`(?i)\bX-Vault-Token\s*:\s*(\S+)`),
		valueGroup: 1,
	},
	{
		name:       "secret flag",
		re:         regexp.MustCompile(`(?i)--(?:password|passwd|token)(?:\s+|=)(\S+)`),
		valueGroup: 1,
	},
	{
		name:       "printed login line",
		re:         regexp.MustCompile(`(?i)\b(?:login|root token|unseal key|api token|password|passwd)\s*:\s*(\S+)`),
		valueGroup: 1,
	},
	{name: "login pair", re: regexp.MustCompile(`\S+ / (?:password|admin|root)\b`)},
}

// OperatorLogins reports the operator logins in text, in line order. secrets are
// extra literal values to refuse (rule 8).
func OperatorLogins(text string, secrets ...string) []Finding {
	var findings []Finding
	for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		var rules []string
		for _, rule := range loginRules {
			if rule.matches(line) {
				rules = append(rules, rule.name)
			}
		}
		for _, s := range secrets {
			if s != "" && strings.Contains(line, s) {
				rules = append(rules, "known lab secret")
				break
			}
		}
		if len(rules) > 0 {
			findings = append(findings, Finding{Line: i + 1, Rules: rules})
		}
	}
	return findings
}

// Check refuses a topic that carries an operator login. It is separate from
// Parse on purpose: Parse is structural and has no list of lab secrets, and a
// user who points HAL_TOPICS_DIR at their own files is not policed at runtime.
// The tests of this package and the CI test of the topics call it. The error
// names the topic, the lines and the rules, never the text matched.
func Check(t Topic, secrets ...string) error {
	findings := OperatorLogins(t.Text, secrets...)
	if len(findings) == 0 {
		return nil
	}
	parts := make([]string, len(findings))
	for i, f := range findings {
		parts[i] = fmt.Sprintf("line %d (%s)", f.Line, strings.Join(f.Rules, ", "))
	}
	return fmt.Errorf("topic %s holds an operator login: %s; list `hal creds status` under read instead", t.ID, strings.Join(parts, ", "))
}

func (r loginRule) matches(line string) bool {
	if r.valueGroup == 0 {
		return r.re.MatchString(line)
	}
	for _, m := range r.re.FindAllStringSubmatch(line, -1) {
		if !isPlaceholder(m[r.valueGroup]) {
			return true
		}
	}
	return false
}

// isPlaceholder reports whether a value stands for a secret instead of being one.
func isPlaceholder(value string) bool {
	value = strings.Trim(value, "\"'`")
	if value == "" || value == "hal" {
		return true
	}
	return strings.ContainsRune("$<{[(%*.", rune(value[0])) || strings.HasPrefix(value, "…")
}
