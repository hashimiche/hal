package creds

import (
	"fmt"
	"os"
	"strings"

	"hal/internal/global"
	"hal/internal/integrations"

	vault "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "creds",
	Short: "Manage active lab credentials",
	Long:  `Commands for inspecting active credentials across running lab services.`,
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show active lab credentials",
	Long:  `Display credentials for all currently running lab services. Only active services are shown.`,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		printed := false

		// ── Vault ────────────────────────────────────────────────────────────
		vaultUp := global.CheckContainer(engine, "hal-vault")
		if vaultUp {
			printed = true
			fmt.Println("🔐 Vault")
			fmt.Println("   URL        : http://vault.localhost:8200")
			fmt.Println("   Root token : root")
			fmt.Println()
			fmt.Println("   export VAULT_ADDR='http://vault.localhost:8200'")
			fmt.Println("   export VAULT_TOKEN='root'")
			fmt.Println()
		}

		// ── Vault OIDC + Authentik ────────────────────────────────────────────
		oidcConsumers := global.GetSharedServiceConsumers(integrations.AuthentikSharedServiceKey)
		oidcActive := false
		for _, c := range oidcConsumers {
			if c == "vault-oidc" {
				oidcActive = true
				break
			}
		}
		if oidcActive || global.CheckContainer(engine, integrations.AuthentikServerContainer) {
			printed = true
			fmt.Println("🔑 Vault OIDC — demo users")
			fmt.Println("   alice / password  →  Vault policy: admin  (all paths)")
			fmt.Println("   bob   / password  →  Vault policy: user-ro  (kv-oidc/team1 read)")
			fmt.Println()
			fmt.Println("   vault login -method=oidc")
			fmt.Println()

			fmt.Println("🌐 Authentik IdP")
			fmt.Printf("   URL      : http://authentik.localhost:%s/if/admin/\n", integrations.AuthentikHTTPPort)
			fmt.Println("   Username : akadmin")
			adminPass := loadAuthentikAdminPassword()
			if adminPass != "" {
				fmt.Printf("   Password : %s\n", adminPass)
			} else {
				fmt.Printf("   Password : (see %s)\n", integrations.AuthentikEnvPath())
			}
			fmt.Println()
		}

		// ── Vault LDAP ───────────────────────────────────────────────────────
		if global.CheckContainer(engine, "hal-openldap") {
			printed = true
			fmt.Println("📂 Vault LDAP — demo users")
			fmt.Println("   alice / password  →  group: admin")
			fmt.Println("   bob   / password  →  group: readers")
			fmt.Println()
			fmt.Println("   vault login -method=ldap username=bob password=password")
			fmt.Println()
		}

		// ── Vault UserPass ───────────────────────────────────────────────────
		if vaultUp && vaultAuthMountExists("userpass") {
			printed = true
			fmt.Println("👤 Vault UserPass — demo user")
			fmt.Println("   michaelScott / threat-level-midnight")
			fmt.Println()
			fmt.Println("   vault login -method=userpass username=michaelScott password=threat-level-midnight")
			fmt.Println()
		}

		// ── Vault Database (dynamic) ─────────────────────────────────────────
		if global.CheckContainer(engine, "hal-vault-mariadb") {
			printed = true
			fmt.Println("🗄️  Vault Database — dynamic credentials")
			fmt.Println("   vault read database/creds/writer")
			fmt.Println("   vault read database/creds/reader")
			fmt.Println()
		}

		// ── Boundary ─────────────────────────────────────────────────────────
		if global.CheckContainer(engine, "hal-boundary") {
			printed = true
			fmt.Println("🔒 Boundary")
			fmt.Println("   URL      : http://boundary.localhost:9200")
			fmt.Println("   Username : admin")
			fmt.Println("   Password : password")
			fmt.Println()
			if global.CheckContainer(engine, "hal-ssh-target") {
				fmt.Println("   SSH lab users (Boundary auth method: ssh-lab-auth)")
				fmt.Println("     ssh-operator / password")
				fmt.Println("     ssh-auditor  / password")
				fmt.Println()
			}
			if global.CheckContainer(engine, "hal-mariadb-target") {
				fmt.Println("   MariaDB target")
				fmt.Println("     host: localhost:3306  user: admin  password: password")
				fmt.Println()
			}
		}

		// ── TFE ──────────────────────────────────────────────────────────────
		if global.CheckContainer(engine, "hal-tfe") {
			printed = true
			token := global.LoadCachedTFEAPIToken()
			fmt.Println("🏗️  Terraform Enterprise")
			fmt.Println("   URL      : https://tfe.localhost")
			fmt.Println("   Username : haladmin")
			if token != "" {
				fmt.Printf("   API token: %s\n", token)
			} else {
				fmt.Println("   API token: (run `hal terraform status` to retrieve)")
			}
			fmt.Println()
		}

		if !printed {
			fmt.Println("No active lab services detected.")
			fmt.Println("Start a service first, e.g.:")
			fmt.Println("  hal vault create")
			fmt.Println("  hal vault oidc enable")
		}
	},
}

func init() {
	Cmd.AddCommand(statusCmd)
}

// CredentialEntry is a single login/secret pair (or command-derived credential)
// for an active lab service. These are non-secret demo values surfaced for
// learning; the only live value is the cached TFE API token, mirroring what
// `hal creds status` already prints to the terminal.
type CredentialEntry struct {
	Name     string `json:"name"`
	Username string `json:"username,omitempty"`
	Secret   string `json:"secret,omitempty"`
	Note     string `json:"note,omitempty"`
}

// ServiceCredentials groups the access surface and credentials for one active service.
type ServiceCredentials struct {
	Service  string            `json:"service"`
	Label    string            `json:"label"`
	URL      string            `json:"url,omitempty"`
	Entries  []CredentialEntry `json:"entries,omitempty"`
	Commands []string          `json:"commands,omitempty"`
}

// ActiveCredentials is the structured form of `hal creds status`, suitable for
// programmatic consumers (e.g. the read-only MCP credentials tool).
type ActiveCredentials struct {
	AnyActive bool                 `json:"any_active"`
	Services  []ServiceCredentials `json:"services"`
}

// CollectActiveCredentials returns the structured equivalent of `hal creds status`.
// It mirrors the detection logic of the status command so both surfaces stay aligned.
func CollectActiveCredentials() (ActiveCredentials, error) {
	engine, err := global.DetectEngine()
	if err != nil {
		return ActiveCredentials{}, err
	}

	var services []ServiceCredentials

	vaultUp := global.CheckContainer(engine, "hal-vault")
	if vaultUp {
		services = append(services, ServiceCredentials{
			Service: "vault",
			Label:   "Vault",
			URL:     "http://vault.localhost:8200",
			Entries: []CredentialEntry{{Name: "Root token", Secret: "root"}},
			Commands: []string{
				"export VAULT_ADDR='http://vault.localhost:8200'",
				"export VAULT_TOKEN='root'",
			},
		})
	}

	oidcConsumers := global.GetSharedServiceConsumers(integrations.AuthentikSharedServiceKey)
	oidcActive := false
	for _, c := range oidcConsumers {
		if c == "vault-oidc" {
			oidcActive = true
			break
		}
	}
	if oidcActive || global.CheckContainer(engine, integrations.AuthentikServerContainer) {
		services = append(services, ServiceCredentials{
			Service: "vault-oidc",
			Label:   "Vault OIDC — demo users",
			Entries: []CredentialEntry{
				{Name: "alice", Username: "alice", Secret: "password", Note: "Vault policy: admin (all paths)"},
				{Name: "bob", Username: "bob", Secret: "password", Note: "Vault policy: user-ro (kv-oidc/team1 read)"},
			},
			Commands: []string{"vault login -method=oidc"},
		})

		authentik := ServiceCredentials{
			Service: "authentik",
			Label:   "Authentik IdP",
			URL:     fmt.Sprintf("http://authentik.localhost:%s/if/admin/", integrations.AuthentikHTTPPort),
		}
		entry := CredentialEntry{Name: "akadmin", Username: "akadmin"}
		if adminPass := loadAuthentikAdminPassword(); adminPass != "" {
			entry.Secret = adminPass
		} else {
			entry.Note = "see " + integrations.AuthentikEnvPath()
		}
		authentik.Entries = []CredentialEntry{entry}
		services = append(services, authentik)
	}

	if global.CheckContainer(engine, "hal-openldap") {
		services = append(services, ServiceCredentials{
			Service: "vault-ldap",
			Label:   "Vault LDAP — demo users",
			Entries: []CredentialEntry{
				{Name: "alice", Username: "alice", Secret: "password", Note: "group: admin"},
				{Name: "bob", Username: "bob", Secret: "password", Note: "group: readers"},
			},
			Commands: []string{"vault login -method=ldap username=bob password=password"},
		})
	}

	if vaultUp && vaultAuthMountExists("userpass") {
		services = append(services, ServiceCredentials{
			Service:  "vault-userpass",
			Label:    "Vault UserPass — demo user",
			Entries:  []CredentialEntry{{Name: "michaelScott", Username: "michaelScott", Secret: "threat-level-midnight"}},
			Commands: []string{"vault login -method=userpass username=michaelScott password=threat-level-midnight"},
		})
	}

	if global.CheckContainer(engine, "hal-vault-mariadb") {
		services = append(services, ServiceCredentials{
			Service:  "vault-database",
			Label:    "Vault Database — dynamic credentials",
			Commands: []string{"vault read database/creds/writer", "vault read database/creds/reader"},
		})
	}

	if global.CheckContainer(engine, "hal-boundary") {
		boundary := ServiceCredentials{
			Service: "boundary",
			Label:   "Boundary",
			URL:     "http://boundary.localhost:9200",
			Entries: []CredentialEntry{{Name: "Admin", Username: "admin", Secret: "password"}},
		}
		if global.CheckContainer(engine, "hal-ssh-target") {
			boundary.Entries = append(boundary.Entries,
				CredentialEntry{Name: "ssh-operator", Username: "ssh-operator", Secret: "password", Note: "Boundary auth method: ssh-lab-auth"},
				CredentialEntry{Name: "ssh-auditor", Username: "ssh-auditor", Secret: "password", Note: "Boundary auth method: ssh-lab-auth"},
			)
		}
		if global.CheckContainer(engine, "hal-mariadb-target") {
			boundary.Entries = append(boundary.Entries,
				CredentialEntry{Name: "MariaDB target", Username: "admin", Secret: "password", Note: "host: localhost:3306"},
			)
		}
		services = append(services, boundary)
	}

	if global.CheckContainer(engine, "hal-tfe") {
		tfe := ServiceCredentials{
			Service: "tfe",
			Label:   "Terraform Enterprise",
			URL:     "https://tfe.localhost",
			Entries: []CredentialEntry{{Name: "Admin", Username: "haladmin", Secret: "hal9000FTW"}},
		}
		// The live TFE API token is intentionally NOT surfaced through this
		// structured collector (consumed by the MCP tool). It remains available
		// via `hal creds status` on the operator's own machine.
		tfe.Entries = append(tfe.Entries, CredentialEntry{Name: "API token", Note: "run `hal creds status` to retrieve"})
		services = append(services, tfe)
	}

	return ActiveCredentials{AnyActive: len(services) > 0, Services: services}, nil
}

// vaultAuthMountExists returns true if the named auth mount is enabled in Vault.
func vaultAuthMountExists(method string) bool {
	cfg := vault.DefaultConfig()
	if os.Getenv("VAULT_ADDR") == "" {
		cfg.Address = "http://127.0.0.1:8200"
	}
	client, err := vault.NewClient(cfg)
	if err != nil {
		return false
	}
	if os.Getenv("VAULT_TOKEN") == "" {
		client.SetToken("root")
	}
	auths, err := client.Sys().ListAuth()
	if err != nil {
		return false
	}
	_, ok := auths[strings.TrimSuffix(method, "/")+"/"]
	return ok
}

// loadAuthentikAdminPassword reads AUTHENTIK_BOOTSTRAP_PASSWORD from ~/.hal/authentik/env.
func loadAuthentikAdminPassword() string {
	data, err := os.ReadFile(integrations.AuthentikEnvPath())
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(parts) == 2 && parts[0] == "AUTHENTIK_BOOTSTRAP_PASSWORD" {
			return parts[1]
		}
	}
	return ""
}
