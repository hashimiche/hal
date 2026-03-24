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
	mariadbDestroy bool
	mariadbVersion string
)

var vaultMariadbCmd = &cobra.Command{
	Use:   "mariadb",
	Short: "Deploy MariaDB and configure Vault Dynamic Database Credentials using least-privilege best practices",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		// 1. Try to get the client
		client, vaultErr := GetHealthyClient()

		// 2. If we are DEPLOYING, we demand Vault is healthy.
		if !mariadbDestroy && vaultErr != nil {
			fmt.Printf("❌ %v\n", vaultErr)
			return
		}

		// ==========================================
		// THE DESTROY LOGIC (--destroy)
		// ==========================================
		if mariadbDestroy {
			fmt.Println("⚙️  Connecting to Vault API for cleanup...")
			// Only attempt Vault cleanup if Vault is actually alive
			if vaultErr == nil && client != nil {

				// 🎯 THE BULLETPROOF FIX: Force-revoke any dangling database leases first.
				// This ensures Vault doesn't panic and block the unmount if the database is already offline!
				fmt.Println("⚙️  Force-revoking dangling database leases...")
				_ = client.Sys().RevokeForce("database/")

				fmt.Println("⚙️  Disabling 'database/' secrets engine...")
				_ = client.Sys().Unmount("database")

			} else {
				fmt.Println("⚠️  Vault is offline. Skipped Vault-internal cleanup.")
			}

			fmt.Println("⚙️  Removing MariaDB container...")
			_ = exec.Command(engine, "rm", "-f", "hal-mariadb").Run()

			fmt.Println("✅ MariaDB environment destroyed successfully!")
			return
		}

		// ==========================================
		// THE DEPLOY LOGIC (Default)
		// ==========================================
		fmt.Printf("⚙️  Booting MariaDB Database (mariadb:%s)...\n", mariadbVersion)
		_ = exec.Command(engine, "rm", "-f", "hal-mariadb").Run()

		dbArgs := []string{
			"run", "-d", "--name", "hal-mariadb",
			"--network", "hal-net",
			"--network-alias", "mariadb.localhost",
			"-p", "3306:3306",
			"-e", "MARIADB_ROOT_PASSWORD=vaultroot", // The true DB root password (humans keep this)
			fmt.Sprintf("mariadb:%s", mariadbVersion),
		}

		if err := exec.Command(engine, dbArgs...).Run(); err != nil {
			fmt.Printf("❌ Failed to start MariaDB: %v\n", err)
			return
		}

		fmt.Println("⚙️  Waiting for MariaDB to initialize (this usually takes 10-15 seconds)...")
		if err := waitForMariaDB(engine, 30); err != nil {
			fmt.Println("\n❌ MariaDB failed to initialize within the time limit.")
			return
		}
		fmt.Println("\n✅ MariaDB is online and accepting connections!")

		// 🎯 BEST PRACTICE 1: Create a least-privileged vaultadmin account
		fmt.Println("⚙️  Provisioning least-privileged 'vaultadmin' account...")
		setupSQL := `
			CREATE USER 'vaultadmin'@'%' IDENTIFIED BY 'temp-vault-pass';
			GRANT SELECT, CREATE USER ON *.* TO 'vaultadmin'@'%' WITH GRANT OPTION;
			FLUSH PRIVILEGES;
		`
		err = exec.Command(engine, "exec", "hal-mariadb", "mariadb", "-u", "root", "-pvaultroot", "-e", setupSQL).Run()
		if err != nil {
			fmt.Printf("❌ Failed to provision vaultadmin account: %v\n", err)
			return
		}

		// 1. Enable Database Secrets Engine
		fmt.Println("⚙️  Configuring Vault Database Secrets Engine...")
		_ = client.Sys().Unmount("database") // Clean state

		err = client.Sys().Mount("database", &vault.MountInput{
			Type: "database",
		})
		if err != nil {
			fmt.Printf("❌ Failed to enable database engine: %v\n", err)
			return
		}

		// 2. Configure Vault Connection using the least-privileged account
		fmt.Println("⚙️  Wiring Vault to MariaDB via the 'vaultadmin' account...")
		_, err = client.Logical().Write("database/config/hal-mariadb", map[string]interface{}{
			"plugin_name":    "mysql-database-plugin",
			"connection_url": "{{username}}:{{password}}@tcp(hal-mariadb:3306)/",
			"allowed_roles":  "readonly-user",
			"username":       "vaultadmin",
			"password":       "temp-vault-pass",
		})
		if err != nil {
			fmt.Printf("❌ Failed to configure database connection: %v\n", err)
			return
		}

		// 🎯 BEST PRACTICE 2: Rotate the Vault Admin Password
		fmt.Println("⚙️  Executing Password Rotation (Vault is taking exclusive ownership of the 'vaultadmin' credentials)...")
		_, err = client.Logical().Write("database/rotate-root/hal-mariadb", map[string]interface{}{})
		if err != nil {
			fmt.Printf("❌ Failed to rotate Vault connection password: %v\n", err)
			return
		}

		// 3. Create the Role with dynamic SQL statements
		fmt.Println("⚙️  Injecting Dynamic SQL Creation Statements...")
		_, err = client.Logical().Write("database/roles/readonly-user", map[string]interface{}{
			"db_name":             "hal-mariadb",
			"creation_statements": "CREATE USER '{{name}}'@'%' IDENTIFIED BY '{{password}}'; GRANT SELECT ON *.* TO '{{name}}'@'%';",
			"default_ttl":         "1h",
			"max_ttl":             "24h",
		})
		if err != nil {
			fmt.Printf("❌ Failed to create Vault role: %v\n", err)
			return
		}

		// 4. Generate the Temporary Credentials
		fmt.Println("⚙️  Requesting temporary JIT (Just-In-Time) credentials from Vault...")
		time.Sleep(2 * time.Second) // Small buffer for connection pool mapping

		secret, err := client.Logical().Read("database/creds/readonly-user")
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
		fmt.Println("   5. This user only has SELECT privileges and will self-destruct in 1 hour.")
		fmt.Println("---------------------------------------------------------")
	},
}

func waitForMariaDB(engine string, maxRetries int) error {
	for i := 0; i < maxRetries; i++ {
		cmd := exec.Command(engine, "exec", "hal-mariadb", "mariadb-admin", "ping", "-h", "127.0.0.1", "-u", "root", "-pvaultroot", "--silent")
		if err := cmd.Run(); err == nil {
			return nil
		}
		fmt.Print(".")
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("timeout")
}

func init() {
	vaultMariadbCmd.Flags().BoolVar(&mariadbDestroy, "destroy", false, "Remove MariaDB and strip the database configuration from Vault")
	vaultMariadbCmd.Flags().StringVar(&mariadbVersion, "mariadb-version", "11.4", "Version of the MariaDB container image to deploy")
	Cmd.AddCommand(vaultMariadbCmd)
}
