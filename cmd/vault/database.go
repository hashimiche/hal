package vault

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"hal/internal/global"

	vault "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
)

var (
	databaseEnable   bool
	databaseDisable  bool
	databaseUpdate   bool
	databaseBackend  string
	mariadbVersion   string
	mariadbImage     string
	dbUsernamePrefix string

	oracleFreeImage     string
	oracleFreeTag       string
	oraclePluginVersion string
	oraclePluginPath    string

	// --k8s: extend database enable to also deploy VSO and a VaultDynamicSecret.
	dbVSOEnabled       bool
	dbVSOKindNodeImage string
	dbVSOChartVersion  string
	dbVSOBackendImage  string
	dbVSOBackendTag    string
	dbVSOProxyImage    string
	dbVSOProxyTag      string
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

		// ==========================================
		// 1. SMART STATUS MODE (Default behavior)
		// ==========================================
		if !databaseEnable && !databaseDisable && !databaseUpdate {
			client, vaultErr := GetHealthyClient()
			showDatabaseStatus(engine, client, vaultErr)
			return
		}

		backend := strings.ToLower(strings.TrimSpace(databaseBackend))

		var (
			containerName string
			hostAlias     string
			containerPort string
			pluginName    string
			connectionURL string
			createStmt    string
			revokeStmt    string
			roleName      string
			backendLabel  string
			startArgs     []string
			setupCmd      []string
		)

		switch backend {
		case "mariadb":
			backendLabel = "MariaDB"
			containerName = vaultMariaDBContainer
			hostAlias = vaultMariaDBHostAlias
			containerPort = "3306"
			pluginName = "mysql-database-plugin"
			connectionURL = "{{username}}:{{password}}@tcp(hal-vault-mariadb:3306)/"
			createStmt = "CREATE USER '{{name}}'@'%' IDENTIFIED BY '{{password}}'; GRANT ALL PRIVILEGES ON *.* TO '{{name}}'@'%';"
			revokeStmt = "DROP USER IF EXISTS '{{name}}'@'%';"
			roleName = "dba-role"
			setupCmd = []string{"mariadb", "-u", "root", "-p" + vaultMariaDBRootPassword, "-e", `
				CREATE USER 'vaultadmin'@'%' IDENTIFIED BY 'temp-vault-pass';
				GRANT ALL PRIVILEGES ON *.* TO 'vaultadmin'@'%' WITH GRANT OPTION;
				FLUSH PRIVILEGES;
			`}
			startArgs = []string{
				"run", "-d", "--name", vaultMariaDBContainer,
				"--network", global.HalNetName,
				"--network-alias", vaultMariaDBHostAlias,
				"-p", fmt.Sprintf("%d:%d", vaultMariaDBPort, vaultMariaDBPort),
				"-e", "MARIADB_ROOT_PASSWORD=" + vaultMariaDBRootPassword,
				fmt.Sprintf("%s:%s", mariadbImage, mariadbVersion),
			}

		case "oracle":
			backendLabel = "Oracle Free"
			containerName = vaultOracleContainer
			hostAlias = vaultOracleHostAlias
			containerPort = fmt.Sprintf("%d", vaultOraclePort)
			pluginName = "vault-plugin-database-oracle"
			connectionURL = fmt.Sprintf("{{username}}/{{password}}@%s:%d/%s",
				vaultOracleContainer, vaultOraclePort, vaultOraclePDB)
			createStmt = `CREATE USER {{username}} IDENTIFIED BY "{{password}}"; GRANT CONNECT TO {{username}}; GRANT CREATE SESSION TO {{username}};`
			revokeStmt = `DROP USER {{username}} CASCADE;`
			roleName = "oracle-dba-role"
			startArgs = []string{
				"run", "-d",
				"--name", vaultOracleContainer,
				"--network", global.HalNetName,
				"--network-alias", vaultOracleHostAlias,
				"-p", fmt.Sprintf("%d:%d", vaultOraclePort, vaultOraclePort),
				"-e", "ORACLE_PASSWORD=" + vaultOracleSysPass,
				"-e", "APP_USER=vault",
				"-e", "APP_USER_PASSWORD=" + vaultOracleVaultPass,
				fmt.Sprintf("%s:%s", oracleFreeImage, oracleFreeTag),
			}

		case "postgres", "pgsql":
			fmt.Println("❌ Backend pgsql is not implemented yet in HAL. Use --backend mariadb for now.")
			fmt.Println("💡 Next Step: hal vault database enable")
			return

		default:
			fmt.Printf("❌ Unsupported backend %q. Valid backends: mariadb, oracle (pgsql planned).\n", databaseBackend)
			return
		}

		client, vaultErr := GetHealthyClient()

		// ==========================================
		// Oracle: Enterprise gate
		// ==========================================
		if backend == "oracle" && (databaseEnable || databaseUpdate) {
			if vaultErr == nil {
				health, hErr := client.Sys().Health()
				if hErr == nil && !strings.Contains(health.Version, "ent") {
					fmt.Println("❌ Error: The Oracle database plugin requires Vault Enterprise.")
					fmt.Println("   💡 Your current Vault is Community Edition.")
					fmt.Println("\n   hal vault delete")
					fmt.Println("   export VAULT_LICENSE='your_license_string'")
					fmt.Println("   hal vault create --edition ent")
					return
				}
			} else {
				fmt.Printf("❌ Cannot reach Vault: %v\n", vaultErr)
				fmt.Println("   💡 Run 'hal vault create --edition ent' first.")
				return
			}
		}

		// ==========================================
		// Oracle: Plugin binary gate
		// ==========================================
		if backend == "oracle" && (databaseEnable || databaseUpdate) {
			if oraclePluginPath == "" {
				fmt.Println("❌ --oracle-plugin-path is required.")
				fmt.Println("   Provide the path to the vault-plugin-database-oracle binary.")
				fmt.Println("   See docs/vault-oracle-plugin-build.md for build instructions.")
				return
			}
			if _, err := os.Stat(oraclePluginPath); err != nil {
				fmt.Printf("❌ Plugin binary not found at: %s\n", oraclePluginPath)
				return
			}
		}

		// ==========================================
		// 2. TEARDOWN / RESET PATH (--disable / --update)
		// ==========================================
		if databaseDisable || databaseUpdate {
			if global.DryRun {
				fmt.Printf("[DRY RUN] Would execute: %s rm -f %s\n", engine, containerName)
				fmt.Println("[DRY RUN] Would call API to force-revoke leases and unmount 'database/'")
				if dbVSOEnabled {
					fmt.Println("[DRY RUN] Would execute: kind delete cluster (db-vso cluster)")
					fmt.Println("[DRY RUN] Would call API to clean up kubernetes auth and policy for database VSO")
				}
			} else {
				if databaseDisable {
					fmt.Printf("🛑 Tearing down %s environment...\n", backendLabel)
				} else {
					fmt.Printf("♻️  Update requested. Destroying %s environment for reset...\n", backendLabel)
				}

				if vaultErr == nil && client != nil {
					fmt.Println("⚙️  Connecting to Vault API for cleanup (Revoking leases)...")
					_ = client.Sys().RevokeForce("database/")

					if backend == "oracle" {
						_, _ = client.Logical().Delete("database/config/" + containerName)
						_, _ = client.Logical().Delete("database/roles/" + roleName)
					}

					_ = client.Sys().Unmount("database")

					if backend == "oracle" {
						_, _ = client.Logical().Delete("sys/plugins/catalog/database/vault-plugin-database-oracle")
					}

					if dbVSOEnabled {
						disableDatabaseVSOVault(client)
					}
				} else {
					fmt.Println("⚠️  Vault is offline. Skipped Vault-internal cleanup.")
				}

				fmt.Printf("⚙️  Removing %s container...\n", backendLabel)
				_ = exec.Command(engine, "rm", "-f", containerName).Run()

				if dbVSOEnabled {
					disableDatabaseVSOCluster()
				}

				if databaseDisable {
					fmt.Printf("✅ %s environment destroyed successfully!\n", backendLabel)
					global.RefreshHalHealth(engine)
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
				fmt.Printf("[DRY RUN] Would execute Docker run command for %s.\n", backendLabel)
				if backend == "oracle" {
					fmt.Printf("[DRY RUN] Would build %s:%s runtime image\n", vaultOracleRuntimeImage, vaultOracleRuntimeTag)
					fmt.Printf("[DRY RUN] Would copy plugin from %s into vault plugins volume\n", oraclePluginPath)
				}
				fmt.Println("[DRY RUN] Would configure Vault Database secrets engine.")
				return
			}

			// ---- Oracle pre-steps: runtime image, plugin copy, vault restart ----
			if backend == "oracle" {
				var ok bool
				client, ok = oraclePreEnable(engine, client)
				if !ok {
					return
				}
			}

			// ---- Start database container ----
			fmt.Printf("🚀 Booting %s database...\n", backendLabel)
			_ = exec.Command(engine, "rm", "-f", containerName).Run()

			if out, err := exec.Command(engine, startArgs...).CombinedOutput(); err != nil {
				fmt.Printf("❌ Failed to start %s: %v\n%s\n", backendLabel, err, string(out))
				return
			}

			// ---- Wait for database to initialize ----
			fmt.Printf("⏳ Waiting for %s to initialize...\n", backendLabel)
			if backend == "oracle" {
				fmt.Println("   (Oracle takes 60-120s)")
				if err := waitForOracle(engine, containerName, 120); err != nil {
					fmt.Printf("\n❌ %s failed to initialize within the time limit.\n", backendLabel)
					fmt.Printf("   💡 Check logs: %s logs %s\n", engine, containerName)
					return
				}
			} else {
				if err := waitForMariaDB(engine, containerName, 30); err != nil {
					fmt.Printf("\n❌ %s failed to initialize within the time limit.\n", backendLabel)
					return
				}
			}
			fmt.Printf("\n✅ %s is online and accepting connections!\n", backendLabel)

			// ---- Setup broker account ----
			if backend == "oracle" {
				fmt.Println("⚙️  Granting Vault user privileges on Oracle Free...")
				grantSQL := strings.ReplaceAll(`
ALTER SESSION SET CONTAINER=FREEPDB1;
GRANT SELECT_CATALOG_ROLE TO vault;
GRANT CREATE USER, DROP USER, ALTER USER TO vault;
GRANT ALTER SYSTEM TO vault;
GRANT CREATE SESSION, CONNECT TO vault WITH ADMIN OPTION;
`, "\r", "")

				grantCmd := fmt.Sprintf(`sqlplus -S sys/%s@localhost/%s as sysdba <<'EOF'
%s
EOF`, vaultOracleSysPass, vaultOraclePDB, strings.TrimSpace(grantSQL))

				if out, err := exec.Command(engine, "exec", containerName,
					"sh", "-c", grantCmd).CombinedOutput(); err != nil {
					fmt.Printf("⚠️  Grant step returned errors (may be partial — continuing): %v\n%s\n", err, string(out))
				} else {
					fmt.Println("  ✅ Vault user privileges granted.")
				}
			} else {
				fmt.Println("⚙️  Provisioning least-privileged 'vaultadmin' broker account...")
				execArgs := append([]string{"exec", containerName}, setupCmd...)
				err = exec.Command(engine, execArgs...).Run()
				if err != nil {
					fmt.Printf("❌ Failed to provision vaultadmin account: %v\n", err)
					return
				}
			}

			// ---- Oracle: register plugin ----
			if backend == "oracle" {
				fmt.Println("⚙️  Registering oracle plugin with Vault...")
				pluginSHA := sha256HexFile(oraclePluginPath)
				_, err = client.Logical().Write("sys/plugins/catalog/database/vault-plugin-database-oracle", map[string]interface{}{
					"sha256":  pluginSHA,
					"command": "vault-plugin-database-oracle",
				})
				if err != nil {
					fmt.Printf("❌ Failed to register oracle plugin: %v\n", err)
					return
				}
				fmt.Println("  ✅ Plugin registered.")
			}

			// ---- Enable database secrets engine ----
			fmt.Println("⚙️  Configuring Vault Database Secrets Engine...")
			_ = client.Sys().Unmount("database")

			err = client.Sys().Mount("database", &vault.MountInput{
				Type: "database",
			})
			if err != nil {
				fmt.Printf("❌ Failed to enable database engine: %v\n", err)
				return
			}

			// ---- Configure connection ----
			fmt.Printf("⚙️  Wiring Vault to %s...\n", backendLabel)
			configData := map[string]interface{}{
				"plugin_name":    pluginName,
				"connection_url": connectionURL,
				"allowed_roles":  roleName,
			}
			if backend == "oracle" {
				configData["username"] = "vault"
				configData["password"] = vaultOracleVaultPass
				configData["max_connection_lifetime"] = "60s"
			} else {
				configData["username"] = "vaultadmin"
				configData["password"] = "temp-vault-pass"
				configData["username_template"] = fmt.Sprintf("%s-{{random 10}}", dbUsernamePrefix)
			}

			_, err = client.Logical().Write("database/config/"+containerName, configData)
			if err != nil {
				fmt.Printf("❌ Failed to configure database connection: %v\n", err)
				return
			}

			// ---- Rotate root password ----
			fmt.Println("⚙️  Executing Password Rotation (Vault is taking exclusive ownership)...")
			_, err = client.Logical().Write("database/rotate-root/"+containerName, map[string]interface{}{})
			if err != nil {
				fmt.Printf("❌ Failed to rotate Vault connection password: %v\n", err)
				return
			}

			// ---- Create role ----
			fmt.Println("⚙️  Injecting Dynamic SQL Creation Statements...")
			roleData := map[string]interface{}{
				"db_name":               containerName,
				"creation_statements":   createStmt,
				"revocation_statements": revokeStmt,
				"default_ttl":           "2m",
				// When --k8s is active, max_ttl matches default_ttl so the
				// lease is non-renewable.  VSO is then forced to call
				// database/creds/<role> fresh on every cycle, which means a
				// genuinely new username + password each time rather than just
				// extending the same lease.  Without --k8s keep the generous
				// 2h window so manual renewals still work.
				// The --k8s TTL is deliberately short (15s) for demo purposes
				// so credential rotation is visible within seconds.
				"max_ttl": "2h",
			}
			if dbVSOEnabled {
				roleData["default_ttl"] = "15s"
				roleData["max_ttl"] = "15s"
			}
			if backend == "oracle" {
				roleData["default_ttl"] = "1h"
				roleData["max_ttl"] = "24h"
				if dbVSOEnabled {
					roleData["default_ttl"] = "15s"
					roleData["max_ttl"] = "15s"
				}
			}

			_, err = client.Logical().Write("database/roles/"+roleName, roleData)
			if err != nil {
				fmt.Printf("❌ Failed to create Vault role: %v\n", err)
				return
			}

			// ---- Generate test credentials ----
			fmt.Println("⚙️  Requesting temporary JIT (Just-In-Time) credentials from Vault...")
			time.Sleep(2 * time.Second)

			secret, err := client.Logical().Read("database/creds/" + roleName)
			if err != nil || secret == nil {
				fmt.Printf("❌ Failed to generate credentials: %v\n", err)
				return
			}

			username := secret.Data["username"].(string)
			password := secret.Data["password"].(string)

			fmt.Println("\n✅ Enterprise Dynamic Database Credentials Generated!")
			global.RefreshHalHealth(engine)
			fmt.Println("---------------------------------------------------------")
			fmt.Printf("🔗 Database Host: %s:%s\n", hostAlias, containerPort)
			fmt.Println("👤 JIT Username:  " + username)
			fmt.Println("🔑 JIT Password:  " + password)
			fmt.Println("\n💡 THE SECURE WORKFLOW:")
			if backend == "oracle" {
				fmt.Printf("   1. gvenzl/oracle-free created the 'vault' broker account in %s.\n", vaultOraclePDB)
				fmt.Println("   2. Vault immediately rotated the 'vault' password. Nobody knows it!")
				fmt.Println("   3. Vault used that account to dynamically create the JIT user above.")
				fmt.Println("   4. Try: vault read database/creds/" + roleName)
				fmt.Println("\n⚠️  NOTE: Vault was restarted with the oracle runtime image.")
				fmt.Println("   After 'hal vault delete/create', re-run 'hal vault database enable --backend oracle'.")
			} else {
				fmt.Println("   1. A least-privileged 'vaultadmin' account was created.")
				fmt.Println("   2. Vault immediately rotated the 'vaultadmin' password. Nobody knows it!")
				fmt.Println("   3. Vault used that account to dynamically create the JIT user above.")
				fmt.Printf("   4. Try logging in: `mysql -h %s -P %s -u %s -p%s`\n", hostAlias, containerPort, username, password)
				fmt.Println("   5. This user has DBA privileges and will self-destruct in 1 hour.")
			}
			fmt.Println("---------------------------------------------------------")

			// --k8s: spin up KinD + VSO and surface live dynamic DB creds in the app.
			if dbVSOEnabled {
				enableDatabaseVSO(engine, client, backend, roleName)
			}
		}
	},
}

// -----------------------------------------------------------------------------
// Status helpers
// -----------------------------------------------------------------------------

func showDatabaseStatus(engine string, client *vault.Client, vaultErr error) {
	fmt.Println("🔍 Vault Database Secrets Engine Status")
	fmt.Println("---------------------------------------")

	dbMounted := false
	if vaultErr == nil {
		mounts, _ := client.Sys().ListMounts()
		_, dbMounted = mounts["database/"]
	}

	if dbMounted {
		fmt.Println("  ✅ Vault Engine   : database/ mounted")
	} else {
		fmt.Println("  ❌ Vault Engine   : database/ not mounted")
	}

	// --- MariaDB ---
	mariaRunning := exec.Command(engine, "inspect", vaultMariaDBContainer).Run() == nil
	mariaConfigured := false
	if dbMounted && vaultErr == nil {
		resp, err := client.Logical().Read("database/config/" + vaultMariaDBContainer)
		mariaConfigured = err == nil && resp != nil
	}

	fmt.Println()
	fmt.Println("  [ MariaDB ]")
	if mariaRunning {
		fmt.Printf("    ✅ Container    : %s running (%s:%d)\n", vaultMariaDBContainer, vaultMariaDBHostAlias, vaultMariaDBPort)
	} else {
		fmt.Printf("    ⚪ Container    : not running\n")
	}
	if mariaConfigured {
		fmt.Println("    ✅ Vault Config : database/ → mariadb")
	} else {
		fmt.Println("    ⚪ Vault Config : not configured")
	}

	// --- Oracle ---
	oracleRunning := exec.Command(engine, "inspect", vaultOracleContainer).Run() == nil
	oracleConfigured := false
	if dbMounted && vaultErr == nil {
		resp, err := client.Logical().Read("database/config/" + vaultOracleContainer)
		oracleConfigured = err == nil && resp != nil
	}

	runtimeBuilt := exec.Command(engine, "image", "inspect",
		vaultOracleRuntimeImage+":"+vaultOracleRuntimeTag).Run() == nil
	pluginPresent := exec.Command(engine, "exec", vaultContainer,
		"test", "-f", "/vault/plugins/vault-plugin-database-oracle").Run() == nil

	fmt.Println()
	fmt.Println("  [ Oracle (Enterprise) ]")
	if runtimeBuilt {
		fmt.Printf("    ✅ Runtime Image: %s:%s built\n", vaultOracleRuntimeImage, vaultOracleRuntimeTag)
	} else {
		fmt.Println("    ⚪ Runtime Image: not built (built on first enable)")
	}
	if pluginPresent {
		fmt.Println("    ✅ Plugin Binary: present in /vault/plugins/")
	} else {
		fmt.Println("    ⚪ Plugin Binary: not present")
	}
	if oracleRunning {
		fmt.Printf("    ✅ Container    : %s running (%s:%d/%s)\n", vaultOracleContainer, vaultOracleHostAlias, vaultOraclePort, vaultOraclePDB)
	} else {
		fmt.Println("    ⚪ Container    : not running")
	}
	if oracleConfigured {
		fmt.Println("    ✅ Vault Config : database/ → oracle")
	} else {
		fmt.Println("    ⚪ Vault Config : not configured")
	}

	// --- Next steps ---
	fmt.Println()
	fmt.Println("💡 Next Steps:")
	if mariaRunning && mariaConfigured {
		fmt.Println("   vault read database/creds/dba-role")
		fmt.Println("   hal vault database disable                              (tear down mariadb)")
	} else if !mariaRunning {
		fmt.Println("   hal vault database enable                               (deploy mariadb)")
	} else {
		fmt.Println("   hal vault database update                               (reset mariadb)")
	}

	if oracleRunning && oracleConfigured {
		fmt.Println("   vault read database/creds/oracle-dba-role")
		fmt.Println("   hal vault database disable --backend oracle             (tear down oracle)")
	} else if !oracleRunning {
		fmt.Println("   hal vault database enable --backend oracle \\")
		fmt.Println("     --oracle-plugin-path /path/to/vault-plugin-database-oracle")
	} else {
		fmt.Println("   hal vault database update --backend oracle \\")
		fmt.Println("     --oracle-plugin-path /path/to/vault-plugin-database-oracle")
	}
}

// -----------------------------------------------------------------------------
// Oracle pre-enable steps (runtime image, plugin copy, vault restart)
// -----------------------------------------------------------------------------

func oraclePreEnable(engine string, client *vault.Client) (*vault.Client, bool) {
	runtimeRef := vaultOracleRuntimeImage + ":" + vaultOracleRuntimeTag
	runtimeBuilt := exec.Command(engine, "image", "inspect", runtimeRef).Run() == nil
	if databaseUpdate || !runtimeBuilt {
		fmt.Printf("🔨 Building runtime image %s (debian + Vault binary + Oracle Instant Client)...\n", runtimeRef)
		if err := buildOracleRuntimeImage(engine, runtimeRef); err != nil {
			fmt.Printf("❌ Failed to build runtime image: %v\n", err)
			return client, false
		}
		fmt.Printf("  ✅ Runtime image %s built.\n", runtimeRef)
	} else {
		fmt.Printf("⚡ Runtime image %s already present — skipping build.\n", runtimeRef)
	}

	fmt.Printf("⚙️  Installing oracle plugin binary from %s...\n", oraclePluginPath)
	pluginData, err := os.ReadFile(oraclePluginPath)
	if err != nil {
		fmt.Printf("❌ Cannot read plugin binary: %v\n", err)
		return client, false
	}
	tmpPlugin := "/tmp/vault-plugin-database-oracle"
	if err := os.WriteFile(tmpPlugin, pluginData, 0755); err != nil {
		fmt.Printf("❌ Cannot stage plugin binary: %v\n", err)
		return client, false
	}

	cpOut, cpErr := exec.Command(engine, "cp", tmpPlugin,
		vaultContainer+":/vault/plugins/vault-plugin-database-oracle").CombinedOutput()
	if cpErr != nil {
		fmt.Printf("❌ Failed to copy plugin into vault container: %v\n%s\n", cpErr, string(cpOut))
		_ = os.Remove(tmpPlugin)
		return client, false
	}
	_ = exec.Command(engine, "exec", vaultContainer,
		"chmod", "+x", "/vault/plugins/vault-plugin-database-oracle").Run()
	fmt.Println("  ✅ Plugin binary installed.")

	currentImage, _ := exec.Command(engine, "inspect", vaultContainer,
		"--format", "{{.Config.Image}}").Output()
	if !strings.Contains(strings.TrimSpace(string(currentImage)), vaultOracleRuntimeImage) {
		fmt.Println("♻️  Restarting Vault with oracle runtime image...")
		if err := restartVaultWithOracleImage(engine, runtimeRef); err != nil {
			fmt.Printf("❌ Failed to restart Vault: %v\n", err)
			_ = os.Remove(tmpPlugin)
			return client, false
		}
		fmt.Println("⏳ Waiting for Vault to re-initialize...")
		if err := waitForService("Vault", vaultHealthURL, 30); err != nil {
			fmt.Println("❌ Vault did not come back healthy after restart.")
			_ = os.Remove(tmpPlugin)
			return client, false
		}
		newClient, vaultErr := GetHealthyClient()
		if vaultErr != nil {
			fmt.Printf("❌ Vault unhealthy after restart: %v\n", vaultErr)
			_ = os.Remove(tmpPlugin)
			return client, false
		}
		client = newClient
		fmt.Println("  ✅ Vault restarted with oracle runtime image.")

		cpOut, cpErr = exec.Command(engine, "cp", tmpPlugin,
			vaultContainer+":/vault/plugins/vault-plugin-database-oracle").CombinedOutput()
		if cpErr != nil {
			fmt.Printf("❌ Failed to re-copy plugin after restart: %v\n%s\n", cpErr, string(cpOut))
			_ = os.Remove(tmpPlugin)
			return client, false
		}
		_ = exec.Command(engine, "exec", vaultContainer,
			"chmod", "+x", "/vault/plugins/vault-plugin-database-oracle").Run()
	} else {
		fmt.Println("⚡ Vault already running with oracle runtime image.")
	}

	_ = os.Remove(tmpPlugin)
	return client, true
}

// -----------------------------------------------------------------------------
// Helper functions
// -----------------------------------------------------------------------------

func waitForMariaDB(engine, containerName string, maxRetries int) error {
	for i := 0; i < maxRetries; i++ {
		cmd := exec.Command(engine, "exec", containerName, "mariadb-admin", "ping", "-h", "127.0.0.1", "-u", "root", "-p"+vaultMariaDBRootPassword, "--silent")
		if err := cmd.Run(); err == nil {
			return nil
		}
		fmt.Print(".")
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("timeout")
}

func waitForOracle(engine, container string, maxRetries int) error {
	for i := 0; i < maxRetries; i++ {
		pingCmd := fmt.Sprintf(
			`echo "SELECT 1 FROM DUAL;" | sqlplus -S sys/%s@localhost/%s as sysdba`,
			vaultOracleSysPass, vaultOraclePDB,
		)
		out, err := exec.Command(engine, "exec", container, "sh", "-c", pingCmd).CombinedOutput()
		if err == nil && strings.Contains(string(out), "1") && !strings.Contains(strings.ToUpper(string(out)), "ERROR") {
			return nil
		}
		fmt.Print(".")
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("timeout waiting for Oracle Free to accept connections")
}

func buildOracleRuntimeImage(engine, imageRef string) error {
	vaultSrcImage := defaultVaultImageEnt + ":" + defaultVaultEntTag

	arch := runtime.GOARCH
	var platform, icZipURL, icDir string

	switch arch {
	case "arm64":
		platform = "linux/arm64"
		icDir = defaultInstantClientDirARM64
		icZipURL = fmt.Sprintf(
			"https://download.oracle.com/otn_software/linux/instantclient/%s/instantclient-basic-linux.arm64-%s.zip",
			strings.ReplaceAll(defaultInstantClientVerARM64, ".", ""),
			defaultInstantClientVerARM64,
		)
	default:
		platform = "linux/amd64"
		icDir = defaultInstantClientDir
		icParts := strings.Split(defaultInstantClientVer, ".")
		var icDirNum string
		for _, p := range icParts {
			icDirNum += p
		}
		icZipURL = fmt.Sprintf(
			"https://download.oracle.com/otn_software/linux/instantclient/%s/instantclient-basic-linux.x64-%sdbru.zip",
			icDirNum, defaultInstantClientVer,
		)
	}

	dockerfile := fmt.Sprintf(`FROM --platform=%s %s AS vault-src

FROM --platform=%s debian:12-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    wget unzip libaio1 libgcc-s1 ca-certificates \
    && rm -rf /var/lib/apt/lists/*

RUN mkdir -p /opt/oracle && \
    wget -q "%s" -O /tmp/ic.zip && \
    unzip -q /tmp/ic.zip -d /opt/oracle && \
    rm /tmp/ic.zip

RUN echo "/opt/oracle/%s" > /etc/ld.so.conf.d/oracle-instantclient.conf && ldconfig

COPY --from=vault-src /bin/vault /bin/vault

RUN mkdir -p /vault/logs /vault/plugins /vault/file && \
    useradd -r -u 100 -g 0 vault || true && \
    chown -R 100:0 /vault

ENV VAULT_PLUGIN_DIR=/vault/plugins
ENV LD_LIBRARY_PATH=/opt/oracle/%s

EXPOSE 8200

ENTRYPOINT ["/bin/vault"]
`, platform, vaultSrcImage, platform, icZipURL, icDir, icDir)

	tmpDir, err := os.MkdirTemp("", "hal-oracle-build-*")
	if err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(tmpDir+"/Dockerfile", []byte(dockerfile), 0644); err != nil {
		return fmt.Errorf("write Dockerfile: %w", err)
	}

	buildCmd := exec.Command(engine, "build", "--platform", platform,
		"-t", imageRef, "-f", tmpDir+"/Dockerfile", tmpDir)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	return buildCmd.Run()
}

func restartVaultWithOracleImage(engine, runtimeRef string) error {
	license := extractVaultLicense(engine)

	_ = exec.Command(engine, "rm", "-f", vaultContainer).Run()

	vaultArgs := []string{
		"run", "-d",
		"--name", vaultContainer,
		"--network", global.HalNetName,
		"--network-alias", "vault.localhost",
		"--cap-add", "IPC_LOCK",
		"-p", fmt.Sprintf("%d:%d", vaultHTTPPort, vaultHTTPPort),
		"-v", vaultLogsVolume + ":/vault/logs",
		"-v", vaultPluginsVolume + ":/vault/plugins",
		"-v", vaultDataVolume + ":/vault/file",
		"-e", "VAULT_PLUGIN_DIR=/vault/plugins",
		"-e", "SKIP_SETCAP=true",
	}

	if license != "" {
		vaultArgs = append(vaultArgs, "-e", "VAULT_LICENSE="+license)
	}

	vaultArgs = append(vaultArgs,
		runtimeRef,
		"server", "-dev",
		fmt.Sprintf("-dev-listen-address=0.0.0.0:%d", vaultHTTPPort),
		"-dev-root-token-id="+vaultRootToken,
		"-dev-plugin-dir=/vault/plugins",
	)

	out, err := exec.Command(engine, vaultArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

func extractVaultLicense(engine string) string {
	licenseOut, _ := exec.Command(engine, "inspect", vaultContainer,
		"--format", `{{range .Config.Env}}{{println .}}{{end}}`).Output()
	for _, line := range strings.Split(string(licenseOut), "\n") {
		if strings.HasPrefix(line, "VAULT_LICENSE=") {
			return strings.TrimPrefix(line, "VAULT_LICENSE=")
		}
	}
	if l := os.Getenv("VAULT_LICENSE"); l != "" {
		return l
	}
	if p := os.Getenv("VAULT_LICENSE_PATH"); p != "" {
		if data, err := os.ReadFile(p); err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return ""
}

func sha256HexFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

func init() {
	vaultDatabaseCmd.Flags().BoolVarP(&databaseEnable, "enable", "e", false, "Deploy selected database backend and configure Vault")
	vaultDatabaseCmd.Flags().BoolVarP(&databaseDisable, "disable", "d", false, "Remove selected backend and clean up Vault database configuration")
	vaultDatabaseCmd.Flags().BoolVarP(&databaseUpdate, "update", "u", false, "Reconcile selected backend and Vault database configuration")
	_ = vaultDatabaseCmd.Flags().MarkHidden("enable")
	_ = vaultDatabaseCmd.Flags().MarkHidden("disable")
	_ = vaultDatabaseCmd.Flags().MarkHidden("update")

	vaultDatabaseCmd.Flags().StringVarP(&databaseBackend, "backend", "b", "mariadb", "Database backend to use (mariadb, oracle; pgsql planned)")
	vaultDatabaseCmd.Flags().StringVar(&mariadbVersion, "vault-mariadb-tag", defaultVaultMariaDBTag, "MariaDB container image tag")
	vaultDatabaseCmd.Flags().StringVar(&mariadbImage, "vault-mariadb-image", defaultVaultMariaDBImage, "MariaDB container image name")
	vaultDatabaseCmd.Flags().StringVar(&dbUsernamePrefix, "username-prefix", "v", "Prefix for dynamically generated database usernames (e.g. 'myapp' → 'myapp-AbCdEfGhIj')")

	vaultDatabaseCmd.Flags().StringVar(&oracleFreeImage, "oracle-image", defaultOracleFreeImage, "Oracle Database Free container image")
	vaultDatabaseCmd.Flags().StringVar(&oracleFreeTag, "oracle-tag", defaultOracleFreeTag, "Oracle Database Free container image tag")
	vaultDatabaseCmd.Flags().StringVar(&oraclePluginVersion, "oracle-plugin-version", defaultOraclePluginVer, "Version string to register with Vault (must match binary)")
	vaultDatabaseCmd.Flags().StringVar(&oraclePluginPath, "oracle-plugin-path", "", "Path to vault-plugin-database-oracle binary (required for --backend oracle)")

	// --k8s and related flags
	vaultDatabaseCmd.Flags().BoolVar(&dbVSOEnabled, "k8s", false, "Also deploy KinD + VSO and sync dynamic DB credentials into a demo app via VaultDynamicSecret")
	vaultDatabaseCmd.Flags().StringVar(&dbVSOKindNodeImage, "db-kind-node-image", "kindest/node:v1.31.1", "KinD node image used when creating the database VSO cluster")
	vaultDatabaseCmd.Flags().StringVar(&dbVSOChartVersion, "db-vso-chart-version", "", "Helm chart version for vault-secrets-operator (empty uses latest)")
	vaultDatabaseCmd.Flags().StringVar(&dbVSOBackendImage, "db-app-image", "httpd", "Demo app container image name")
	vaultDatabaseCmd.Flags().StringVar(&dbVSOBackendTag, "db-app-tag", "2.4-alpine", "Demo app container image tag")
	vaultDatabaseCmd.Flags().StringVar(&dbVSOProxyImage, "db-proxy-image", "nginx", "Demo proxy container image name")
	vaultDatabaseCmd.Flags().StringVar(&dbVSOProxyTag, "db-proxy-tag", "alpine", "Demo proxy container image tag")

	Cmd.AddCommand(vaultDatabaseCmd)
}
