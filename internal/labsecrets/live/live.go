// Package live turns what the container engine reports into the credential
// tree of internal/labsecrets. It is the only half of the credential surface
// that touches the engine: the catalog, the runtime store and the tree itself
// stay pure so they can be read and tested on their own.
//
// `hal creds status`, `hal vault status`, `hal terraform status` and the MCP
// credentials tool all come through here, so none of them can drift from
// another.
package live

import (
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"hal/internal/global"
	"hal/internal/integrations"
	"hal/internal/labsecrets"

	vault "github.com/hashicorp/vault/api"
)

const (
	vaultContainer    = "hal-vault"
	vaultMariaDB      = "hal-vault-mariadb"
	vaultOSInstance   = "hal-vault-os"
	openLDAPContainer = "hal-openldap"
	aapContainer      = "hal-aap"
	gitLabContainer   = "hal-gitlab"
	tfeContainer      = "hal-tfe"
	tfeTwinContainer  = "hal-tfe-bis"

	vaultDevURL     = "http://vault.localhost:8200"
	phpLDAPAdminURL = "https://phpldapadmin.localhost:8082"
	tfeURL          = "https://tfe.localhost"
	tfeTwinURL      = "https://tfe-bis.localhost"
	aapURL          = "https://aap.localhost"
)

// Tree returns the credential tree of everything currently running.
func Tree(engine string) labsecrets.Tree {
	return labsecrets.BuildTree(Facts(engine), Values())
}

// Facts reports which products and scenarios are up.
func Facts(engine string) labsecrets.Facts {
	vaultUp := global.CheckContainer(engine, vaultContainer)
	oidcEnabled, authentikRunning := authentikState(engine)
	gitLabConsumers := global.GetSharedServiceConsumers(global.SharedGitLabServiceKey)
	authentikConsumers := global.GetSharedServiceConsumers(integrations.AuthentikSharedServiceKey)

	facts := labsecrets.Facts{
		VaultRunning: vaultUp,
		VaultProd:    vaultUp && prodVault(),

		VaultOIDCEnabled:       oidcEnabled,
		VaultAgenticIAMRunning: global.CheckContainer(engine, global.AgenticIAMChatContainer),
		VaultAgenticIAMURL:     global.AgenticIAMChatURL,
		VaultLDAPRunning:       global.CheckContainer(engine, openLDAPContainer),
		VaultLDAPAdminURL:      phpLDAPAdminURL,
		VaultUserPassEnabled:   vaultUp && vaultAuthMountExists("userpass"),
		VaultDatabaseActive:    global.CheckContainer(engine, vaultMariaDB) && global.VaultDatabaseUsesMariaDB(),
		VaultOSRunning:         global.MultipassInstanceExists(vaultOSInstance),
		VaultAAPRunning:        global.IsContainerRunning(engine, aapContainer),

		GitLabRunning:      global.CheckContainer(engine, gitLabContainer),
		GitLabUnderVault:   slices.Contains(gitLabConsumers, global.GitLabConsumerVaultJWT),
		GitLabUnderTFE:     slices.Contains(gitLabConsumers, global.GitLabConsumerVCSPrimary),
		GitLabUnderTFETwin: slices.Contains(gitLabConsumers, global.GitLabConsumerVCSTwin),

		AuthentikRunning:      authentikRunning,
		AuthentikURL:          integrations.AuthentikAdminURL() + "/if/admin/",
		AuthentikUnderTFE:     slices.Contains(authentikConsumers, global.AuthentikConsumerTFESAMLPrimary),
		AuthentikUnderTFETwin: slices.Contains(authentikConsumers, global.AuthentikConsumerTFESAMLTwin),

		TFERunning:     global.CheckContainer(engine, tfeContainer),
		TFEURL:         tfeURL,
		TFETwinRunning: global.CheckContainer(engine, tfeTwinContainer),
		TFETwinURL:     tfeTwinURL,
	}

	// A Vault lab owns Authentik whenever it is an OIDC or Agentic IAM
	// consumer, and also when nothing registered for it at all — that is an
	// Authentik started before consumers were counted.
	facts.AuthentikUnderVault = oidcEnabled ||
		slices.Contains(authentikConsumers, integrations.AgenticIAMAuthentikConsumer) ||
		(authentikRunning && len(authentikConsumers) == 0)

	if facts.GitLabRunning {
		facts.GitLabURL = gitLabURL(engine)
	}
	if facts.VaultAAPRunning {
		facts.VaultAAPURL = aapAddress(engine)
	}

	return facts
}

// Values are the secrets the labs generated. The runtime store is the first
// place to look; a lab that booted before the store existed still has its
// secret in the file it boots from, so that is the fallback.
func Values() labsecrets.Values {
	values := labsecrets.Load()

	if values.Get(labsecrets.KeyVaultRootToken) == "" {
		if init, err := global.LoadCachedVaultInit(); err == nil && init.RootToken != "" {
			values[labsecrets.KeyVaultRootToken] = init.RootToken
			if len(init.UnsealKeysB64) > 0 {
				values[labsecrets.KeyVaultUnsealKey] = init.UnsealKeysB64[0]
			}
		}
	}
	if values.Get(labsecrets.KeyVaultCACert) == "" && values.Get(labsecrets.KeyVaultRootToken) != "" {
		values[labsecrets.KeyVaultCACert] = global.VaultProdCertPath()
	}
	if values.Get(labsecrets.KeyAuthentikAdminPassword) == "" {
		if password := integrations.LoadAuthentikAdminPassword(); password != "" {
			values[labsecrets.KeyAuthentikAdminPassword] = password
		}
	}
	if values.Get(labsecrets.KeyTFEAPIToken) == "" {
		if token := global.LoadCachedTFEAPIToken(); token != "" {
			values[labsecrets.KeyTFEAPIToken] = token
		}
	}

	return values
}

// prodVault reports whether Vault was created with --mode prod, which is what
// a cached init (root token and unseal key) means.
func prodVault() bool {
	init, err := global.LoadCachedVaultInit()
	return err == nil && init.RootToken != ""
}

// authentikState tells whether the Vault OIDC demo users exist, and whether
// Authentik itself runs. The demo users belong to vault-oidc; an Authentik
// running for nobody predates consumer counting, so its users are shown too.
func authentikState(engine string) (oidc, running bool) {
	consumers := global.GetSharedServiceConsumers(integrations.AuthentikSharedServiceKey)
	running = global.CheckContainer(engine, integrations.AuthentikServerContainer)
	oidc = slices.Contains(consumers, "vault-oidc") || (running && len(consumers) == 0)
	return oidc, oidc || running
}

// gitLabURL is the address GitLab actually answers on: it is a shared
// singleton, so whichever product booted it fixed the port.
func gitLabURL(engine string) string {
	port := integrations.DetectGitLabPort(engine)
	if port <= 0 {
		port = integrations.DefaultGitLabPort
	}
	return "http://gitlab.localhost:" + strconv.Itoa(port)
}

// aapAddress follows `hal vault aap enable --host-port`, which publishes the
// controller's 443 on whichever host port was free.
func aapAddress(engine string) string {
	port := publishedPort(engine, aapContainer, "443/tcp")
	if port <= 0 || port == 443 {
		return aapURL
	}
	return aapURL + ":" + strconv.Itoa(port)
}

// vaultAuthMountExists reports whether an auth method is enabled in Vault.
func vaultAuthMountExists(method string) bool {
	config := vault.DefaultConfig()
	if os.Getenv("VAULT_ADDR") == "" {
		config.Address = vaultDevURL
	}
	client, err := vault.NewClient(config)
	if err != nil {
		return false
	}
	if os.Getenv("VAULT_TOKEN") == "" {
		client.SetToken(labsecrets.VaultDevRootToken)
	}
	mounts, err := client.Sys().ListAuth()
	if err != nil {
		return false
	}
	_, ok := mounts[strings.TrimSuffix(method, "/")+"/"]
	return ok
}

// publishedPort returns the host port a container publishes for the given
// container port, or 0 when it is not published.
func publishedPort(engine, container, containerPort string) int {
	out, err := exec.Command(engine, "inspect", "-f",
		`{{(index .NetworkSettings.Ports "`+containerPort+`" 0).HostPort}}`, container).Output()
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0
	}
	return port
}
