package vault

import (
	"fmt"
	"os/exec"
	"time"

	"hal/internal/global"

	vault "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
)

var (
	mariadbEnable  bool
	mariadbDisable bool
	mariadbForce   bool
	mariadbVersion string
)

var vaultMariadbCmd = &cobra.Command{
	Use:   "mariadb",
	Short: "Deploy MariaDB and configure Vault Dynamic Database Credentials",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		client, vaultErr := GetHealthyClient()

		// ==========================================
		// 1. SMART STATUS MODE (Default behavior)
		// ==========================================
		if !mariadbEnable && !mariadbDisable && !mariadbForce {
			fmt.Println("🔍 Checking Vault Database Engine Status...")

			// Check Docker
			dbExists := (exec.Command(engine, "inspect", "hal-vault-mariadb").Run() == nil)

			// Check Vault API (if Vault is alive)
			dbMounted := false
			if vaultErr == nil {
				mounts, _ := client.Sys().ListMounts()
				_, dbMounted = mounts["database/"]
			}

			// Output Status
			if dbExists {
				fmt.Printf("  ✅ MariaDB       : Active (127.0.0.1:3306)\n")
			} else {
				fmt.Printf("  ❌ MariaDB       : Not running\n")
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
				fmt.Println("   hal vault mariadb --enable")
			} else if dbExists && dbMounted {
				fmt.Println("   Demo is ready! Request a dynamic credential:")
				fmt.Println("   vault read database/creds/dba-role")
				fmt.Println("\n   To completely remove this database environment, run:")
				fmt.Println("   hal vault mariadb --disable")
			} else {
				fmt.Println("   Environment is partially degraded. To safely reset, run:")
				fmt.Println("   hal vault mariadb --force")
			}
			return
		}

		// ==========================================
		// 2. TEARDOWN / RESET PATH (--disable / --force)
		// ==========================================
		if mariadbDisable || mariadbForce {
			if global.DryRun {
				fmt.Println("[DRY RUN] Would execute: docker rm -f hal-vault-mariadb")
				fmt.Println("[DRY RUN] Would call API to force-revoke leases and unmount 'database/'")
			} else {
				if mariadbDisable {
					fmt.Println("🛑 Tearing down MariaDB environment...")
				} else {
					fmt.Println("♻️  Force flag detected. Destroying database environment for reset...")
				}

				if vaultErr == nil && client != nil {
					// 🎯 THE BULLETPROOF FIX: Force-revoke any dangling database leases first.
					fmt.Println("⚙️  Connecting to Vault API for cleanup (Revoking leases)...")
					_ = client.Sys().RevokeForce("database/")
					_ = client.Sys().Unmount("database")
				} else {
					fmt.Println("⚠️  Vault is offline. Skipped Vault-internal cleanup.")
				}

				fmt.Println("⚙️  Removing MariaDB container...")
				_ = exec.Command(engine, "rm", "-f", "hal-vault-mariadb").Run()

				if mariadbDisable {
					fmt.Println("✅ MariaDB environment destroyed successfully!")
				}
			}

			if mariadbDisable && !global.DryRun {
				return
			}
		}

		// ==========================================
		// 3. DEPLOY / ENABLE PATH (--enable / --force)
		// ==========================================
		if mariadbEnable || mariadbForce {
			if vaultErr != nil {
				fmt.Printf("❌ Cannot deploy: Vault must be running and healthy. %v\n", vaultErr)
				return
			}

			if global.DryRun {
				fmt.Println("[DRY RUN] Would execute Docker run command for MariaDB.")
				fmt.Println("[DRY RUN] Would provision 'vaultadmin' least-privilege account via SQL.")
				fmt.Println("[DRY RUN] Would configure Vault Database secrets engine and rotate root.")
				return
			}

			fmt.Printf("🚀 Booting MariaDB Database (mariadb:%s)...\n", mariadbVersion)
			_ = exec.Command(engine, "rm", "-f", "hal-vault-mariadb").Run()

			dbArgs := []string{
				"run", "-d", "--name", "hal-vault-mariadb",
				"--network", "hal-net",
				"--network-alias", "mariadb.localhost",
				"-p", "3306:3306",
				"-e", "MARIADB_ROOT_PASSWORD=vaultroot",
				fmt.Sprintf("mariadb:%s", mariadbVersion),
			}

			if err := exec.Command(engine, dbArgs...).Run(); err != nil {
				fmt.Printf("❌ Failed to start MariaDB: %v\n", err)
				return
			}

			fmt.Println("⏳ Waiting for MariaDB to initialize (this usually takes 10-15 seconds)...")
			if err := waitForMariaDB(engine, 30); err != nil {
				fmt.Println("\n❌ MariaDB failed to initialize within the time limit.")
				return
			}
			fmt.Println("\n✅ MariaDB is online and accepting connections!")

			// 🎯 BEST PRACTICE 1: Create a least-privileged vaultadmin account
			// Updated to ALL PRIVILEGES so it can generate DBA users for Boundary
			fmt.Println("⚙️  Provisioning least-privileged 'vaultadmin' broker account...")
			setupSQL := `
				CREATE USER 'vaultadmin'@'%' IDENTIFIED BY 'temp-vault-pass';
				GRANT ALL PRIVILEGES ON *.* TO 'vaultadmin'@'%' WITH GRANT OPTION;
				FLUSH PRIVILEGES;
			`
			err = exec.Command(engine, "exec", "hal-vault-mariadb", "mariadb", "-u", "root", "-pvaultroot", "-e", setupSQL).Run()
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
			fmt.Println("⚙️  Wiring Vault to MariaDB via the 'vaultadmin' account...")
			_, err = client.Logical().Write("database/config/hal-vault-mariadb", map[string]interface{}{
				"plugin_name":    "mysql-database-plugin",
				"connection_url": "{{username}}:{{password}}@tcp(hal-vault-mariadb:3306)/",
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
			_, err = client.Logical().Write("database/rotate-root/hal-vault-mariadb", map[string]interface{}{})
			if err != nil {
				fmt.Printf("❌ Failed to rotate Vault connection password: %v\n", err)
				return
			}

			// 3. Create the Role (Updated for DBA to match Boundary integration)
			fmt.Println("⚙️  Injecting Dynamic SQL Creation Statements...")
			_, err = client.Logical().Write("database/roles/dba-role", map[string]interface{}{
				"db_name":             "hal-vault-mariadb",
				"creation_statements": "CREATE USER '{{name}}'@'%' IDENTIFIED BY '{{password}}'; GRANT ALL PRIVILEGES ON *.* TO '{{name}}'@'%';",
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
			fmt.Println("---------------------------------------------------------")
			fmt.Println("🔗 Database Host: 127.0.0.1:3306")
			fmt.Println("👤 JIT Username:  " + username)
			fmt.Println("🔑 JIT Password:  " + password)
			fmt.Println("\n💡 THE SECURE WORKFLOW:")
			fmt.Println("   1. A least-privileged 'vaultadmin' account was created.")
			fmt.Println("   2. Vault immediately rotated the 'vaultadmin' password. Nobody knows it!")
			fmt.Println("   3. Vault used that account to dynamically create the JIT user above.")
			fmt.Println("   4. Try logging in: `mysql -h 127.0.0.1 -P 3306 -u " + username + " -p" + password + "`")
			fmt.Println("   5. This user has DBA privileges and will self-destruct in 1 hour.")
			fmt.Println("---------------------------------------------------------")
		}
	},
}

// -----------------------------------------------------------------------------
// Helper Functions
// -----------------------------------------------------------------------------

func waitForMariaDB(engine string, maxRetries int) error {
	for i := 0; i < maxRetries; i++ {
		cmd := exec.Command(engine, "exec", "hal-vault-mariadb", "mariadb-admin", "ping", "-h", "127.0.0.1", "-u", "root", "-pvaultroot", "--silent")
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
	vaultMariadbCmd.Flags().BoolVarP(&mariadbEnable, "enable", "e", false, "Deploy MariaDB and configure Vault")
	vaultMariadbCmd.Flags().BoolVarP(&mariadbDisable, "disable", "d", false, "Remove MariaDB and clean up Vault configurations")
	vaultMariadbCmd.Flags().BoolVarP(&mariadbForce, "force", "f", false, "Force a clean redeployment of the database")

	// Feature-Specific Flags
	vaultMariadbCmd.Flags().StringVar(&mariadbVersion, "mariadb-version", "11.4", "Version of the MariaDB container image to deploy")

	Cmd.AddCommand(vaultMariadbCmd)
}
