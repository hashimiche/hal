package boundary

import (
	"fmt"
	"os/exec"
	"strings"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var (
	mariadbEnable    bool
	mariadbDisable   bool
	mariadbUpdate    bool
	mariadbWithVault bool
	targetMariadbVer string
)

var mariadbCmd = &cobra.Command{
	Use:   "mariadb [status|enable|disable|update]",
	Short: "Deploy a dummy MariaDB Database as a Boundary Target",
	Run: func(cmd *cobra.Command, args []string) {
		if err := parseLifecycleAction(args, &mariadbEnable, &mariadbDisable, &mariadbUpdate); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		// 1. STATUS CHECK
		if !mariadbEnable && !mariadbDisable && !mariadbUpdate {
			out, err := exec.Command(engine, "inspect", "-f", "{{.State.Status}}", "hal-boundary-target-mariadb").Output()
			status := strings.TrimSpace(string(out))
			if err != nil {
				fmt.Println("❌ MariaDB Target: Not deployed. Run: hal boundary mariadb enable")
			} else {
				fmt.Printf("✅ MariaDB Target: %s\n", status)
			}
			return
		}

		// 2. TEARDOWN
		if mariadbDisable || mariadbUpdate {
			fmt.Println("🛑 Tearing down MariaDB and Boundary resources...")
			if global.DryRun {
				fmt.Println("[DRY RUN] Would clean Boundary MariaDB lab resources via API")
				fmt.Println("[DRY RUN] Would remove container hal-boundary-target-mariadb")
			} else {
				if err := cleanupBoundaryMariaDB(); err != nil {
					fmt.Printf("⚠️  Boundary cleanup warning: %v\n", err)
				}
				if err := exec.Command(engine, "rm", "-f", "hal-boundary-target-mariadb").Run(); err != nil {
					fmt.Printf("⚠️  Container cleanup warning: %v\n", err)
				}
			}
			if mariadbDisable {
				return
			}
		}

		// 3. DEPLOY
		if mariadbEnable || mariadbUpdate {
			dbContainerName := "hal-boundary-target-mariadb"
			if global.DryRun {
				if mariadbWithVault {
					dbContainerName = "hal-vault-mariadb"
					fmt.Println("[DRY RUN] Would attach Boundary target to existing hal-vault-mariadb")
				} else {
					fmt.Printf("[DRY RUN] Would create/start MariaDB container hal-boundary-target-mariadb (mariadb:%s) on hal-net\n", targetMariadbVer)
				}
				fmt.Printf("[DRY RUN] Would configure Boundary API for target host %s\n", dbContainerName)
				if mariadbWithVault {
					fmt.Println("[DRY RUN] Would link Boundary target to Vault dynamic credentials")
				}
				return
			}

			// Boundary Guardrail
			out, err := exec.Command(engine, "inspect", "-f", "{{.State.Status}}", "hal-boundary").Output()
			if err != nil || strings.TrimSpace(string(out)) != "running" {
				fmt.Println("❌ Error: Boundary controller is not running! Run: hal boundary create")
				return
			}

			if mariadbWithVault {
				// Vault Guardrail
				out, err = exec.Command(engine, "inspect", "-f", "{{.State.Status}}", "hal-vault").Output()
				if err != nil || strings.TrimSpace(string(out)) != "running" {
					fmt.Println("❌ Error: Vault is not running! Run: hal vault create && hal vault database enable")
					return
				}
				dbContainerName = "hal-vault-mariadb"
				fmt.Printf("🔗 Attaching Boundary to existing %s...\n", dbContainerName)
			} else {
				fmt.Println("🚀 Deploying standalone MariaDB...")
				global.EnsureNetwork(engine)
				out, err = exec.Command(engine, "run", "-d", "--name", dbContainerName, "--network", "hal-net", "-p", "3306:3306", "-e", "MARIADB_ROOT_PASSWORD=password", "-e", "MARIADB_DATABASE=targetdb", "-e", "MARIADB_USER=admin", "-e", "MARIADB_PASSWORD=password", fmt.Sprintf("mariadb:%s", targetMariadbVer)).CombinedOutput()
				if err != nil {
					fmt.Printf("❌ Failed to start MariaDB container: %v\n%s\n", err, strings.TrimSpace(string(out)))
					return
				}
			}

			// API Bootstrap
			err = bootstrapBoundaryMariaDB(dbContainerName)
			if err != nil {
				fmt.Printf("❌ Failed to configure Boundary API: %v\n", err)
				return
			}

			if mariadbWithVault {
				if err = linkBoundaryToVault(); err != nil {
					fmt.Printf("❌ Failed to link Boundary to Vault: %v\n", err)
					return
				}
			}
		}
	},
}

func init() {
	mariadbCmd.Flags().BoolVarP(&mariadbEnable, "enable", "e", false, "Deploy MariaDB")
	mariadbCmd.Flags().BoolVarP(&mariadbDisable, "disable", "d", false, "Remove MariaDB")
	mariadbCmd.Flags().BoolVarP(&mariadbUpdate, "update", "u", false, "Reconcile MariaDB target and Boundary target configuration")
	_ = mariadbCmd.Flags().MarkHidden("enable")
	_ = mariadbCmd.Flags().MarkHidden("disable")
	_ = mariadbCmd.Flags().MarkHidden("update")
	mariadbCmd.Flags().BoolVar(&mariadbWithVault, "with-vault", false, "Link with Vault Dynamic Creds")
	mariadbCmd.Flags().StringVar(&targetMariadbVer, "mariadb-version", "11.4", "Version")
	Cmd.AddCommand(mariadbCmd)
}

func bootstrapBoundaryMariaDB(targetHost string) error {
	fmt.Println("⚙️  Configuring Boundary Academy Lab...")
	client, err := newBoundaryAdminClient()
	if err != nil {
		return err
	}

	orgID, err := client.CreateOrGetResource("scopes", map[string]interface{}{"name": "hal-academy", "scope_id": "global"}, "name", map[string]string{"scope_id": "global"})
	if err != nil {
		return fmt.Errorf("failed to create org scope: %v", err)
	}

	projID, err := client.CreateOrGetResource("scopes", map[string]interface{}{"name": "db-infrastructure", "scope_id": orgID}, "name", map[string]string{"scope_id": orgID})
	if err != nil {
		return fmt.Errorf("failed to create project scope: %v", err)
	}

	catalogID, err := client.CreateOrGetResource("host-catalogs", map[string]interface{}{"name": "mariadb-catalog", "type": "static", "scope_id": projID}, "name", map[string]string{"scope_id": projID})
	if err != nil {
		return fmt.Errorf("failed to create host catalog: %v", err)
	}

	hostID, err := client.CreateOrGetResource("hosts", map[string]interface{}{"name": "mariadb-server", "type": "static", "address": targetHost, "host_catalog_id": catalogID}, "name", map[string]string{"host_catalog_id": catalogID})
	if err != nil {
		return fmt.Errorf("failed to create host: %v", err)
	}

	setID, err := client.CreateOrGetResource("host-sets", map[string]interface{}{"name": "mariadb-set", "type": "static", "host_catalog_id": catalogID}, "name", map[string]string{"host_catalog_id": catalogID})
	if err != nil {
		return fmt.Errorf("failed to create host set: %v", err)
	}
	if err := client.AddResourceAction("host-sets", setID, "add-hosts", map[string]interface{}{"host_ids": []string{hostID}}); err != nil && !isBoundaryAlreadyExists(err) {
		return fmt.Errorf("failed to add host to host set: %v", err)
	}

	targetID, err := client.CreateOrGetResource("targets", map[string]interface{}{"name": "mariadb-secure-access", "type": "tcp", "default_port": 3306, "scope_id": projID}, "name", map[string]string{"scope_id": projID})
	if err != nil {
		return fmt.Errorf("failed to create target: %v", err)
	}
	if err := client.AddResourceAction("targets", targetID, "add-host-sources", map[string]interface{}{"host_source_ids": []string{setID}}); err != nil && !isBoundaryAlreadyExists(err) {
		return fmt.Errorf("failed to link host set to target: %v", err)
	}

	labAuthID, err := client.CreateOrGetResource("auth-methods", map[string]interface{}{"name": "lab-auth", "type": "password", "scope_id": orgID}, "name", map[string]string{"scope_id": orgID})
	if err != nil {
		return fmt.Errorf("failed to create auth method: %v", err)
	}

	dbaAcctID, err := client.CreateOrGetResource("accounts", map[string]interface{}{"login_name": "dba-user", "password": "password", "type": "password", "auth_method_id": labAuthID}, "login_name", map[string]string{"auth_method_id": labAuthID})
	if err != nil {
		return fmt.Errorf("failed to create dba account: %v", err)
	}

	dbaUserID, err := client.CreateOrGetResource("users", map[string]interface{}{"name": "DBA Admin", "scope_id": orgID}, "name", map[string]string{"scope_id": orgID})
	if err != nil {
		return fmt.Errorf("failed to create dba user: %v", err)
	}
	if err := client.AddResourceAction("users", dbaUserID, "set-accounts", map[string]interface{}{"account_ids": []string{dbaAcctID}}); err != nil && !isBoundaryAlreadyExists(err) {
		return fmt.Errorf("failed to bind dba account to user: %v", err)
	}

	// DBA ROLE WITH CONNECT BUTTON UNLOCKED
	dbaRoleID, err := client.CreateOrGetResource("roles", map[string]interface{}{"name": "dba-role", "scope_id": projID}, "name", map[string]string{"scope_id": projID})
	if err != nil {
		return fmt.Errorf("failed to create dba role: %v", err)
	}
	if err := client.AddResourceAction("roles", dbaRoleID, "add-principals", map[string]interface{}{"principal_ids": []string{dbaUserID}}); err != nil && !isBoundaryAlreadyExists(err) {
		return fmt.Errorf("failed to add dba role principal: %v", err)
	}
	if err := client.AddResourceAction("roles", dbaRoleID, "add-grants", map[string]interface{}{
		"grant_strings": []string{"ids=*;type=*;actions=*"},
	}); err != nil && !isBoundaryAlreadyExists(err) {
		return fmt.Errorf("failed to grant dba role permissions: %v", err)
	}

	fmt.Println("✅ Boundary Academy Lab Bootstrapped!")
	return nil
}

func linkBoundaryToVault() error {
	fmt.Println("🔐 Linking Vault Brokering...")
	engine, err := global.DetectEngine()
	if err != nil {
		return err
	}

	policy := "path \"auth/token/lookup-self\" { capabilities = [\"read\"] }\npath \"auth/token/renew-self\" { capabilities = [\"update\"] }\npath \"auth/token/revoke-self\" { capabilities = [\"update\"] }\npath \"sys/leases/renew\" { capabilities = [\"update\"] }\npath \"sys/leases/revoke\" { capabilities = [\"update\"] }\npath \"sys/capabilities-self\" { capabilities = [\"update\"] }\npath \"database/creds/dba-role\" { capabilities = [\"read\", \"update\"] }"
	policyOut, err := exec.Command(engine, "exec", "-i", "-e", "VAULT_TOKEN=root", "-e", "VAULT_ADDR=http://127.0.0.1:8200", "hal-vault", "sh", "-c", fmt.Sprintf("echo '%s' | vault policy write boundary-controller -", policy)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to write vault policy: %v\n%s", err, strings.TrimSpace(string(policyOut)))
	}

	out, err := exec.Command(engine, "exec", "-e", "VAULT_TOKEN=root", "-e", "VAULT_ADDR=http://127.0.0.1:8200", "hal-vault", "vault", "token", "create", "-field=token", "-orphan", "-period=24h", "-policy=boundary-controller").Output()
	if err != nil {
		return fmt.Errorf("failed to create vault token: %v", err)
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return fmt.Errorf("failed to create vault token: empty token output")
	}

	client, err := newBoundaryAdminClient()
	if err != nil {
		return err
	}

	orgID, err := client.FindResourceIDByField("scopes", "name", "hal-academy", map[string]string{"scope_id": "global"})
	if err != nil {
		return fmt.Errorf("failed to find org scope: %v", err)
	}
	if orgID == "" {
		return fmt.Errorf("failed to find org scope hal-academy")
	}

	projID, err := client.FindResourceIDByField("scopes", "name", "db-infrastructure", map[string]string{"scope_id": orgID})
	if err != nil {
		return fmt.Errorf("failed to find project scope: %v", err)
	}
	if projID == "" {
		return fmt.Errorf("failed to find project scope db-infrastructure")
	}

	storeID, err := client.CreateOrGetResource("credential-stores", map[string]interface{}{"name": "hal-vault-store", "type": "vault", "scope_id": projID, "attributes": map[string]interface{}{"address": "http://hal-vault:8200", "token": token}}, "name", map[string]string{"scope_id": projID})
	if err != nil {
		return fmt.Errorf("failed to create vault credential store: %v", err)
	}

	libID, err := client.CreateOrGetResource("credential-libraries", map[string]interface{}{"name": "mariadb-dba-creds", "type": "vault-generic", "credential_store_id": storeID, "attributes": map[string]interface{}{"path": "database/creds/dba-role", "http_method": "GET"}}, "name", map[string]string{"credential_store_id": storeID})
	if err != nil {
		return fmt.Errorf("failed to create vault credential library: %v", err)
	}

	targetID, err := client.FindResourceIDByField("targets", "name", "mariadb-secure-access", map[string]string{"scope_id": projID})
	if err != nil {
		return fmt.Errorf("failed to find target: %v", err)
	}
	if targetID == "" {
		return fmt.Errorf("failed to find target mariadb-secure-access")
	}
	if err := client.AddResourceAction("targets", targetID, "add-credential-sources", map[string]interface{}{
		"brokered_credential_source_ids": []string{libID}}); err != nil && !isBoundaryAlreadyExists(err) {
		return fmt.Errorf("failed to set target credential sources: %v", err)
	}

	authID, err := client.FindResourceIDByField("auth-methods", "name", "lab-auth", map[string]string{"scope_id": orgID})
	if err != nil {
		return fmt.Errorf("failed to find auth method: %v", err)
	}
	if authID == "" {
		return fmt.Errorf("failed to find auth method lab-auth")
	}

	fmt.Printf("\n🖥️  Success! Run:\n1. BOUNDARY_AUTHENTICATE_PASSWORD_PASSWORD=password boundary authenticate password -addr http://boundary.localhost:9200 -auth-method-id %s -login-name dba-user\n2. boundary connect mysql -addr http://boundary.localhost:9200 -target-id %s\n", authID, targetID)
	return nil
}

func cleanupBoundaryMariaDB() error {
	client, err := newBoundaryAdminClient()
	if err != nil {
		return err
	}

	orgID, err := client.FindResourceIDByField("scopes", "name", "hal-academy", map[string]string{"scope_id": "global"})
	if err != nil {
		return err
	}
	if orgID == "" {
		return nil
	}
	projID, err := client.FindResourceIDByField("scopes", "name", "db-infrastructure", map[string]string{"scope_id": orgID})
	if err != nil {
		return err
	}

	// Helper to delete by name in scope
	del := func(ep, name, sidKey, sidVal string) {
		id, findErr := client.FindResourceIDByField(ep, "name", name, map[string]string{sidKey: sidVal})
		if findErr != nil {
			fmt.Printf("⚠️  Failed to find %s/%s: %v\n", ep, name, findErr)
			return
		}
		if id != "" {
			if delErr := client.DeleteResource(ep, id); delErr != nil && !isBoundaryNotFound(delErr) {
				fmt.Printf("⚠️  Failed to delete %s/%s: %v\n", ep, id, delErr)
			}
		}
	}

	if projID != "" {
		storeID, findErr := client.FindResourceIDByField("credential-stores", "name", "hal-vault-store", map[string]string{"scope_id": projID})
		if findErr != nil {
			return findErr
		}
		if storeID != "" {
			libID, libErr := client.FindResourceIDByField("credential-libraries", "name", "mariadb-dba-creds", map[string]string{"credential_store_id": storeID})
			if libErr != nil {
				return libErr
			}
			if libID != "" {
				if delErr := client.DeleteResource("credential-libraries", libID); delErr != nil && !isBoundaryNotFound(delErr) {
					fmt.Printf("⚠️  Failed to delete credential library %s: %v\n", libID, delErr)
				}
			}
			if delErr := client.DeleteResource("credential-stores", storeID); delErr != nil && !isBoundaryNotFound(delErr) {
				fmt.Printf("⚠️  Failed to delete credential store %s: %v\n", storeID, delErr)
			}
		}
		del("roles", "dba-role", "scope_id", projID)
		del("targets", "mariadb-secure-access", "scope_id", projID)
		catID, catErr := client.FindResourceIDByField("host-catalogs", "name", "mariadb-catalog", map[string]string{"scope_id": projID})
		if catErr != nil {
			return catErr
		}
		if catID != "" {
			del("host-sets", "mariadb-set", "host_catalog_id", catID)
			del("hosts", "mariadb-server", "host_catalog_id", catID)
			if delErr := client.DeleteResource("host-catalogs", catID); delErr != nil && !isBoundaryNotFound(delErr) {
				fmt.Printf("⚠️  Failed to delete host catalog %s: %v\n", catID, delErr)
			}
		}
		if delErr := client.DeleteResource("scopes", projID); delErr != nil && !isBoundaryNotFound(delErr) {
			fmt.Printf("⚠️  Failed to delete project scope %s: %v\n", projID, delErr)
		}
	}
	del("auth-methods", "lab-auth", "scope_id", orgID)
	if delErr := client.DeleteResource("scopes", orgID); delErr != nil && !isBoundaryNotFound(delErr) {
		fmt.Printf("⚠️  Failed to delete org scope %s: %v\n", orgID, delErr)
	}
	return nil
}
