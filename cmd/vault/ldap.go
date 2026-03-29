package vault

import (
	"fmt"
	"hal/internal/global"
	"os"
	"os/exec"
	"strings"
	"time"

	vault "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
)

var (
	ldapDestroy bool
	ldapForce   bool
)

var vaultLdapCmd = &cobra.Command{
	Use:   "ldap",
	Short: "Deploy OpenLDAP and configure Vault Auth & Secrets engines",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		client, vaultErr := GetHealthyClient()

		if !ldapDestroy && !ldapForce && vaultErr != nil {
			fmt.Printf("❌ %v\n", vaultErr)
			return
		}

		// ==========================================
		// THE DESTROY LOGIC (--destroy OR --force)
		// ==========================================
		if ldapDestroy || ldapForce {
			// 🎯 FIX 1: Vault Cleanup MUST happen BEFORE killing the containers!
			if vaultErr == nil && client != nil {
				fmt.Println("⚙️  Connecting to Vault API for cleanup (Revoking leases)...")
				_ = client.Sys().RevokePrefix("ldap/") // Force wipe all active leases first
				_ = client.Sys().DisableAuth("ldap")
				_ = client.Sys().Unmount("ldap")
				_ = client.Sys().Unmount("kv-ldap")
				_ = client.Sys().DeletePolicy("ldap-admin")
				_ = client.Sys().DeletePolicy("ldap-reader")
			} else {
				fmt.Println("⚠️  Vault is offline. Skipped Vault-internal cleanup.")
			}

			fmt.Println("⚙️  Removing OpenLDAP and phpLDAPadmin containers...")
			_ = exec.Command(engine, "rm", "-f", "hal-openldap", "hal-phpldapadmin").Run()

			homeDir, _ := os.UserHomeDir()
			if homeDir != "" {
				_ = os.Remove(homeDir + "/.vault-token")
			}

			if ldapDestroy {
				fmt.Println("✅ LDAP environment destroyed successfully!")
				return
			}
			fmt.Println("♻️  Force cleanup complete. Proceeding with fresh deployment...")
		}

		// ==========================================
		// THE DEPLOY LOGIC
		// ==========================================
		global.EnsureNetwork(engine)

		fmt.Println("⚙️  Booting OpenLDAP Directory Server...")
		_ = exec.Command(engine, "rm", "-f", "hal-openldap").Run()
		err = exec.Command(engine, "run", "-d",
			"--name", "hal-openldap",
			"--network", "hal-net",
			"-p", "1389:389",
			"--platform", "linux/amd64",
			"-e", "LDAP_ORGANISATION=HAL9000",
			"-e", "LDAP_DOMAIN=hal.local",
			"-e", "LDAP_ADMIN_PASSWORD=admin",
			"-e", "LDAP_READONLY_USER=true", // 🎯 Automatically configures global read ACLs
			"-e", "LDAP_READONLY_USER_USERNAME=vault-auth", // 🎯 Creates cn=vault-auth,dc=hal,dc=local
			"-e", "LDAP_READONLY_USER_PASSWORD=authpass",
			"-e", "LDAP_TLS=false",
			"osixia/openldap:1.5.0",
		).Run()

		if err != nil {
			fmt.Printf("❌ Failed to start OpenLDAP: %v\n", err)
			return
		}

		fmt.Println("⚙️  Booting phpLDAPadmin UI...")
		_ = exec.Command(engine, "rm", "-f", "hal-phpldapadmin").Run()
		_ = exec.Command(engine, "run", "-d",
			"--name", "hal-phpldapadmin",
			"--network", "hal-net",
			"-p", "8082:443",
			"--platform", "linux/amd64",
			"-e", "PHPLDAPADMIN_LDAP_HOSTS=hal-openldap",
			"-e", "PHPLDAPADMIN_HTTPS=true",
			"osixia/phpldapadmin:0.9.0",
		).Run()

		fmt.Println("⚙️  Seeding LDAP Users, Groups, and Service Accounts...")
		seedLDAP(engine)

		fmt.Println("⚙️  Configuring Vault Policies and KV Engine...")

		adminPolicy := `
path "kv-ldap/*" { capabilities = ["create", "read", "update", "delete", "list"] }
path "ldap/*" { capabilities = ["create", "read", "update", "delete", "list"] }
`
		readerPolicy := `
path "kv-ldap/data/secret" { capabilities = ["read"] }
path "ldap/creds/dynamic-reader" { capabilities = ["read", "update"] }
path "ldap/static-cred/static-app" { capabilities = ["read"] }
path "ldap/library/dev-pool/check-out" { capabilities = ["create", "update"] }
path "ldap/library/dev-pool/check-in" { capabilities = ["create", "update"] }
`
		_ = client.Sys().PutPolicy("ldap-admin", adminPolicy)
		_ = client.Sys().PutPolicy("ldap-reader", readerPolicy)

		_ = client.Sys().Mount("kv-ldap", &vault.MountInput{Type: "kv", Options: map[string]string{"version": "2"}})
		_, _ = client.Logical().Write("kv-ldap/data/secret", map[string]interface{}{
			"data": map[string]interface{}{"mission": "Jupiter Monolith Investigation"},
		})

		// 5. Enable LDAP Auth Method & Native Group Mapping
		fmt.Println("⚙️  Configuring Vault LDAP Auth Engine...")
		_ = client.Sys().EnableAuthWithOptions("ldap", &vault.EnableAuthOptions{Type: "ldap"})

		// 🎯 Use the auto-generated Readonly ACL account
		_, _ = client.Logical().Write("auth/ldap/config", map[string]interface{}{
			"url":          "ldap://hal-openldap",
			"binddn":       "cn=vault-auth,dc=hal,dc=local", // 🎯 Notice it is at the root DC now
			"bindpass":     "authpass",
			"userdn":       "ou=users,dc=hal,dc=local",
			"userattr":     "cn",
			"groupdn":      "ou=groups,dc=hal,dc=local",
			"insecure_tls": true,
			"starttls":     false,
		})

		_, _ = client.Logical().Write("auth/ldap/groups/admin", map[string]interface{}{"policies": "ldap-admin"})
		_, _ = client.Logical().Write("auth/ldap/groups/reader", map[string]interface{}{"policies": "ldap-reader"})

		// 6. Enable LDAP Secrets Engine
		fmt.Println("⚙️  Configuring Vault LDAP Secrets Engine...")
		_ = client.Sys().Mount("ldap", &vault.MountInput{Type: "ldap"})

		_, _ = client.Logical().Write("ldap/config", map[string]interface{}{
			"url":          "ldap://hal-openldap",
			"binddn":       "cn=admin,dc=hal,dc=local",
			"bindpass":     "admin",
			"userdn":       "ou=users,dc=hal,dc=local",
			"insecure_tls": true,
		})

		_, _ = client.Logical().Write("ldap/role/dynamic-reader", map[string]interface{}{
			"creation_ldif": `dn: cn={{.Username}},ou=users,dc=hal,dc=local
objectClass: top
objectClass: person
objectClass: organizationalPerson
objectClass: inetOrgPerson
cn: {{.Username}}
sn: Ephemeral
userPassword: {{.Password}}`,
			"deletion_ldif": "dn: cn={{.Username}},ou=users,dc=hal,dc=local",
			"default_ttl":   "1h",
		})

		_, _ = client.Logical().Write("ldap/static-role/static-app", map[string]interface{}{
			"dn":              "cn=app-service,ou=users,dc=hal,dc=local",
			"username":        "app-service",
			"rotation_period": "24h",
		})

		_, _ = client.Logical().Write("ldap/library/dev-pool", map[string]interface{}{
			"service_account_names": []string{"lib-service-1", "lib-service-2"},
			"ttl":                   "1h",
			"max_ttl":               "24h",
		})

		// 🎯 The ultimate payoff: Secret Zero Rotation.
		fmt.Println("⚙️  Rotating OpenLDAP root password (Vault now owns it exclusively)...")
		_, _ = client.Logical().Write("ldap/rotate-root", map[string]interface{}{})

		fmt.Println("\n✅ LDAP Infrastructure & Vault Integration Complete!")
		fmt.Println("---------------------------------------------------------")
		fmt.Println("🔗 phpLDAPadmin UI: https://localhost:8082")
		fmt.Println("   Login DN: cn=admin,dc=hal,dc=local")
		fmt.Println("   Password: (UNKNOWN! Vault rotated the root password!)")
		fmt.Println("\n👤 Try logging into Vault as Bob (Reader):")
		fmt.Println("   vault login -method=ldap username=bob password=bobpass")
		fmt.Println("\n💡 As Bob, run these LDAP Secret Engine commands:")
		fmt.Println("   1. vault read ldap/creds/dynamic-reader")
		fmt.Println("   2. vault read ldap/static-cred/static-app")
		fmt.Println("   3. vault write -f ldap/library/dev-pool/check-out")
		fmt.Println("---------------------------------------------------------")
	},
}

func seedLDAP(engine string) {
	// 🎯 Materialize the Admin Ghost Account at the very top so Vault can find it and rotate it
	ldif := `dn: cn=admin,dc=hal,dc=local
objectClass: simpleSecurityObject
objectClass: organizationalRole
cn: admin
description: LDAP administrator
userPassword: admin

dn: ou=groups,dc=hal,dc=local
objectClass: organizationalUnit
objectClass: top
ou: groups

dn: ou=users,dc=hal,dc=local
objectClass: organizationalUnit
objectClass: top
ou: users

dn: cn=alice,ou=users,dc=hal,dc=local
objectClass: top
objectClass: person
objectClass: organizationalPerson
objectClass: inetOrgPerson
cn: alice
sn: Admin
userPassword: alicepass

dn: cn=bob,ou=users,dc=hal,dc=local
objectClass: top
objectClass: person
objectClass: organizationalPerson
objectClass: inetOrgPerson
cn: bob
sn: Reader
userPassword: bobpass

dn: cn=app-service,ou=users,dc=hal,dc=local
objectClass: top
objectClass: person
objectClass: organizationalPerson
objectClass: inetOrgPerson
cn: app-service
sn: Service
userPassword: initialpass

dn: cn=lib-service-1,ou=users,dc=hal,dc=local
objectClass: top
objectClass: person
objectClass: organizationalPerson
objectClass: inetOrgPerson
cn: lib-service-1
sn: Service
userPassword: initialpass

dn: cn=lib-service-2,ou=users,dc=hal,dc=local
objectClass: top
objectClass: person
objectClass: organizationalPerson
objectClass: inetOrgPerson
cn: lib-service-2
sn: Service
userPassword: initialpass

dn: cn=admin,ou=groups,dc=hal,dc=local
objectClass: groupOfNames
objectClass: top
cn: admin
member: cn=alice,ou=users,dc=hal,dc=local

dn: cn=reader,ou=groups,dc=hal,dc=local
objectClass: groupOfNames
objectClass: top
cn: reader
member: cn=bob,ou=users,dc=hal,dc=local
`
	ldifClean := strings.ReplaceAll(ldif, "\r", "")

	tmpFile := "/tmp/hal_seed.ldif"
	_ = os.WriteFile(tmpFile, []byte(ldifClean), 0644)
	defer os.Remove(tmpFile)

	_ = exec.Command(engine, "cp", tmpFile, "hal-openldap:/tmp/seed.ldif").Run()

	fmt.Print("⚙️  Waiting for OpenLDAP to accept connections")

	for i := 0; i < 10; i++ {
		// 🎯 Added the '-c' flag (continue on errors) so it safely ignores if OpenLDAP already created the admin object
		out, err := exec.Command(engine, "exec", "hal-openldap", "ldapadd", "-c", "-x", "-D", "cn=admin,dc=hal,dc=local", "-w", "admin", "-H", "ldap://localhost", "-f", "/tmp/seed.ldif").CombinedOutput()
		if err == nil {
			fmt.Println("\n✅ LDAP Directory seeded successfully.")
			return
		}
		if global.Debug {
			fmt.Printf("\n[DEBUG] LDAP Retry %d failed: %s\n", i, string(out))
		}
		fmt.Print(".")
		time.Sleep(3 * time.Second)
	}
	fmt.Println("\n❌ Fatal: Failed to seed LDAP after 30 seconds.")
	os.Exit(1)
}

func init() {
	vaultLdapCmd.Flags().BoolVarP(&ldapDestroy, "destroy", "d", false, "Destroy the LDAP infrastructure and configurations")
	vaultLdapCmd.Flags().BoolVarP(&ldapForce, "force", "f", false, "Force a clean redeployment")
	Cmd.AddCommand(vaultLdapCmd)
}
