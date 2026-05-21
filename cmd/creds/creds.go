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
			fmt.Println("   Username : admin")
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
