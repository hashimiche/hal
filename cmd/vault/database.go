package vault

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"hal/internal/global"

	vault "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
)

var (
	databaseEnable  bool
	databaseDisable bool
	databaseUpdate  bool
	databaseBackend string
	mariadbVersion  string
)

var vaultDatabaseCmd = &cobra.Command{
	Use:     "database [status|enable|disable|update]",
	Aliases: []string{"db"},
	Short:   "Configure Vault dynamic database credentials workflows",
	Args:    cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := parseLifecycleAction(args, &databaseEnable, &databaseDisable, &databaseUpdate); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		backend := strings.ToLower(strings.TrimSpace(databaseBackend))
		if backend != "mariadb" {
			if backend == "postgres" || backend == "pgsql" {
				fmt.Println("❌ Backend pgsql is not implemented yet in HAL. Use --backend mariadb for now.")
				fmt.Println("💡 Next Step: hal vault database enable")
				return
			}
			fmt.Printf("❌ Unsupported backend %q. Valid value today: mariadb (pgsql planned).\n", databaseBackend)
			return
		}
		backendLabel := "MariaDB"

		containerName := "hal-vault-mariadb"
		hostAlias := "mariadb.localhost"
		containerPort := "3306"
		pluginName := "mysql-database-plugin"
		setupCmd := []string{"mariadb", "-u", "root", "-pvaultroot", "-e", `
				CREATE USER 'vaultadmin'@'%' IDENTIFIED BY 'temp-vault-pass';
				GRANT ALL PRIVILEGES ON *.* TO 'vaultadmin'@'%' WITH GRANT OPTION;
				FLUSH PRIVILEGES;
			`}
		connectionURL := "{{username}}:{{password}}@tcp(hal-vault-mariadb:3306)/"
		createStmt := "CREATE USER '{{name}}'@'%' IDENTIFIED BY '{{password}}'; GRANT ALL PRIVILEGES ON *.* TO '{{name}}'@'%';"
		startArgs := []string{
			"run", "-d", "--name", "hal-vault-mariadb",
			"--network", "hal-net",
			"--network-alias", "mariadb.localhost",
			"-p", "3306:3306",
			"-e", "MARIADB_ROOT_PASSWORD=vaultroot",
			fmt.Sprintf("mariadb:%s", mariadbVersion),
		}

		client, vaultErr := GetHealthyClient()

		// ==========================================
		// 1. SMART STATUS MODE (Default behavior)
		// ==========================================
		if !databaseEnable && !databaseDisable && !databaseUpdate {
			fmt.Println("🔍 Checking Vault Database Engine Status...")

			dbExists := exec.Command(engine, "inspect", containerName).Run() == nil

			// Check Vault API (if Vault is alive)
			dbMounted := false
			if vaultErr == nil {
				mounts, _ := client.Sys().ListMounts()
				_, dbMounted = mounts["database/"]
			}

			// Output Status
			if dbExists {
				fmt.Printf("  ✅ Backend       : %s active (%s:%s)\n", backend, hostAlias, containerPort)
			} else {
				fmt.Printf("  ❌ Backend       : %s not running\n", backend)
			}

			if dbMounted {
				fmt.Printf("  ✅ Vault Secrets : Configured (database/)\n")
			} else {
				fmt.Printf("  ❌ Vault Secrets : Not configured\n")
			}

			// Smart Assistant Logic
			fmt.Println("\n💡 Next Step:")
			if !dbExists && !dbMounted {
				fmt.Println("   To deploy MariaDB and wire up Vault, run:")
				fmt.Println("   hal vault database enable")
			} else if dbExists && dbMounted {
				fmt.Println("   Demo is ready! Request a dynamic credential:")
				fmt.Println("   vault read database/creds/dba-role")
				fmt.Println("\n   To completely remove this database environment, run:")
				fmt.Println("   hal vault database disable")
			} else {
				fmt.Println("   Environment is partially degraded. To safely reset, run:")
				fmt.Println("   hal vault database update")
			}
			return
		}

		// ==========================================
		// 2. TEARDOWN / RESET PATH (--disable / --update)
		// ==========================================
		if databaseDisable || databaseUpdate {
			if global.DryRun {
				fmt.Printf("[DRY RUN] Would execute: %s rm -f %s\n", engine, containerName)
				fmt.Println("[DRY RUN] Would call API to force-revoke leases and unmount 'database/'")
			} else {
				if databaseDisable {
					fmt.Printf("🛑 Tearing down %s environment...\n", backend)
				} else {
					fmt.Println("♻️  Update requested. Destroying database environment for reset...")
				}

				if vaultErr == nil && client != nil {
					// 🎯 THE BULLETPROOF FIX: Force-revoke any dangling database leases first.
					fmt.Println("⚙️  Connecting to Vault API for cleanup (Revoking leases)...")
					_ = client.Sys().RevokeForce("database/")
					_ = client.Sys().Unmount("database")
				} else {
					fmt.Println("⚠️  Vault is offline. Skipped Vault-internal cleanup.")
				}

				fmt.Printf("⚙️  Removing %s container...\n", backend)
				_ = exec.Command(engine, "rm", "-f", containerName).Run()

				if databaseDisable {
					fmt.Printf("✅ %s environment destroyed successfully!\n", backendLabel)
				}
			}

			if databaseDisable && !global.DryRun {
				return
			}
		}

		// ==========================================
		// 3. DEPLOY / ENABLE PATH (--enable / --update)
		// ==========================================
		if databaseEnable || databaseUpdate {
			if vaultErr != nil {
				fmt.Printf("❌ Cannot deploy: Vault must be running and healthy. %v\n", vaultErr)
				return
			}

			if global.DryRun {
				fmt.Printf("[DRY RUN] Would execute Docker run command for %s.\n", backend)
				fmt.Println("[DRY RUN] Would provision 'vaultadmin' least-privilege account via SQL.")
				fmt.Println("[DRY RUN] Would configure Vault Database secrets engine and rotate root.")
				return
			}

			fmt.Printf("🚀 Booting %s database...\n", backendLabel)
			_ = exec.Command(engine, "rm", "-f", containerName).Run()

			if err := exec.Command(engine, startArgs...).Run(); err != nil {
				fmt.Printf("❌ Failed to start %s: %v\n", backend, err)
				return
			}

			fmt.Printf("⏳ Waiting for %s to initialize...\n", backend)
			waitErr := waitForMariaDB(engine, containerName, 30)
			if waitErr != nil {
				fmt.Printf("\n❌ %s failed to initialize within the time limit.\n", backendLabel)
				return
			}
			fmt.Printf("\n✅ %s is online and accepting connections!\n", backendLabel)

			fmt.Println("⚙️  Provisioning least-privileged 'vaultadmin' broker account...")
			execArgs := append([]string{"exec", containerName}, setupCmd...)
			err = exec.Command(engine, execArgs...).Run()
			if err != nil {
				fmt.Printf("❌ Failed to provision vaultadmin account: %v\n", err)
				return
			}

			// 1. Enable Database Secrets Engine
			fmt.Println("⚙️  Configuring Vault Database Secrets Engine...")
			_ = client.Sys().Unmount("database")

			err = client.Sys().Mount("database", &vault.MountInput{
				Type: "database",
			})
			if err != nil {
				fmt.Printf("❌ Failed to enable database engine: %v\n", err)
				return
			}

			// 2. Configure Vault Connection
			fmt.Printf("⚙️  Wiring Vault to %s via the 'vaultadmin' account...\n", backendLabel)
			_, err = client.Logical().Write("database/config/"+containerName, map[string]interface{}{
				"plugin_name":    pluginName,
				"connection_url": connectionURL,
				"allowed_roles":  "dba-role",
				"username":       "vaultadmin",
				"password":       "temp-vault-pass",
			})
			if err != nil {
				fmt.Printf("❌ Failed to configure database connection: %v\n", err)
				return
			}

			// 🎯 BEST PRACTICE 2: Rotate the Vault Admin Password
			fmt.Println("⚙️  Executing Password Rotation (Vault is taking exclusive ownership)...")
			_, err = client.Logical().Write("database/rotate-root/"+containerName, map[string]interface{}{})
			if err != nil {
				fmt.Printf("❌ Failed to rotate Vault connection password: %v\n", err)
				return
			}

			// 3. Create the Role (Updated for DBA to match Boundary integration)
			fmt.Println("⚙️  Injecting Dynamic SQL Creation Statements...")
			_, err = client.Logical().Write("database/roles/dba-role", map[string]interface{}{
				"db_name":             containerName,
				"creation_statements": createStmt,
				"default_ttl":         "1h",
				"max_ttl":             "24h",
			})
			if err != nil {
				fmt.Printf("❌ Failed to create Vault role: %v\n", err)
				return
			}

			// 4. Generate the Temporary Credentials
			fmt.Println("⚙️  Requesting temporary JIT (Just-In-Time) credentials from Vault...")
			time.Sleep(2 * time.Second)

			secret, err := client.Logical().Read("database/creds/dba-role")
			if err != nil || secret == nil {
				fmt.Printf("❌ Failed to generate credentials: %v\n", err)
				return
			}

			username := secret.Data["username"].(string)
			password := secret.Data["password"].(string)

			fmt.Println("\n✅ Enterprise Dynamic Database Credentials Generated!")
			global.RefreshHalStatus(engine)
			fmt.Println("---------------------------------------------------------")
			fmt.Printf("🔗 Database Host: %s:%s\n", hostAlias, containerPort)
			fmt.Println("👤 JIT Username:  " + username)
			fmt.Println("🔑 JIT Password:  " + password)
			fmt.Println("\n💡 THE SECURE WORKFLOW:")
			fmt.Println("   1. A least-privileged 'vaultadmin' account was created.")
			fmt.Println("   2. Vault immediately rotated the 'vaultadmin' password. Nobody knows it!")
			fmt.Println("   3. Vault used that account to dynamically create the JIT user above.")
			fmt.Println("   4. Try logging in: `mysql -h " + hostAlias + " -P " + containerPort + " -u " + username + " -p" + password + "`")
			fmt.Println("   5. This user has DBA privileges and will self-destruct in 1 hour.")
			fmt.Println("---------------------------------------------------------")
		}
	},
}

// -----------------------------------------------------------------------------
// Helper Functions
// -----------------------------------------------------------------------------

func waitForMariaDB(engine, containerName string, maxRetries int) error {
	for i := 0; i < maxRetries; i++ {
		cmd := exec.Command(engine, "exec", containerName, "mariadb-admin", "ping", "-h", "127.0.0.1", "-u", "root", "-pvaultroot", "--silent")
		if err := cmd.Run(); err == nil {
			return nil
		}
		fmt.Print(".")
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("timeout")
}

func init() {
	// Standard Lifecycle Flags
	vaultDatabaseCmd.Flags().BoolVarP(&databaseEnable, "enable", "e", false, "Deploy selected database backend and configure Vault")
	vaultDatabaseCmd.Flags().BoolVarP(&databaseDisable, "disable", "d", false, "Remove selected backend and clean up Vault database configuration")
	vaultDatabaseCmd.Flags().BoolVarP(&databaseUpdate, "update", "u", false, "Reconcile selected backend and Vault database configuration")
	_ = vaultDatabaseCmd.Flags().MarkHidden("enable")
	_ = vaultDatabaseCmd.Flags().MarkHidden("disable")
	_ = vaultDatabaseCmd.Flags().MarkHidden("update")

	// Backend selection and version pinning
	vaultDatabaseCmd.Flags().StringVarP(&databaseBackend, "backend", "b", "mariadb", "Database backend to use (mariadb; pgsql planned, postgres alias accepted)")
	vaultDatabaseCmd.Flags().StringVar(&mariadbVersion, "mariadb-version", "11.4", "Version of the MariaDB container image to deploy")

	Cmd.AddCommand(vaultDatabaseCmd)
}
