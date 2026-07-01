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
	oracleEnable        bool
	oracleDisable       bool
	oracleUpdate        bool
	oracleFreeImage     string
	oracleFreeTag       string
	oraclePluginVersion string
	oraclePluginPath    string
)

var vaultOracleCmd = &cobra.Command{
	Use:   "oracle [status|enable|disable|update]",
	Short: "Deploy Oracle Database Free and configure Vault dynamic Oracle credentials (Enterprise only)",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := parseLifecycleAction(args, &oracleEnable, &oracleDisable, &oracleUpdate); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		client, vaultErr := GetHealthyClient()

		// ==========================================
		// 1. SMART STATUS MODE (Default behavior)
		// ==========================================
		if !oracleEnable && !oracleDisable && !oracleUpdate {
			fmt.Println("🔍 Checking Vault Oracle Database Engine Status...")

			oracleRunning := exec.Command(engine, "inspect", vaultOracleContainer).Run() == nil

			dbMounted := false
			oracleConfigured := false
			if vaultErr == nil {
				mounts, _ := client.Sys().ListMounts()
				_, dbMounted = mounts["database/"]
				if dbMounted {
					resp, err := client.Logical().Read("database/config/" + vaultOracleContainer)
					oracleConfigured = err == nil && resp != nil
				}
			}

			runtimeBuilt := exec.Command(engine, "image", "inspect",
				vaultOracleRuntimeImage+":"+vaultOracleRuntimeTag).Run() == nil

			pluginPresent := exec.Command(engine, "exec", vaultContainer,
				"test", "-f", "/vault/plugins/vault-plugin-database-oracle").Run() == nil

			if runtimeBuilt {
				fmt.Printf("  ✅ Runtime Image  : %s:%s built\n", vaultOracleRuntimeImage, vaultOracleRuntimeTag)
			} else {
				fmt.Printf("  ⚪ Runtime Image  : Not built (built on first enable)\n")
			}

			if pluginPresent {
				fmt.Printf("  ✅ Plugin Binary  : Present in /vault/plugins/\n")
			} else {
				fmt.Printf("  ⚪ Plugin Binary  : Not present\n")
			}

			if oracleRunning {
				fmt.Printf("  ✅ Oracle Free    : Active (%s:%d/%s)\n", vaultOracleHostAlias, vaultOraclePort, vaultOraclePDB)
			} else {
				fmt.Printf("  ❌ Oracle Free    : Not running\n")
			}

			if dbMounted && oracleConfigured {
				fmt.Printf("  ✅ Vault Secrets  : Configured (database/ → oracle)\n")
			} else if dbMounted {
				fmt.Printf("  ⚠️  Vault Secrets  : database/ mounted but oracle not configured\n")
			} else {
				fmt.Printf("  ❌ Vault Secrets  : Not configured\n")
			}

			fmt.Println("\n💡 Next Step:")
			if !oracleRunning && !dbMounted {
				fmt.Println("   hal vault oracle enable --oracle-plugin-path /path/to/vault-plugin-database-oracle")
				fmt.Println("\n   ⚠️  Requires: Vault Enterprise + VAULT_LICENSE set")
				fmt.Println("   ℹ️  Build the plugin binary first — see docs/vault-oracle-plugin-build.md")
			} else if oracleRunning && oracleConfigured {
				fmt.Println("   vault read database/creds/oracle-dba-role")
				fmt.Println("\n   hal vault oracle disable   (to tear down)")
			} else {
				fmt.Println("   hal vault oracle update --oracle-plugin-path /path/to/vault-plugin-database-oracle")
			}
			return
		}

		// ==========================================
		// Enterprise gate
		// ==========================================
		if oracleEnable || oracleUpdate {
			if vaultErr == nil {
				health, err := client.Sys().Health()
				if err == nil && !strings.Contains(health.Version, "ent") {
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
		// Plugin binary gate (enable/update only)
		// ==========================================
		if oracleEnable || oracleUpdate {
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
		// 2. TEARDOWN / RESET PATH (disable / update)
		// ==========================================
		if oracleDisable || oracleUpdate {
			if global.DryRun {
				fmt.Printf("[DRY RUN] Would remove container %s\n", vaultOracleContainer)
				fmt.Println("[DRY RUN] Would revoke leases and unmount database/")
			} else {
				if oracleDisable {
					fmt.Println("🛑 Tearing down Oracle environment...")
				} else {
					fmt.Println("♻️  Update requested. Destroying Oracle environment for reset...")
				}

				if vaultErr == nil && client != nil {
					fmt.Println("⚙️  Revoking leases and cleaning Vault config...")
					_ = client.Sys().RevokeForce("database/")
					_, _ = client.Logical().Delete("database/config/" + vaultOracleContainer)
					_, _ = client.Logical().Delete("database/roles/oracle-dba-role")
					_ = client.Sys().Unmount("database")
					_, _ = client.Logical().Delete("sys/plugins/catalog/database/vault-plugin-database-oracle")
				} else {
					fmt.Println("⚠️  Vault offline — skipped Vault-side cleanup.")
				}

				fmt.Printf("⚙️  Removing Oracle container (%s)...\n", vaultOracleContainer)
				_ = exec.Command(engine, "rm", "-f", vaultOracleContainer).Run()

				if oracleDisable {
					fmt.Println("✅ Oracle environment destroyed.")
					global.RefreshHalHealth(engine)
				}
			}

			if oracleDisable && !global.DryRun {
				return
			}
		}

		// ==========================================
		// 3. ENABLE / UPDATE PATH
		// ==========================================
		if oracleEnable || oracleUpdate {
			if global.DryRun {
				fmt.Printf("[DRY RUN] Would build %s:%s runtime image\n", vaultOracleRuntimeImage, vaultOracleRuntimeTag)
				fmt.Printf("[DRY RUN] Would copy plugin from %s into vault plugins volume\n", oraclePluginPath)
				fmt.Printf("[DRY RUN] Would start Oracle Free (%s:%s)\n", oracleFreeImage, oracleFreeTag)
				fmt.Println("[DRY RUN] Would configure Vault database engine")
				return
			}

			// ---- Step 1: Build the custom vault+oracle runtime image ----
			runtimeRef := vaultOracleRuntimeImage + ":" + vaultOracleRuntimeTag
			runtimeBuilt := exec.Command(engine, "image", "inspect", runtimeRef).Run() == nil
			if oracleUpdate || !runtimeBuilt {
				fmt.Printf("🔨 Building runtime image %s (debian + Vault binary + Oracle Instant Client)...\n", runtimeRef)
				if err := buildOracleRuntimeImage(engine, runtimeRef); err != nil {
					fmt.Printf("❌ Failed to build runtime image: %v\n", err)
					return
				}
				fmt.Printf("  ✅ Runtime image %s built.\n", runtimeRef)
			} else {
				fmt.Printf("⚡ Runtime image %s already present — skipping build.\n", runtimeRef)
			}

			// ---- Step 2: Copy plugin binary into shared plugins volume ----
			fmt.Printf("⚙️  Installing oracle plugin binary from %s...\n", oraclePluginPath)
			pluginData, err := os.ReadFile(oraclePluginPath)
			if err != nil {
				fmt.Printf("❌ Cannot read plugin binary: %v\n", err)
				return
			}
			tmpPlugin := "/tmp/vault-plugin-database-oracle"
			if err := os.WriteFile(tmpPlugin, pluginData, 0755); err != nil {
				fmt.Printf("❌ Cannot stage plugin binary: %v\n", err)
				return
			}
			defer os.Remove(tmpPlugin)

			cpOut, cpErr := exec.Command(engine, "cp", tmpPlugin,
				vaultContainer+":/vault/plugins/vault-plugin-database-oracle").CombinedOutput()
			if cpErr != nil {
				fmt.Printf("❌ Failed to copy plugin into vault container: %v\n%s\n", cpErr, string(cpOut))
				return
			}
			_ = exec.Command(engine, "exec", vaultContainer,
				"chmod", "+x", "/vault/plugins/vault-plugin-database-oracle").Run()
			fmt.Println("  ✅ Plugin binary installed.")

			// ---- Step 3: Restart vault with the oracle runtime image ----
			currentImage, _ := exec.Command(engine, "inspect", vaultContainer,
				"--format", "{{.Config.Image}}").Output()
			if !strings.Contains(strings.TrimSpace(string(currentImage)), vaultOracleRuntimeImage) {
				fmt.Println("♻️  Restarting Vault with oracle runtime image...")
				if err := restartVaultWithOracleImage(engine, runtimeRef); err != nil {
					fmt.Printf("❌ Failed to restart Vault: %v\n", err)
					return
				}
				fmt.Println("⏳ Waiting for Vault to re-initialize...")
				if err := waitForService("Vault", vaultHealthURL, 30); err != nil {
					fmt.Println("❌ Vault did not come back healthy after restart.")
					return
				}
				client, vaultErr = GetHealthyClient()
				if vaultErr != nil {
					fmt.Printf("❌ Vault unhealthy after restart: %v\n", vaultErr)
					return
				}
				fmt.Println("  ✅ Vault restarted with oracle runtime image.")

				// Re-copy plugin binary — volume is preserved but the new container
				// may have created a fresh overlay for /vault/plugins if the volume
				// wasn't attached correctly.
				cpOut, cpErr = exec.Command(engine, "cp", tmpPlugin,
					vaultContainer+":/vault/plugins/vault-plugin-database-oracle").CombinedOutput()
				if cpErr != nil {
					fmt.Printf("❌ Failed to re-copy plugin after restart: %v\n%s\n", cpErr, string(cpOut))
					return
				}
				_ = exec.Command(engine, "exec", vaultContainer,
					"chmod", "+x", "/vault/plugins/vault-plugin-database-oracle").Run()
			} else {
				fmt.Println("⚡ Vault already running with oracle runtime image.")
			}

			// ---- Step 4: Start Oracle Database Free ----
			fmt.Printf("🚀 Starting Oracle Database Free (%s:%s)...\n", oracleFreeImage, oracleFreeTag)
			_ = exec.Command(engine, "rm", "-f", vaultOracleContainer).Run()

			oracleArgs := []string{
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

			if out, err := exec.Command(engine, oracleArgs...).CombinedOutput(); err != nil {
				fmt.Printf("❌ Failed to start Oracle Free: %v\n%s\n", err, string(out))
				return
			}

			// ---- Step 5: Wait for Oracle to accept connections ----
			fmt.Println("⏳ Waiting for Oracle Database Free to accept connections (this takes 60-120s)...")
			if err := waitForOracle(engine, vaultOracleContainer, 120); err != nil {
				fmt.Println("❌ Oracle did not become ready in time.")
				fmt.Printf("   💡 Check logs: %s logs %s\n", engine, vaultOracleContainer)
				return
			}
			fmt.Println("  ✅ Oracle is ready and accepting connections.")

			// ---- Step 6: Grant vault user extra privileges ----
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

			if out, err := exec.Command(engine, "exec", vaultOracleContainer,
				"sh", "-c", grantCmd).CombinedOutput(); err != nil {
				fmt.Printf("⚠️  Grant step returned errors (may be partial — continuing): %v\n%s\n", err, string(out))
			} else {
				fmt.Println("  ✅ Vault user privileges granted.")
			}

			// ---- Step 7: Register oracle plugin with Vault (API) ----
			fmt.Printf("⚙️  Registering oracle plugin with Vault...\n")
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

			// ---- Step 8: Enable database secrets engine ----
			fmt.Println("⚙️  Enabling Vault database secrets engine...")
			_ = client.Sys().Unmount("database")
			if err := client.Sys().Mount("database", &vault.MountInput{
				Type: "database",
			}); err != nil {
				fmt.Printf("❌ Failed to enable database engine: %v\n", err)
				return
			}

			// ---- Step 9: Configure oracle connection ----
			fmt.Println("⚙️  Configuring Vault → Oracle connection...")
			connectionURL := fmt.Sprintf("{{username}}/{{password}}@%s:%d/%s",
				vaultOracleContainer, vaultOraclePort, vaultOraclePDB)

			_, err = client.Logical().Write("database/config/"+vaultOracleContainer, map[string]interface{}{
				"plugin_name":             "vault-plugin-database-oracle",
				"connection_url":          connectionURL,
				"allowed_roles":           "oracle-dba-role",
				"username":                "vault",
				"password":                vaultOracleVaultPass,
				"max_connection_lifetime": "60s",
			})
			if err != nil {
				fmt.Printf("❌ Failed to configure oracle connection: %v\n", err)
				return
			}

			// ---- Step 10: Rotate vault user password ----
			fmt.Println("⚙️  Rotating vault user password (Vault takes exclusive ownership)...")
			if _, err := client.Logical().Write("database/rotate-root/"+vaultOracleContainer,
				map[string]interface{}{}); err != nil {
				fmt.Printf("❌ Failed to rotate root password: %v\n", err)
				return
			}

			// ---- Step 11: Create role ----
			fmt.Println("⚙️  Creating oracle-dba-role...")
			creationSQL := `CREATE USER {{username}} IDENTIFIED BY "{{password}}"; GRANT CONNECT TO {{username}}; GRANT CREATE SESSION TO {{username}};`
			_, err = client.Logical().Write("database/roles/oracle-dba-role", map[string]interface{}{
				"db_name":               vaultOracleContainer,
				"creation_statements":   creationSQL,
				"revocation_statements": `DROP USER {{username}} CASCADE;`,
				"default_ttl":           "1h",
				"max_ttl":               "24h",
			})
			if err != nil {
				fmt.Printf("❌ Failed to create role: %v\n", err)
				return
			}

			// ---- Step 12: Generate test credentials ----
			fmt.Println("⚙️  Generating test JIT credentials...")
			time.Sleep(2 * time.Second)
			secret, err := client.Logical().Read("database/creds/oracle-dba-role")
			if err != nil || secret == nil {
				fmt.Printf("❌ Failed to generate credentials: %v\n", err)
				return
			}

			username := secret.Data["username"].(string)
			password := secret.Data["password"].(string)

			fmt.Println("\n✅ Vault Oracle Database Secrets Engine Configured!")
			global.RefreshHalHealth(engine)
			fmt.Println("---------------------------------------------------------")
			fmt.Printf("🔗 Oracle Free : %s:%d/%s\n", vaultOracleHostAlias, vaultOraclePort, vaultOraclePDB)
			fmt.Println("👤 JIT Username: " + username)
			fmt.Println("🔑 JIT Password: " + password)
			fmt.Println("\n💡 THE SECURE WORKFLOW:")
			fmt.Println("   1. gvenzl/oracle-free created the 'vault' broker account in FREEPDB1.")
			fmt.Println("   2. Vault immediately rotated the 'vault' password. Nobody knows it!")
			fmt.Println("   3. Vault used that account to dynamically create the JIT user above.")
			fmt.Println("   4. Try: vault read database/creds/oracle-dba-role")
			fmt.Println("\n⚠️  NOTE: Vault was restarted with the oracle runtime image.")
			fmt.Println("   After 'hal vault delete/create', re-run 'hal vault oracle enable'.")
			fmt.Println("---------------------------------------------------------")
		}
	},
}

// buildOracleRuntimeImage builds a debian-slim image containing the vault binary
// and Oracle Instant Client. On amd64 it uses IC 19.26; on arm64 it uses IC 23.26.2
// which ships libclntsh.so.19.1 as a backward-compat symlink.
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

// restartVaultWithOracleImage replaces hal-vault with the oracle runtime image,
// preserving all volumes, ports, and the license env var.
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

// extractVaultLicense retrieves the Vault Enterprise license from the running
// container env, the VAULT_LICENSE env var, or the VAULT_LICENSE_PATH file.
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

// waitForOracle polls Oracle Free using sqlplus until the listener accepts
// connections on FREEPDB1. The gvenzl/oracle-free:slim image does not include
// a Docker HEALTHCHECK, so we cannot rely on container health status.
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

func sha256HexFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

func init() {
	vaultOracleCmd.Flags().BoolVarP(&oracleEnable, "enable", "e", false, "Deploy Oracle Database Free and configure Vault oracle secrets engine")
	vaultOracleCmd.Flags().BoolVarP(&oracleDisable, "disable", "d", false, "Remove Oracle Database Free and clean up Vault configuration")
	vaultOracleCmd.Flags().BoolVarP(&oracleUpdate, "update", "u", false, "Reconcile Oracle Database Free and Vault oracle integration")
	_ = vaultOracleCmd.Flags().MarkHidden("enable")
	_ = vaultOracleCmd.Flags().MarkHidden("disable")
	_ = vaultOracleCmd.Flags().MarkHidden("update")

	vaultOracleCmd.Flags().StringVar(&oracleFreeImage, "oracle-image", defaultOracleFreeImage, "Oracle Database Free container image")
	vaultOracleCmd.Flags().StringVar(&oracleFreeTag, "oracle-tag", defaultOracleFreeTag, "Oracle Database Free container image tag")
	vaultOracleCmd.Flags().StringVar(&oraclePluginVersion, "oracle-plugin-version", defaultOraclePluginVer, "Version string to register with Vault (must match binary)")
	vaultOracleCmd.Flags().StringVar(&oraclePluginPath, "oracle-plugin-path", "", "Path to vault-plugin-database-oracle binary (required for enable/update)")

	Cmd.AddCommand(vaultOracleCmd)
}
