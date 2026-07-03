package vault

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"hal/internal/global"

	vault "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
)

const (
	aapDefaultImage          = "ubi9-aap"
	aapDefaultTag            = "latest"
	aapDefaultHostPort       = 443
	aapSSHTargetContainer    = "hal-ssh-target"
	aapSSHTargetDefaultImage = "redhat/ubi9-init"
	aapSSHTargetDefaultTag   = "latest"
)

const (
	vaultAAPJWTMountPath          = "jwt-aap"
	vaultAAPDevelopmentPolicyName = "aap-development-policy"
	vaultAAPProductionPolicyName  = "aap-production-policy"
	vaultAAPDevelopmentRoleName   = "aap-development-role"
	vaultAAPProductionRoleName    = "aap-production-role"
	vaultAAPAAPExternalCredPrefix = "Vault OIDC Base Credential - "
	vaultAAPAAPCustomTypePrefix   = "Vault KV Lookup "
	vaultAAPAAPCustomTypeSuffix   = " Credential"
	vaultAAPAAPCustomCredSuffix   = " Values"
	vaultAAPAAPProjectSuffix      = " Project"
	vaultAAPAAPJobTemplateSuffix  = " KV Demo"
	vaultAAPAAPDemoInventoryName  = "Demo Inventory"
	vaultAAPAAPProjectRepoURL     = "https://github.com/chrisdola/hashicorp-ansible-playbooks"
	vaultAAPAAPProjectBranch      = "main"
	vaultAAPAAPProjectPlaybook    = "print_kv.yml"
)

const (
	vaultAAPSSHMountPath           = "ssh"
	vaultAAPSSHPolicyName          = "ssh-signer"
	vaultAAPSSHSignerRoleName      = "ssh-signer-role"
	vaultAAPSSHDevJWTRoleName      = "aap-ssh-dev-role"
	vaultAAPSSHProdJWTRoleName     = "aap-ssh-prod-role"
	vaultAAPSSHTargetInventoryName = "SSH Target Inventory"
)

var (
	vaultAAPEnable  bool
	vaultAAPDisable bool
	vaultAAPUpdate  bool

	vaultAAPOIDCDiscoveryURL string
	vaultAAPBoundAudience    string
	vaultAAPDevOrgName       string
	vaultAAPProdOrgName      string
	vaultAAPCACertFile       string
	vaultAAPAAPUsername      string
	vaultAAPAAPPassword      string
	vaultAAPAAPVaultURL      string
	vaultAAPAAPLookupTypeID  int

	vaultAAPImage    string
	vaultAAPTag      string
	vaultAAPHostPort int

	vaultAAPSSHTargetImage string
	vaultAAPSSHTargetTag   string
)

type aapEnvironmentConfig struct {
	Environment        string
	Organization       string
	RoleName           string
	SecretPath         string
	ExternalCredName   string
	CustomTypeName     string
	CustomCredName     string
	ProjectName        string
	JobTemplateName    string
	SSHVaultCredName   string
	SSHMachineCredName string
	SSHJWTRoleName     string
}

type aapAPIClient struct {
	engine   string
	baseURL  string
	username string
	password string
	client   *http.Client
}

var vaultAAPCmd = &cobra.Command{
	Use:   "aap [status|enable|disable|update]",
	Short: "Manage AAP runtime and Vault OIDC integration",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := parseLifecycleAction(args, &vaultAAPEnable, &vaultAAPDisable, &vaultAAPUpdate); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		client, vaultErr := GetHealthyClient()

		if !vaultAAPEnable && !vaultAAPDisable && !vaultAAPUpdate {
			vaultAAPStatus(engine, client, vaultErr)
			return
		}

		if vaultAAPDisable {
			if global.DryRun {
				fmt.Printf("[DRY RUN] Would delete AAP JWT roles/policies and disable auth/%s mount.\n", vaultAAPJWTMountPath)
				fmt.Println("[DRY RUN] Would remove the hal-aap container.")
				return
			}

			fmt.Println("🛑 Tearing down AAP environment...")
	
			if global.IsContainerRunning(engine, "hal-aap") {
				aapClient, aapClientErr := newAAPAPIClient(engine, vaultAAPAAPUsername, vaultAAPAAPPassword)
				if aapClientErr == nil {
					envCfgs := vaultAAPAAPEnvironmentConfigs()
					if err := disableAAPSSHResources(aapClient, envCfgs); err != nil {
						fmt.Printf("⚠️  Failed to remove AAP SSH credentials: %v\n", err)
					} else {
						fmt.Println("✅ AAP SSH credentials removed.")
					}
					if err := disableAAPAAPOIDCResources(aapClient, envCfgs); err != nil {
						fmt.Printf("⚠️  Failed to remove AAP OIDC resources: %v\n", err)
					} else {
						fmt.Println("✅ AAP OIDC resources removed.")
					}
				} else {
					fmt.Printf("⚠️  AAP API unavailable; skipped AAP resource cleanup: %v\n", aapClientErr)
				}
			}
	
			if vaultErr == nil && client != nil {
				if err := disableVaultSSHEngine(client); err != nil {
					fmt.Printf("⚠️  Failed to disable Vault SSH engine: %v\n", err)
				} else {
					fmt.Println("✅ Vault SSH engine disabled.")
				}
				if err := disableVaultAAPJWT(client); err != nil {
					fmt.Printf("⚠️  Failed to disable Vault AAP JWT integration: %v\n", err)
				} else {
					fmt.Println("✅ Vault AAP JWT integration disabled.")
				}
			} else {
				fmt.Println("⚠️  Vault is offline. Skipped Vault-internal cleanup.")
			}
	
			fmt.Printf("⚙️  Removing %s container...\n", aapSSHTargetContainer)
			_ = exec.Command(engine, "rm", "-f", aapSSHTargetContainer).Run()
	
			fmt.Println("⚙️  Removing hal-aap container...")
			_ = exec.Command(engine, "rm", "-f", "hal-aap").Run()
	
			fmt.Println("✅ AAP environment destroyed successfully!")
			global.RefreshHalHealth(engine)
			return
		}

		if vaultErr != nil {
			fmt.Printf("❌ Cannot configure AAP JWT integration: %v\n", vaultErr)
			return
		}

		// Ensure the AAP container is running, starting it if necessary.
		if !global.IsContainerRunning(engine, "hal-aap") {
			if global.DryRun {
				fmt.Println("[DRY RUN] Would start AAP container.")
			} else {
				if err := ensureAAPContainer(engine); err != nil {
					fmt.Printf("❌ Failed to start AAP runtime: %v\n", err)
					return
				}
			}
		}

		if global.DryRun {
			fmt.Println("[DRY RUN] Would read AAP CA cert from hal-aap container.")
			fmt.Printf("[DRY RUN] Would enable/configure Vault auth/%s mount for AAP OIDC discovery.\n", vaultAAPJWTMountPath)
			fmt.Println("[DRY RUN] Would reconcile AAP Vault policies, roles, and secret seed data.")
			fmt.Println("[DRY RUN] Would reconcile AAP organizations, external credentials, custom credential types, and input sources.")
			return
		}

		if vaultAAPUpdate {
			fmt.Println("♻️  Update requested. Reconciling Vault AAP JWT integration...")
		} else {
			fmt.Println("🚀 Enabling Vault AAP JWT integration (this can take a few minutes)...")
		}

		caPem, err := readAAPCACertFromContainer(engine, vaultAAPCACertFile)
		if err != nil {
			fmt.Printf("❌ Failed to read AAP CA cert from container: %v\n", err)
			return
		}

		if err := ensureVaultJWTMount(client); err != nil {
			fmt.Printf("❌ Failed to enable auth/%s mount: %v\n", vaultAAPJWTMountPath, err)
			return
		}

		if _, err := client.Logical().Write("auth/"+vaultAAPJWTMountPath+"/config", map[string]interface{}{
			"oidc_discovery_url":    vaultAAPOIDCDiscoveryURL,
			"oidc_discovery_ca_pem": caPem,
		}); err != nil {
			fmt.Printf("❌ Failed to configure auth/%s/config: %v\n", vaultAAPJWTMountPath, err)
			return
		}

		if err := enableVaultSSHEngine(client); err != nil {
			fmt.Printf("❌ Failed to enable SSH secrets engine: %v\n", err)
			return
		}

		if err := ensureVaultSSHCA(client); err != nil {
			fmt.Printf("❌ Failed to configure SSH CA: %v\n", err)
			return
		}

		if err := ensureVaultSSHPolicy(client); err != nil {
			fmt.Printf("❌ Failed to write SSH signer policy: %v\n", err)
			return
		}

		if err := ensureVaultSSHSignerRole(client); err != nil {
			fmt.Printf("❌ Failed to write SSH signer role: %v\n", err)
			return
		}

		if err := ensureVaultSSHJWTRoles(client, vaultAAPDevOrgName, vaultAAPProdOrgName); err != nil {
			fmt.Printf("❌ Failed to write SSH JWT roles: %v\n", err)
			return
		}

		devPolicy := `path "secret/data/development" {
	capabilities = ["read"]
}`
		prodPolicy := `path "secret/data/production" {
	capabilities = ["read"]
}`

		if err := client.Sys().PutPolicy(vaultAAPDevelopmentPolicyName, devPolicy); err != nil {
			fmt.Printf("❌ Failed to write policy %s: %v\n", vaultAAPDevelopmentPolicyName, err)
			return
		}
		if err := client.Sys().PutPolicy(vaultAAPProductionPolicyName, prodPolicy); err != nil {
			fmt.Printf("❌ Failed to write policy %s: %v\n", vaultAAPProductionPolicyName, err)
			return
		}

		if _, err := client.Logical().Write("auth/"+vaultAAPJWTMountPath+"/role/"+vaultAAPDevelopmentRoleName, map[string]interface{}{
			"role_type":       "jwt",
			"bound_audiences": []string{vaultAAPBoundAudience},
			"user_claim":      "sub",
			"bound_claims": map[string]interface{}{
				"aap_controller_organization_name": []string{vaultAAPDevOrgName},
			},
			"token_policies": []string{vaultAAPDevelopmentPolicyName},
		}); err != nil {
			fmt.Printf("❌ Failed to write role %s: %v\n", vaultAAPDevelopmentRoleName, err)
			return
		}

		if _, err := client.Logical().Write("auth/"+vaultAAPJWTMountPath+"/role/"+vaultAAPProductionRoleName, map[string]interface{}{
			"role_type":       "jwt",
			"bound_audiences": []string{vaultAAPBoundAudience},
			"user_claim":      "sub",
			"bound_claims": map[string]interface{}{
				"aap_controller_organization_name": []string{vaultAAPProdOrgName},
			},
			"token_policies": []string{vaultAAPProductionPolicyName},
		}); err != nil {
			fmt.Printf("❌ Failed to write role %s: %v\n", vaultAAPProductionRoleName, err)
			return
		}

		if _, err := client.Logical().Write("secret/data/development", map[string]interface{}{
			"data": map[string]interface{}{"env_name": "development"},
		}); err != nil {
			fmt.Printf("❌ Failed to seed secret/data/development: %v\n", err)
			return
		}

		if _, err := client.Logical().Write("secret/data/production", map[string]interface{}{
			"data": map[string]interface{}{"env_name": "production"},
		}); err != nil {
			fmt.Printf("❌ Failed to seed secret/data/production: %v\n", err)
			return
		}

		if err := ensureSSHTargetContainer(engine); err != nil {
			fmt.Printf("❌ Failed to start %s: %v\n", aapSSHTargetContainer, err)
			return
		}

		if err := injectVaultCAPublicKey(engine, client); err != nil {
			fmt.Printf("❌ Failed to inject Vault CA public key into %s: %v\n", aapSSHTargetContainer, err)
			return
		}

		aapClient, err := newAAPAPIClient(engine, vaultAAPAAPUsername, vaultAAPAAPPassword)
		if err != nil {
			fmt.Printf("❌ Failed to connect to AAP API: %v\n", err)
			return
		}

		envCfgs := vaultAAPAAPEnvironmentConfigs()

		if err := ensureSSHTargetInventory(aapClient); err != nil {
			fmt.Printf("❌ Failed to reconcile SSH target inventory: %v\n", err)
			return
		}

		sshPrivPEM, sshPubOpenSSH, err := generateSSHKeyPair()
		if err != nil {
			fmt.Printf("❌ Failed to generate SSH key pair: %v\n", err)
			return
		}

		sshVaultTypeID, machineTypeID, machineCredIDs, err := reconcileAAPSSHResources(aapClient, envCfgs, sshPrivPEM, sshPubOpenSSH)
		if err != nil {
			fmt.Printf("❌ Failed to reconcile AAP SSH resources: %v\n", err)
			return
		}
		_ = sshVaultTypeID
		_ = machineTypeID

		if err := reconcileAAPAAPOIDCResources(aapClient, envCfgs, caPem, machineCredIDs); err != nil {
			fmt.Printf("❌ Failed to reconcile AAP-side OIDC resources: %v\n", err)
			return
		}

		aapUIURL := aapUIBaseURL(aapClient.baseURL)
		fmt.Println("✅ Vault AAP JWT integration configured.")
		fmt.Println("   Roles: aap-development-role, aap-production-role")
		fmt.Println("   Policies: aap-development-policy, aap-production-policy")
		fmt.Println("   Secret paths: secret/data/development, secret/data/production")
		fmt.Println("   SSH engine, CA, and signing role configured.")
		fmt.Println("   SSH target container: hal-ssh-target (ssh-target.demo.local)")
		fmt.Println("   AAP resources: orgs, external lookup creds, custom credential types, SSH credentials, input sources, projects, and job templates reconciled")
		fmt.Printf("\n🌐 AAP URL  : %s\n", aapUIURL)
		fmt.Printf("   Username : %s\n", vaultAAPAAPUsername)
		fmt.Printf("   Password : %s\n", vaultAAPAAPPassword)
		global.RefreshHalHealth(engine)
	},
}

func vaultAAPStatus(engine string, client *vault.Client, vaultErr error) {
	fmt.Println("🔍 Checking Vault AAP JWT integration status...")

	aapRunning := global.IsContainerRunning(engine, "hal-aap")
	sshTargetRunning := global.IsContainerRunning(engine, aapSSHTargetContainer)
	if aapRunning {
		fmt.Println("  ✅ AAP runtime     : running (hal-aap)")
	} else {
		fmt.Println("  ❌ AAP runtime     : not running")
	}
	if sshTargetRunning {
		fmt.Printf("  ✅ SSH target      : running (%s)\n", aapSSHTargetContainer)
	} else {
		fmt.Printf("  ❌ SSH target      : not running (%s)\n", aapSSHTargetContainer)
	}

	if vaultErr != nil {
		fmt.Printf("  ❌ Vault API       : unavailable (%v)\n", vaultErr)
		fmt.Println("\n💡 Next Step:")
		fmt.Println("   hal vault create")
		return
	}

	auths, _ := client.Sys().ListAuth()
	_, jwtMounted := auths[vaultAAPJWTMountPath+"/"]
	if jwtMounted {
		fmt.Printf("  ✅ JWT auth mount  : enabled (auth/%s)\n", vaultAAPJWTMountPath)
	} else {
		fmt.Println("  ❌ JWT auth mount  : not enabled")
	}

	devRole := vaultPathExists(client, "auth/"+vaultAAPJWTMountPath+"/role/"+vaultAAPDevelopmentRoleName)
	prodRole := vaultPathExists(client, "auth/"+vaultAAPJWTMountPath+"/role/"+vaultAAPProductionRoleName)
	devSecret := vaultPathExists(client, "secret/data/development")
	prodSecret := vaultPathExists(client, "secret/data/production")
	devPolicy := vaultPolicyExists(client, vaultAAPDevelopmentPolicyName)
	prodPolicy := vaultPolicyExists(client, vaultAAPProductionPolicyName)

	statusIcon := func(ok bool) string {
		if ok {
			return "✅"
		}
		return "❌"
	}

	fmt.Printf("  %s Dev role        : %s\n", statusIcon(devRole), vaultAAPDevelopmentRoleName)
	fmt.Printf("  %s Prod role       : %s\n", statusIcon(prodRole), vaultAAPProductionRoleName)
	fmt.Printf("  %s Dev policy      : %s\n", statusIcon(devPolicy), vaultAAPDevelopmentPolicyName)
	fmt.Printf("  %s Prod policy     : %s\n", statusIcon(prodPolicy), vaultAAPProductionPolicyName)
	fmt.Printf("  %s Dev secret      : secret/data/development\n", statusIcon(devSecret))
	fmt.Printf("  %s Prod secret     : secret/data/production\n", statusIcon(prodSecret))

	// SSH Vault checks
	mounts, _ := client.Sys().ListMounts()
	_, sshMounted := mounts[vaultAAPSSHMountPath+"/"]
	sshCA := vaultPathExists(client, vaultAAPSSHMountPath+"/config/ca")
	sshPolicy := vaultPolicyExists(client, vaultAAPSSHPolicyName)
	sshSignerRole := vaultPathExists(client, vaultAAPSSHMountPath+"/roles/"+vaultAAPSSHSignerRoleName)
	sshDevJWTRole := vaultPathExists(client, "auth/"+vaultAAPJWTMountPath+"/role/"+vaultAAPSSHDevJWTRoleName)
	sshProdJWTRole := vaultPathExists(client, "auth/"+vaultAAPJWTMountPath+"/role/"+vaultAAPSSHProdJWTRoleName)

	fmt.Printf("  %s SSH engine      : %s\n", statusIcon(sshMounted), vaultAAPSSHMountPath+"/")
	fmt.Printf("  %s SSH CA          : ssh/config/ca\n", statusIcon(sshCA))
	fmt.Printf("  %s SSH policy      : %s\n", statusIcon(sshPolicy), vaultAAPSSHPolicyName)
	fmt.Printf("  %s SSH signer role : %s\n", statusIcon(sshSignerRole), vaultAAPSSHSignerRoleName)
	fmt.Printf("  %s SSH dev role    : %s\n", statusIcon(sshDevJWTRole), vaultAAPSSHDevJWTRoleName)
	fmt.Printf("  %s SSH prod role   : %s\n", statusIcon(sshProdJWTRole), vaultAAPSSHProdJWTRoleName)

	fullyReady := aapRunning && sshTargetRunning && jwtMounted && devRole && prodRole && devPolicy && prodPolicy && devSecret && prodSecret &&
		sshMounted && sshCA && sshPolicy && sshSignerRole && sshDevJWTRole && sshProdJWTRole

	if aapRunning {
		envCfgs := vaultAAPAAPEnvironmentConfigs()
		aapClient, err := newAAPAPIClient(engine, vaultAAPAAPUsername, vaultAAPAAPPassword)
		if err != nil {
			fmt.Printf("  ⚠️  AAP API         : unavailable (%v)\n", err)
		} else {
			aapReady, statusErr := aapOIDCResourcesReady(aapClient, envCfgs)
			if statusErr != nil {
				fmt.Printf("  ⚠️  AAP resources   : unknown (%v)\n", statusErr)
			} else {
				if aapReady {
					fmt.Println("  ✅ AAP resources   : reconciled")
				} else {
					fmt.Println("  ❌ AAP resources   : not fully reconciled")
					fullyReady = false
				}
			}

			sshReady, sshStatusErr := aapSSHResourcesReady(aapClient, envCfgs)
			if sshStatusErr != nil {
				fmt.Printf("  ⚠️  AAP SSH creds   : unknown (%v)\n", sshStatusErr)
			} else {
				if sshReady {
					fmt.Println("  ✅ AAP SSH creds   : reconciled")
				} else {
					fmt.Println("  ❌ AAP SSH creds   : not fully reconciled")
					fullyReady = false
				}
			}
		}
	}

	fmt.Println("\n💡 Next Step:")
	if fullyReady {
		fmt.Println("   Integration is ready. Reconcile at any time with: hal vault aap update")
		return
	}
	fmt.Println("   hal vault aap enable")
}

func ensureVaultJWTMount(client *vault.Client) error {
	auths, err := client.Sys().ListAuth()
	if err == nil {
		if _, exists := auths[vaultAAPJWTMountPath+"/"]; exists {
			return nil
		}
	}

	return client.Sys().EnableAuthWithOptions(vaultAAPJWTMountPath, &vault.EnableAuthOptions{Type: "jwt"})
}

func disableVaultAAPJWT(client *vault.Client) error {
	_, _ = client.Logical().Delete("auth/" + vaultAAPJWTMountPath + "/role/" + vaultAAPDevelopmentRoleName)
	_, _ = client.Logical().Delete("auth/" + vaultAAPJWTMountPath + "/role/" + vaultAAPProductionRoleName)
	_ = client.Sys().DeletePolicy(vaultAAPDevelopmentPolicyName)
	_ = client.Sys().DeletePolicy(vaultAAPProductionPolicyName)
	_, _ = client.Logical().Delete("secret/metadata/development")
	_, _ = client.Logical().Delete("secret/metadata/production")

	if err := client.Sys().DisableAuth(vaultAAPJWTMountPath); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no handler for route") {
			return nil
		}
		return err
	}

	return nil
}

func enableVaultSSHEngine(client *vault.Client) error {
	mounts, err := client.Sys().ListMounts()
	if err == nil {
		if _, exists := mounts[vaultAAPSSHMountPath+"/"]; exists {
			return nil
		}
	}
	return client.Sys().Mount(vaultAAPSSHMountPath, &vault.MountInput{Type: "ssh"})
}

func ensureVaultSSHCA(client *vault.Client) error {
	secret, err := client.Logical().Read(vaultAAPSSHMountPath + "/config/ca")
	if err == nil && secret != nil && secret.Data["public_key"] != nil {
		return nil
	}
	_, err = client.Logical().Write(vaultAAPSSHMountPath+"/config/ca", map[string]interface{}{
		"generate_signing_key": true,
	})
	return err
}

func ensureVaultSSHPolicy(client *vault.Client) error {
	policy := `path "ssh/sign/ssh-signer-role" {
	capabilities = ["read", "create", "update"]
}
path "ssh/issue/ssh-signer-role" {
	capabilities = ["read", "create", "update"]
}
path "ssh/public_key" {
	capabilities = ["read"]
}
path "ssh/config/ca" {
	capabilities = ["read"]
}`
	return client.Sys().PutPolicy(vaultAAPSSHPolicyName, policy)
}

func ensureVaultSSHJWTRoles(client *vault.Client, devOrg, prodOrg string) error {
	for _, entry := range []struct {
		roleName string
		org      string
	}{
		{vaultAAPSSHDevJWTRoleName, devOrg},
		{vaultAAPSSHProdJWTRoleName, prodOrg},
	} {
		if _, err := client.Logical().Write("auth/"+vaultAAPJWTMountPath+"/role/"+entry.roleName, map[string]interface{}{
			"role_type":       "jwt",
			"bound_audiences": []string{vaultAAPBoundAudience},
			"user_claim":      "sub",
			"bound_claims": map[string]interface{}{
				"aap_controller_organization_name": []string{entry.org},
			},
			"token_policies": []string{vaultAAPSSHPolicyName},
		}); err != nil {
			return fmt.Errorf("writing JWT role %s: %w", entry.roleName, err)
		}
	}
	return nil
}

func ensureVaultSSHSignerRole(client *vault.Client) error {
	_, err := client.Logical().Write(vaultAAPSSHMountPath+"/roles/"+vaultAAPSSHSignerRoleName, map[string]interface{}{
		"key_type":               "ca",
		"algorithm_signer":       "rsa-sha2-256",
		"allow_user_certificates": true,
		"allow_host_certificates": true,
		"allowed_users":          "rhel",
		"default_user":           "rhel",
		"ttl":                    "30m",
		"max_ttl":                "1h",
	})
	return err
}

func disableVaultSSHEngine(client *vault.Client) error {
	_, _ = client.Logical().Delete("auth/" + vaultAAPJWTMountPath + "/role/" + vaultAAPSSHDevJWTRoleName)
	_, _ = client.Logical().Delete("auth/" + vaultAAPJWTMountPath + "/role/" + vaultAAPSSHProdJWTRoleName)
	_ = client.Sys().DeletePolicy(vaultAAPSSHPolicyName)

	if err := client.Sys().Unmount(vaultAAPSSHMountPath); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no handler for route") ||
			strings.Contains(strings.ToLower(err.Error()), "not found") {
			return nil
		}
		return err
	}
	return nil
}

func readAAPCACertFromContainer(engine, containerPath string) (string, error) {
	out, err := exec.Command(engine, "exec", "hal-aap", "cat", containerPath).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("exec failed: %s", strings.TrimSpace(string(out)))
	}
	cert := strings.TrimSpace(string(out))
	if cert == "" {
		return "", fmt.Errorf("empty certificate content at %s", containerPath)
	}
	return cert, nil
}

func ensureAAPContainer(engine string) error {
	global.EnsureNetwork(engine)

	imageRef := resolveAAPImageRef(engine)
	runArgs := []string{
		"run", "-d",
		"--name", "hal-aap",
		"--hostname", "aap.demo.local",
		"--network", "hal-net",
		"--privileged",
		"--cgroupns=host",
		"-v", "/sys/fs/cgroup:/sys/fs/cgroup:rw",
		"--tmpfs", "/run",
		"--tmpfs", "/run/lock",
		"-p", fmt.Sprintf("%d:443", vaultAAPHostPort),
		imageRef,
	}

	out, err := exec.Command(engine, runArgs...).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "AlreadyExists") || strings.Contains(string(out), "already in use") {
			// Container exists but with a potentially different image — remove it and re-run.
			_ = exec.Command(engine, "rm", "-f", "hal-aap").Run()
			out2, err2 := exec.Command(engine, runArgs...).CombinedOutput()
			if err2 != nil {
				return fmt.Errorf("%s", strings.TrimSpace(string(out2)))
			}
		} else {
			return fmt.Errorf("%s", strings.TrimSpace(string(out)))
		}
	}

	fmt.Println("⏳ Waiting for AAP to initialize (this can take a few minutes)...")
	if err := waitForAAPHealth(engine, 600); err != nil {
		return err
	}
	fmt.Println("✅ AAP runtime started.")
	return nil
}

func resolveAAPImageRef(engine string) string {
	ref := fmt.Sprintf("%s:%s", vaultAAPImage, vaultAAPTag)
	localRef := fmt.Sprintf("local/%s:%s", vaultAAPImage, vaultAAPTag)

	if !strings.HasPrefix(vaultAAPImage, "local/") {
		if exec.Command(engine, "image", "inspect", localRef).Run() == nil {
			return localRef
		}
	}
	return ref
}

func waitForAAPHealth(engine string, maxRetries int) error {
	portOut, err := exec.Command(
		engine,
		"inspect",
		"-f",
		"{{(index (index .NetworkSettings.Ports \"443/tcp\") 0).HostPort}}",
		"hal-aap",
	).Output()
	if err != nil {
		return fmt.Errorf("failed to inspect hal-aap port mapping: %w", err)
	}

	hostPort := strings.TrimSpace(string(portOut))
	healthBase := "https://127.0.0.1"
	if hostPort != "" && hostPort != "<no value>" && hostPort != "443" {
		healthBase = fmt.Sprintf("https://127.0.0.1:%s", hostPort)
	}

	candidates := []string{
		healthBase + "/api/controller/v2/ping/",
		healthBase + "/api/v2/ping/",
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	transport.Proxy = nil
	client := &http.Client{Timeout: 10 * time.Second, Transport: transport}

	for i := 0; i < maxRetries; i++ {
		for _, u := range candidates {
			resp, reqErr := client.Get(u)
			if reqErr != nil {
				continue
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("timeout waiting for AAP to become healthy")
}

func vaultPathExists(client *vault.Client, path string) bool {
	secret, err := client.Logical().Read(path)
	if err != nil {
		return false
	}
	return secret != nil
}

func vaultPolicyExists(client *vault.Client, policyName string) bool {
	policy, err := client.Sys().GetPolicy(policyName)
	if err != nil {
		return false
	}
	return strings.TrimSpace(policy) != ""
}

func init() {
	bindVaultAAPFlags(vaultAAPCmd)
	Cmd.AddCommand(vaultAAPCmd)
}

func bindVaultAAPFlags(cmd *cobra.Command) {
	// OIDC / Vault integration flags
	cmd.Flags().StringVar(&vaultAAPOIDCDiscoveryURL, "oidc-discovery-url", "https://hal-aap/o", "AAP OIDC discovery URL used by Vault JWT auth config")
	cmd.Flags().StringVar(&vaultAAPBoundAudience, "bound-audience", "http://hal-vault:8200", "JWT audience expected by Vault AAP roles")
	cmd.Flags().StringVar(&vaultAAPDevOrgName, "development-org", "Development", "AAP organization mapped to development Vault role")
	cmd.Flags().StringVar(&vaultAAPProdOrgName, "production-org", "Production", "AAP organization mapped to production Vault role")
	cmd.Flags().StringVar(&vaultAAPCACertFile, "aap-ca-cert-file", "/home/aap/aap/tls/ca.cert", "Path to the AAP CA certificate inside hal-aap container")
	cmd.Flags().StringVar(&vaultAAPAAPUsername, "aap-username", "admin", "AAP controller username")
	cmd.Flags().StringVar(&vaultAAPAAPPassword, "aap-password", "admin", "AAP controller password")
	cmd.Flags().StringVar(&vaultAAPAAPVaultURL, "aap-vault-url", "http://hal-vault:8200", "Vault URL used by AAP external secret lookup credentials")
	cmd.Flags().IntVar(&vaultAAPAAPLookupTypeID, "aap-vault-lookup-type-id", 29, "AAP credential type ID for HashiCorp Vault Secret Lookup (OIDC)")

	// AAP container lifecycle flags
	cmd.Flags().StringVar(&vaultAAPImage, "aap-image", aapDefaultImage, "AAP container image name")
	cmd.Flags().StringVar(&vaultAAPTag, "aap-tag", aapDefaultTag, "AAP container image tag")
	cmd.Flags().IntVar(&vaultAAPHostPort, "host-port", aapDefaultHostPort, "Host HTTPS port to publish AAP container port 443")

	// SSH target container flags
	cmd.Flags().StringVar(&vaultAAPSSHTargetImage, "ssh-target-image", aapSSHTargetDefaultImage, "SSH target container image name")
	cmd.Flags().StringVar(&vaultAAPSSHTargetTag, "ssh-target-tag", aapSSHTargetDefaultTag, "SSH target container image tag")
}

func vaultAAPAAPEnvironmentConfigs() []aapEnvironmentConfig {
	devLabel := strings.Title(strings.ToLower(vaultAAPDevOrgName))
	prodLabel := strings.Title(strings.ToLower(vaultAAPProdOrgName))

	return []aapEnvironmentConfig{
		{
			Environment:        "development",
			Organization:       vaultAAPDevOrgName,
			RoleName:           vaultAAPDevelopmentRoleName,
			SecretPath:         "/development",
			ExternalCredName:   vaultAAPAAPExternalCredPrefix + devLabel,
			CustomTypeName:     vaultAAPAAPCustomTypePrefix + devLabel + vaultAAPAAPCustomTypeSuffix,
			CustomCredName:     vaultAAPAAPCustomTypePrefix + devLabel + vaultAAPAAPCustomTypeSuffix + vaultAAPAAPCustomCredSuffix,
			ProjectName:        devLabel + vaultAAPAAPProjectSuffix,
			JobTemplateName:    devLabel + vaultAAPAAPJobTemplateSuffix,
			SSHVaultCredName:   "Vault Signed SSH Credential - " + devLabel,
			SSHMachineCredName: devLabel + " SSH Credential",
			SSHJWTRoleName:     vaultAAPSSHDevJWTRoleName,
		},
		{
			Environment:        "production",
			Organization:       vaultAAPProdOrgName,
			RoleName:           vaultAAPProductionRoleName,
			SecretPath:         "/production",
			ExternalCredName:   vaultAAPAAPExternalCredPrefix + prodLabel,
			CustomTypeName:     vaultAAPAAPCustomTypePrefix + prodLabel + vaultAAPAAPCustomTypeSuffix,
			CustomCredName:     vaultAAPAAPCustomTypePrefix + prodLabel + vaultAAPAAPCustomTypeSuffix + vaultAAPAAPCustomCredSuffix,
			ProjectName:        prodLabel + vaultAAPAAPProjectSuffix,
			JobTemplateName:    prodLabel + vaultAAPAAPJobTemplateSuffix,
			SSHVaultCredName:   "Vault Signed SSH Credential - " + prodLabel,
			SSHMachineCredName: prodLabel + " SSH Credential",
			SSHJWTRoleName:     vaultAAPSSHProdJWTRoleName,
		},
	}
}

func newAAPAPIClient(engine, username, password string) (*aapAPIClient, error) {
	base, err := detectAAPAPIBaseURL(engine)
	if err != nil {
		return nil, err
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	transport.Proxy = nil

	return &aapAPIClient{
		engine:   engine,
		baseURL:  base,
		username: username,
		password: password,
		client: &http.Client{
			Timeout:   20 * time.Second,
			Transport: transport,
		},
	}, nil
}

func detectAAPAPIBaseURL(engine string) (string, error) {
	portOut, err := exec.Command(
		engine,
		"inspect",
		"-f",
		"{{(index (index .NetworkSettings.Ports \"443/tcp\") 0).HostPort}}",
		"hal-aap",
	).Output()
	if err != nil {
		return "", fmt.Errorf("failed to inspect hal-aap port mapping: %w", err)
	}

	hostPort := strings.TrimSpace(string(portOut))
	hostBase := "https://127.0.0.1"
	if hostPort != "" && hostPort != "<no value>" && hostPort != "443" {
		hostBase = fmt.Sprintf("https://127.0.0.1:%s", hostPort)
	}

	candidates := []string{
		hostBase + "/api/controller/v2",
		hostBase + "/api/v2",
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	transport.Proxy = nil
	client := &http.Client{Timeout: 10 * time.Second, Transport: transport}

	for _, base := range candidates {
		resp, reqErr := client.Get(base + "/ping/")
		if reqErr != nil {
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized {
			return base, nil
		}
	}

	return "", fmt.Errorf("unable to find a reachable AAP API base URL")
}

// aapUIBaseURL strips the API path suffix from baseURL and returns the bare
// host URL suitable for browser access (e.g. "https://127.0.0.1:8443").
func aapUIBaseURL(baseURL string) string {
	for _, suffix := range []string{"/api/controller/v2", "/api/v2"} {
		if idx := strings.LastIndex(baseURL, suffix); idx >= 0 {
			return baseURL[:idx]
		}
	}
	return baseURL
}

func (c *aapAPIClient) doJSON(method, path string, query url.Values, payload interface{}) (map[string]interface{}, int, error) {
	fullURL := strings.TrimRight(c.baseURL, "/") + "/" + strings.TrimLeft(path, "/")
	if len(query) > 0 {
		fullURL = fullURL + "?" + query.Encode()
	}

	var body io.Reader
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, err
		}
		body = bytes.NewBuffer(buf)
	}

	req, err := http.NewRequest(method, fullURL, body)
	if err != nil {
		return nil, 0, err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	var data map[string]interface{}
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &data); err != nil {
			if resp.StatusCode >= 400 {
				return nil, resp.StatusCode, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
			}
			return nil, resp.StatusCode, nil
		}
	}

	if resp.StatusCode >= 400 {
		detail := strings.TrimSpace(string(bodyBytes))
		if data != nil {
			if v, ok := data["detail"]; ok {
				detail = fmt.Sprintf("%v", v)
			}
			if v, ok := data["error"]; ok {
				detail = fmt.Sprintf("%v", v)
			}
		}
		return data, resp.StatusCode, fmt.Errorf("http %d: %s", resp.StatusCode, detail)
	}

	return data, resp.StatusCode, nil
}

func aapResults(body map[string]interface{}) []map[string]interface{} {
	if body == nil {
		return nil
	}
	raw, ok := body["results"].([]interface{})
	if !ok {
		return nil
	}
	results := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]interface{}); ok {
			results = append(results, m)
		}
	}
	return results
}

func aapMapString(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

func aapMapInt(m map[string]interface{}, key string) int {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	s := fmt.Sprintf("%v", v)
	n, _ := strconv.Atoi(strings.Split(s, ".")[0])
	return n
}

func (c *aapAPIClient) findOrganization(name string) (map[string]interface{}, error) {
	body, _, err := c.doJSON(http.MethodGet, "/organizations/", url.Values{"name": []string{name}}, nil)
	if err != nil {
		return nil, err
	}
	for _, item := range aapResults(body) {
		if aapMapString(item, "name") == name {
			return item, nil
		}
	}
	return nil, nil
}

func (c *aapAPIClient) ensureOrganization(name string) (map[string]interface{}, error) {
	existing, err := c.findOrganization(name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	body, _, err := c.doJSON(http.MethodPost, "/organizations/", nil, map[string]interface{}{"name": name})
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (c *aapAPIClient) findCredentialTypeByName(name string) (map[string]interface{}, error) {
	script := fmt.Sprintf(
		"import json; name=%q; ct=CredentialType.objects.filter(name=name).values('id','name','kind','namespace','inputs','injectors').first(); print('__HAL_JSON__'+json.dumps(ct))",
		name,
	)

	var ct map[string]interface{}
	if err := c.runAWXManageJSON(script, &ct); err != nil {
		return nil, err
	}
	if len(ct) == 0 {
		return nil, nil
	}
	return ct, nil
}

func (c *aapAPIClient) getCredentialTypeByID(id int) (map[string]interface{}, error) {
	if id <= 0 {
		return nil, nil
	}
	script := fmt.Sprintf(
		"import json; ct=CredentialType.objects.filter(id=%d).values('id','name','kind','namespace','inputs','injectors').first(); print('__HAL_JSON__'+json.dumps(ct))",
		id,
	)

	var ct map[string]interface{}
	if err := c.runAWXManageJSON(script, &ct); err != nil {
		return nil, err
	}
	if len(ct) == 0 {
		return nil, nil
	}
	return ct, nil
}

func (c *aapAPIClient) findCredentialTypeForVaultOIDC() (map[string]interface{}, error) {
	if vaultAAPAAPLookupTypeID > 0 {
		ct, err := c.getCredentialTypeByID(vaultAAPAAPLookupTypeID)
		if err != nil {
			return nil, err
		}
		if ct != nil {
			return ct, nil
		}
	}

	preferredNames := []string{
		"HashiCorp Vault Secret Lookup (OIDC)",
		"HashiCorp Vault KV (OIDC)",
		"HashiCorp Vault Secret Lookup",
	}
	for _, name := range preferredNames {
		ct, err := c.findCredentialTypeByName(name)
		if err != nil {
			return nil, err
		}
		if ct != nil {
			return ct, nil
		}
	}

	script := "import json; rows=list(CredentialType.objects.values('id','name','kind','namespace','inputs','injectors')); print('__HAL_JSON__'+json.dumps(rows))"
	rows := []map[string]interface{}{}
	if err := c.runAWXManageJSON(script, &rows); err != nil {
		return nil, err
	}

	for _, item := range rows {
		ns := strings.ToLower(aapMapString(item, "namespace"))
		name := strings.ToLower(aapMapString(item, "name"))
		if ns == "hashivault-kv-oidc" || strings.Contains(name, "vault") && strings.Contains(name, "oidc") {
			return item, nil
		}
	}
	for _, item := range rows {
		ns := strings.ToLower(aapMapString(item, "namespace"))
		if ns == "hashivault-kv" {
			return item, nil
		}
	}

	return nil, fmt.Errorf("could not locate HashiCorp Vault external credential type in AAP")
}

func (c *aapAPIClient) findCredential(name string, orgID, credTypeID int) (map[string]interface{}, error) {
	q := url.Values{"name": []string{name}}
	if orgID > 0 {
		q.Set("organization", strconv.Itoa(orgID))
	}
	if credTypeID > 0 {
		q.Set("credential_type", strconv.Itoa(credTypeID))
	}
	body, _, err := c.doJSON(http.MethodGet, "/credentials/", q, nil)
	if err != nil {
		return nil, err
	}
	for _, item := range aapResults(body) {
		if aapMapString(item, "name") == name {
			return item, nil
		}
	}
	return nil, nil
}

func (c *aapAPIClient) findInventory(name string) (map[string]interface{}, error) {
	body, _, err := c.doJSON(http.MethodGet, "/inventories/", url.Values{"name": []string{name}}, nil)
	if err != nil {
		return nil, err
	}
	for _, item := range aapResults(body) {
		if aapMapString(item, "name") == name {
			return item, nil
		}
	}
	return nil, nil
}

func (c *aapAPIClient) findProject(name string, orgID int) (map[string]interface{}, error) {
	q := url.Values{"name": []string{name}}
	if orgID > 0 {
		q.Set("organization", strconv.Itoa(orgID))
	}
	body, _, err := c.doJSON(http.MethodGet, "/projects/", q, nil)
	if err != nil {
		return nil, err
	}
	for _, item := range aapResults(body) {
		if aapMapString(item, "name") == name {
			return item, nil
		}
	}
	return nil, nil
}

func (c *aapAPIClient) ensureProject(name string, orgID int) (map[string]interface{}, error) {
	payload := map[string]interface{}{
		"name":                 name,
		"organization":         orgID,
		"scm_type":             "git",
		"scm_url":              vaultAAPAAPProjectRepoURL,
		"scm_branch":           vaultAAPAAPProjectBranch,
		"scm_update_on_launch": true,
	}

	existing, err := c.findProject(name, orgID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		body, _, err := c.doJSON(http.MethodPost, "/projects/", nil, payload)
		if err != nil {
			return nil, err
		}
		return body, nil
	}

	id := aapMapInt(existing, "id")
	body, _, err := c.doJSON(http.MethodPatch, fmt.Sprintf("/projects/%d/", id), nil, payload)
	if err != nil {
		return nil, err
	}
	if body == nil {
		return existing, nil
	}
	return body, nil
}

func (c *aapAPIClient) updateProject(projectID int) error {
	if projectID <= 0 {
		return fmt.Errorf("project id is required")
	}
	body, status, err := c.doJSON(http.MethodPost, fmt.Sprintf("/projects/%d/update/", projectID), nil, map[string]interface{}{})
	if err != nil {
		// If an update is already running, allow the existing sync to settle.
		if status == http.StatusBadRequest && strings.Contains(strings.ToLower(err.Error()), "already") {
			return c.waitForProjectToSettle(projectID, 90*time.Second)
		}
		return err
	}
	if body == nil {
		return c.waitForProjectToSettle(projectID, 90*time.Second)
	}
	updateID := aapMapInt(body, "project_update")
	if updateID == 0 {
		updateID = aapMapInt(body, "id")
	}
	if updateID == 0 {
		return c.waitForProjectToSettle(projectID, 90*time.Second)
	}
	return c.waitForProjectUpdate(updateID, 90*time.Second)
}

func (c *aapAPIClient) waitForProjectToSettle(projectID int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		body, _, err := c.doJSON(http.MethodGet, fmt.Sprintf("/projects/%d/", projectID), nil, nil)
		if err != nil {
			return err
		}
		status := strings.ToLower(aapMapString(body, "status"))
		switch status {
		case "successful", "never updated":
			return nil
		case "failed", "error", "canceled":
			return fmt.Errorf("project sync ended with status %s", status)
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout waiting for project %d to settle", projectID)
}

func (c *aapAPIClient) waitForProjectUpdate(updateID int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		body, _, err := c.doJSON(http.MethodGet, fmt.Sprintf("/project_updates/%d/", updateID), nil, nil)
		if err != nil {
			return err
		}
		status := strings.ToLower(aapMapString(body, "status"))
		switch status {
		case "successful":
			return nil
		case "failed", "error", "canceled":
			return fmt.Errorf("project update ended with status %s", status)
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout waiting for project update %d", updateID)
}

func (c *aapAPIClient) waitForProjectPlaybook(projectID int, playbook string, timeout time.Duration) error {
	if projectID <= 0 {
		return fmt.Errorf("project id is required")
	}
	if strings.TrimSpace(playbook) == "" {
		return fmt.Errorf("playbook is required")
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		playbooks, err := c.projectPlaybooks(projectID)
		if err != nil {
			return err
		}

		for _, pb := range playbooks {
			if pb == playbook {
				return nil
			}
		}

		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("timeout waiting for playbook %q in project %d", playbook, projectID)
}

func (c *aapAPIClient) projectPlaybooks(projectID int) ([]string, error) {
	fullURL := strings.TrimRight(c.baseURL, "/") + fmt.Sprintf("/projects/%d/playbooks/", projectID)
	req, err := http.NewRequest(http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	var list []string
	if err := json.Unmarshal(bodyBytes, &list); err == nil {
		return list, nil
	}

	var wrapper map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &wrapper); err == nil {
		results := aapResults(wrapper)
		out := make([]string, 0, len(results))
		for _, r := range results {
			name := aapMapString(r, "name")
			if name != "" {
				out = append(out, name)
			}
		}
		return out, nil
	}

	return nil, fmt.Errorf("unexpected project playbooks response")
}

func (c *aapAPIClient) deleteProject(name string, orgID int) error {
	project, err := c.findProject(name, orgID)
	if err != nil {
		return err
	}
	if project == nil {
		return nil
	}
	id := aapMapInt(project, "id")
	_, _, err = c.doJSON(http.MethodDelete, fmt.Sprintf("/projects/%d/", id), nil, nil)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "404") {
		return err
	}
	return nil
}

func (c *aapAPIClient) findJobTemplate(name string, orgID int) (map[string]interface{}, error) {
	q := url.Values{"name": []string{name}}
	if orgID > 0 {
		q.Set("organization", strconv.Itoa(orgID))
	}
	body, _, err := c.doJSON(http.MethodGet, "/job_templates/", q, nil)
	if err != nil {
		return nil, err
	}
	for _, item := range aapResults(body) {
		if aapMapString(item, "name") == name {
			return item, nil
		}
	}
	return nil, nil
}

func (c *aapAPIClient) ensureJobTemplate(name string, orgID, inventoryID, projectID int, extraVars map[string]interface{}) (map[string]interface{}, error) {
	extraVarsJSON, err := json.Marshal(extraVars)
	if err != nil {
		return nil, err
	}

	payload := map[string]interface{}{
		"name":         name,
		"job_type":     "run",
		"inventory":    inventoryID,
		"project":      projectID,
		"playbook":     vaultAAPAAPProjectPlaybook,
		"organization": orgID,
		"extra_vars":   string(extraVarsJSON),
	}

	existing, err := c.findJobTemplate(name, orgID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		body, _, err := c.doJSON(http.MethodPost, "/job_templates/", nil, payload)
		if err != nil {
			return nil, err
		}
		return body, nil
	}

	id := aapMapInt(existing, "id")
	body, _, err := c.doJSON(http.MethodPatch, fmt.Sprintf("/job_templates/%d/", id), nil, payload)
	if err != nil {
		return nil, err
	}
	if body == nil {
		return existing, nil
	}
	return body, nil
}

func (c *aapAPIClient) ensureJobTemplateCredential(jobTemplateID, credentialID int) error {
	if jobTemplateID <= 0 || credentialID <= 0 {
		return fmt.Errorf("job template id and credential id are required")
	}
	hasCredential, err := c.jobTemplateHasCredential(jobTemplateID, credentialID)
	if err != nil {
		return err
	}
	if hasCredential {
		return nil
	}
	_, _, err = c.doJSON(http.MethodPost, fmt.Sprintf("/job_templates/%d/credentials/", jobTemplateID), nil, map[string]interface{}{"id": credentialID})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "already") {
			return nil
		}
		return err
	}
	return nil
}

func (c *aapAPIClient) deleteJobTemplate(name string, orgID int) error {
	jt, err := c.findJobTemplate(name, orgID)
	if err != nil {
		return err
	}
	if jt == nil {
		return nil
	}
	id := aapMapInt(jt, "id")
	_, _, err = c.doJSON(http.MethodDelete, fmt.Sprintf("/job_templates/%d/", id), nil, nil)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "404") {
		return err
	}
	return nil
}

func (c *aapAPIClient) ensureCredential(name string, orgID, credTypeID int, inputs map[string]interface{}) (map[string]interface{}, error) {
	existing, err := c.findCredential(name, orgID, credTypeID)
	if err != nil {
		return nil, err
	}

	if existing == nil {
		payload := map[string]interface{}{
			"name":            name,
			"organization":    orgID,
			"credential_type": credTypeID,
			"inputs":          inputs,
		}
		body, _, err := c.doJSON(http.MethodPost, "/credentials/", nil, payload)
		if err != nil {
			return nil, err
		}
		return body, nil
	}

	id := aapMapInt(existing, "id")
	body, _, err := c.doJSON(http.MethodPatch, fmt.Sprintf("/credentials/%d/", id), nil, map[string]interface{}{
		"name":            name,
		"organization":    orgID,
		"credential_type": credTypeID,
		"inputs":          inputs,
	})
	if err != nil {
		return nil, err
	}
	if body == nil {
		return existing, nil
	}
	return body, nil
}

func (c *aapAPIClient) ensureCustomCredentialType(name string) (map[string]interface{}, error) {
	inputs := map[string]interface{}{
		"fields": []map[string]interface{}{
			{"id": "env_name", "label": "Environment Name", "type": "string"},
		},
		"required": []string{"env_name"},
	}
	injectors := map[string]interface{}{
		"extra_vars": map[string]interface{}{"env_name": "{{ env_name }}"},
	}

	inputsJSON, err := json.Marshal(inputs)
	if err != nil {
		return nil, err
	}
	injectorsJSON, err := json.Marshal(injectors)
	if err != nil {
		return nil, err
	}

	script := fmt.Sprintf(
		"import json; name=%q; inputs=json.loads(%q); injectors=json.loads(%q); ct,_=CredentialType.objects.get_or_create(name=name, defaults={'kind':'cloud','inputs':inputs,'injectors':injectors}); ct.kind='cloud'; ct.inputs=inputs; ct.injectors=injectors; ct.save(); out={'id':ct.id,'name':ct.name,'kind':ct.kind,'namespace':ct.namespace,'inputs':ct.inputs,'injectors':ct.injectors}; print('__HAL_JSON__'+json.dumps(out))",
		name,
		string(inputsJSON),
		string(injectorsJSON),
	)

	var ct map[string]interface{}
	if err := c.runAWXManageJSON(script, &ct); err != nil {
		return nil, err
	}
	if len(ct) == 0 {
		return nil, fmt.Errorf("failed to create or update credential type %q", name)
	}
	return ct, nil
}

func (c *aapAPIClient) findCredentialInputSource(targetCredID int, inputFieldName string) (map[string]interface{}, error) {
	body, _, err := c.doJSON(http.MethodGet, "/credential_input_sources/", url.Values{
		"target_credential": []string{strconv.Itoa(targetCredID)},
		"input_field_name":  []string{inputFieldName},
	}, nil)
	if err != nil {
		return nil, err
	}
	results := aapResults(body)
	if len(results) == 0 {
		return nil, nil
	}
	return results[0], nil
}

func (c *aapAPIClient) ensureCredentialInputSource(targetCredID, sourceCredID int, inputFieldName string, metadata map[string]interface{}) error {
	existing, err := c.findCredentialInputSource(targetCredID, inputFieldName)
	if err != nil {
		return err
	}

	payload := map[string]interface{}{
		"target_credential": targetCredID,
		"source_credential": sourceCredID,
		"input_field_name":  inputFieldName,
		"metadata":          metadata,
	}

	if existing == nil {
		_, _, err := c.doJSON(http.MethodPost, "/credential_input_sources/", nil, payload)
		return err
	}

	id := aapMapInt(existing, "id")
	_, _, err = c.doJSON(http.MethodPatch, fmt.Sprintf("/credential_input_sources/%d/", id), nil, payload)
	return err
}

func (c *aapAPIClient) deleteCredentialInputSource(targetCredID int, inputFieldName string) error {
	existing, err := c.findCredentialInputSource(targetCredID, inputFieldName)
	if err != nil {
		return err
	}
	if existing == nil {
		return nil
	}
	id := aapMapInt(existing, "id")
	_, _, err = c.doJSON(http.MethodDelete, fmt.Sprintf("/credential_input_sources/%d/", id), nil, nil)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "404") {
		return err
	}
	return nil
}

func (c *aapAPIClient) deleteCredential(name string, orgID, credTypeID int) error {
	cred, err := c.findCredential(name, orgID, credTypeID)
	if err != nil {
		return err
	}
	if cred == nil {
		return nil
	}
	id := aapMapInt(cred, "id")
	_, _, err = c.doJSON(http.MethodDelete, fmt.Sprintf("/credentials/%d/", id), nil, nil)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "404") {
		return err
	}
	return nil
}

func (c *aapAPIClient) deleteCredentialTypeByName(name string) error {
	script := fmt.Sprintf(
		"import json; name=%q; deleted,_=CredentialType.objects.filter(name=name).delete(); print('__HAL_JSON__'+json.dumps({'deleted':deleted}))",
		name,
	)

	var result map[string]interface{}
	if err := c.runAWXManageJSON(script, &result); err != nil {
		return err
	}
	return nil
}

func (c *aapAPIClient) runAWXManageJSON(script string, out interface{}) error {
	const jsonPrefix = "__HAL_JSON__"

	manageCommand := fmt.Sprintf(
		"XDG_RUNTIME_DIR=/run/user/$(id -u) podman exec automation-controller-web /usr/bin/awx-manage shell_plus --quiet-load -c %q",
		script,
	)

	cmd := exec.Command(c.engine, "exec", "-u", "aap", "hal-aap", "bash", "-lc", manageCommand)
	rawOut, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("awx-manage shell_plus failed: %s", strings.TrimSpace(string(rawOut)))
	}

	lines := strings.Split(strings.ReplaceAll(string(rawOut), "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, jsonPrefix) {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, jsonPrefix))
		if payload == "" || payload == "null" {
			return nil
		}
		if err := json.Unmarshal([]byte(payload), out); err != nil {
			return fmt.Errorf("failed to parse awx-manage JSON output: %w", err)
		}
		return nil
	}

	return fmt.Errorf("awx-manage output did not include JSON payload: %s", strings.TrimSpace(string(rawOut)))
}

func reconcileAAPAAPOIDCResources(c *aapAPIClient, envCfgs []aapEnvironmentConfig, caPem string, machineCredIDs map[string]int) error {
	lookupType, err := c.findCredentialTypeForVaultOIDC()
	if err != nil {
		return err
	}
	lookupTypeID := aapMapInt(lookupType, "id")

	// Prefer SSH target inventory; fall back to Demo Inventory if not present.
	inventoryID := 0
	sshInventory, err := c.findInventory(vaultAAPSSHTargetInventoryName)
	if err == nil && sshInventory != nil {
		inventoryID = aapMapInt(sshInventory, "id")
	}
	if inventoryID == 0 {
		fmt.Printf("  ⚠️  SSH target inventory not found; falling back to %s\n", vaultAAPAAPDemoInventoryName)
		demoInventory, err := c.findInventory(vaultAAPAAPDemoInventoryName)
		if err != nil {
			return err
		}
		if demoInventory == nil {
			return fmt.Errorf("could not locate %s or %s inventory in AAP", vaultAAPSSHTargetInventoryName, vaultAAPAAPDemoInventoryName)
		}
		inventoryID = aapMapInt(demoInventory, "id")
	}

	for _, cfg := range envCfgs {
		org, err := c.ensureOrganization(cfg.Organization)
		if err != nil {
			return fmt.Errorf("org %s: %w", cfg.Organization, err)
		}
		orgID := aapMapInt(org, "id")

		externalInputs, err := buildAAPVaultLookupInputs(lookupType, cfg, caPem)
		if err != nil {
			return fmt.Errorf("external credential inputs for %s: %w", cfg.Environment, err)
		}

		externalCred, err := c.ensureCredential(cfg.ExternalCredName, orgID, lookupTypeID, externalInputs)
		if err != nil {
			return fmt.Errorf("external credential %s: %w", cfg.ExternalCredName, err)
		}
		externalCredID := aapMapInt(externalCred, "id")

		customType, err := c.ensureCustomCredentialType(cfg.CustomTypeName)
		if err != nil {
			return fmt.Errorf("custom credential type %s: %w", cfg.CustomTypeName, err)
		}
		customTypeID := aapMapInt(customType, "id")

		targetCred, err := c.ensureCredential(cfg.CustomCredName, orgID, customTypeID, map[string]interface{}{})
		if err != nil {
			return fmt.Errorf("target credential %s: %w", cfg.CustomCredName, err)
		}
		targetCredID := aapMapInt(targetCred, "id")

		project, err := c.ensureProject(cfg.ProjectName, orgID)
		if err != nil {
			return fmt.Errorf("project %s: %w", cfg.ProjectName, err)
		}
		projectID := aapMapInt(project, "id")
		if err := c.updateProject(projectID); err != nil {
			return fmt.Errorf("project sync %s: %w", cfg.ProjectName, err)
		}
		if err := c.waitForProjectPlaybook(projectID, vaultAAPAAPProjectPlaybook, 90*time.Second); err != nil {
			return fmt.Errorf("project playbooks %s: %w", cfg.ProjectName, err)
		}

		jobTemplate, err := c.ensureJobTemplate(cfg.JobTemplateName, orgID, inventoryID, projectID, map[string]interface{}{
			"var_names": []string{"env_name"},
		})
		if err != nil {
			return fmt.Errorf("job template %s: %w", cfg.JobTemplateName, err)
		}
		jobTemplateID := aapMapInt(jobTemplate, "id")
		if err := c.ensureJobTemplateCredential(jobTemplateID, targetCredID); err != nil {
			return fmt.Errorf("job template credential %s: %w", cfg.JobTemplateName, err)
		}

		// Attach SSH Machine Credential if available.
		if machineCredIDs != nil {
			if machineCredID, ok := machineCredIDs[cfg.Environment]; ok && machineCredID > 0 {
				if err := c.ensureJobTemplateCredential(jobTemplateID, machineCredID); err != nil {
					return fmt.Errorf("job template SSH machine credential %s: %w", cfg.JobTemplateName, err)
				}
			}
		}

		metadata, err := buildAAPVaultLookupMetadata(lookupType, cfg)
		if err != nil {
			return fmt.Errorf("input source metadata for %s: %w", cfg.Environment, err)
		}

		if err := c.ensureCredentialInputSource(targetCredID, externalCredID, "env_name", metadata); err != nil {
			return fmt.Errorf("input source for %s: %w", cfg.Environment, err)
		}
	}

	return nil
}

func disableAAPAAPOIDCResources(c *aapAPIClient, envCfgs []aapEnvironmentConfig) error {
	lookupType, err := c.findCredentialTypeForVaultOIDC()
	if err != nil {
		lookupType = nil
	}
	lookupTypeID := 0
	if lookupType != nil {
		lookupTypeID = aapMapInt(lookupType, "id")
	}

	for _, cfg := range envCfgs {
		org, err := c.findOrganization(cfg.Organization)
		if err != nil {
			return err
		}
		if org == nil {
			continue
		}
		orgID := aapMapInt(org, "id")

		if err := c.deleteJobTemplate(cfg.JobTemplateName, orgID); err != nil {
			return err
		}
		if err := c.deleteProject(cfg.ProjectName, orgID); err != nil {
			return err
		}

		customType, err := c.findCredentialTypeByName(cfg.CustomTypeName)
		if err != nil {
			return err
		}
		customTypeID := 0
		if customType != nil {
			customTypeID = aapMapInt(customType, "id")
		}

		targetCred, err := c.findCredential(cfg.CustomCredName, orgID, customTypeID)
		if err != nil {
			return err
		}
		if targetCred != nil {
			targetCredID := aapMapInt(targetCred, "id")
			if err := c.deleteCredentialInputSource(targetCredID, "env_name"); err != nil {
				return err
			}
		}

		if err := c.deleteCredential(cfg.CustomCredName, orgID, customTypeID); err != nil {
			return err
		}
		if err := c.deleteCredential(cfg.ExternalCredName, orgID, lookupTypeID); err != nil {
			return err
		}
		if err := c.deleteCredentialTypeByName(cfg.CustomTypeName); err != nil {
			return err
		}
	}

	return nil
}

func aapOIDCResourcesReady(c *aapAPIClient, envCfgs []aapEnvironmentConfig) (bool, error) {
	lookupType, err := c.findCredentialTypeForVaultOIDC()
	if err != nil {
		return false, nil
	}
	lookupTypeID := aapMapInt(lookupType, "id")

	// Determine expected inventory ID (SSH target preferred, Demo Inventory fallback).
	expectedInventoryID := 0
	sshInventory, err := c.findInventory(vaultAAPSSHTargetInventoryName)
	if err == nil && sshInventory != nil {
		expectedInventoryID = aapMapInt(sshInventory, "id")
	}
	if expectedInventoryID == 0 {
		demoInventory, err := c.findInventory(vaultAAPAAPDemoInventoryName)
		if err == nil && demoInventory != nil {
			expectedInventoryID = aapMapInt(demoInventory, "id")
		}
	}

	for _, cfg := range envCfgs {
		org, err := c.findOrganization(cfg.Organization)
		if err != nil {
			return false, err
		}
		if org == nil {
			return false, nil
		}
		orgID := aapMapInt(org, "id")

		externalCred, err := c.findCredential(cfg.ExternalCredName, orgID, lookupTypeID)
		if err != nil {
			return false, err
		}
		if externalCred == nil {
			return false, nil
		}

		customType, err := c.findCredentialTypeByName(cfg.CustomTypeName)
		if err != nil {
			return false, err
		}
		if customType == nil {
			return false, nil
		}

		customTypeID := aapMapInt(customType, "id")
		targetCred, err := c.findCredential(cfg.CustomCredName, orgID, customTypeID)
		if err != nil {
			return false, err
		}
		if targetCred == nil {
			return false, nil
		}

		targetCredID := aapMapInt(targetCred, "id")
		inputSource, err := c.findCredentialInputSource(targetCredID, "env_name")
		if err != nil {
			return false, err
		}
		if inputSource == nil {
			return false, nil
		}

		project, err := c.findProject(cfg.ProjectName, orgID)
		if err != nil {
			return false, err
		}
		if project == nil {
			return false, nil
		}
		projectID := aapMapInt(project, "id")

		jobTemplate, err := c.findJobTemplate(cfg.JobTemplateName, orgID)
		if err != nil {
			return false, err
		}
		if jobTemplate == nil {
			return false, nil
		}
		if aapMapInt(jobTemplate, "project") != projectID {
			return false, nil
		}
		if expectedInventoryID > 0 && aapMapInt(jobTemplate, "inventory") != expectedInventoryID {
			return false, nil
		}
		jobTemplateID := aapMapInt(jobTemplate, "id")
		hasCredential, err := c.jobTemplateHasCredential(jobTemplateID, targetCredID)
		if err != nil {
			return false, err
		}
		if !hasCredential {
			return false, nil
		}

		// Check SSH Machine Credential is attached.
		if cfg.SSHMachineCredName != "" {
			machineCred, err := c.findCredential(cfg.SSHMachineCredName, orgID, 0)
			if err == nil && machineCred != nil {
				machineCredID := aapMapInt(machineCred, "id")
				hasSSH, err := c.jobTemplateHasCredential(jobTemplateID, machineCredID)
				if err != nil {
					return false, err
				}
				if !hasSSH {
					return false, nil
				}
			}
		}
	}

	return true, nil
}

func (c *aapAPIClient) jobTemplateHasCredential(jobTemplateID, credentialID int) (bool, error) {
	body, _, err := c.doJSON(http.MethodGet, fmt.Sprintf("/job_templates/%d/credentials/", jobTemplateID), nil, nil)
	if err != nil {
		return false, err
	}
	for _, item := range aapResults(body) {
		if aapMapInt(item, "id") == credentialID {
			return true, nil
		}
	}
	return false, nil
}

func buildAAPVaultLookupInputs(lookupType map[string]interface{}, cfg aapEnvironmentConfig, caPem string) (map[string]interface{}, error) {
	inputs := map[string]interface{}{}
	required := map[string]bool{}

	inputsDef, _ := lookupType["inputs"].(map[string]interface{})
	reqFields, _ := inputsDef["required"].([]interface{})
	for _, r := range reqFields {
		required[fmt.Sprintf("%v", r)] = true
	}

	fields, _ := inputsDef["fields"].([]interface{})
	for _, rawField := range fields {
		field, ok := rawField.(map[string]interface{})
		if !ok {
			continue
		}
		id := aapMapString(field, "id")
		if id == "" {
			continue
		}

		lc := strings.ToLower(id)
		switch {
		case lc == "url" || strings.Contains(lc, "server_url"):
			inputs[id] = vaultAAPAAPVaultURL
		case lc == "auth_path" || (strings.Contains(lc, "auth") && strings.Contains(lc, "path")):
			inputs[id] = vaultAAPJWTMountPath
		case lc == "role_id" || lc == "jwt_role" || lc == "role":
			inputs[id] = cfg.RoleName
		case lc == "api_version":
			inputs[id] = "v2"
		case lc == "ca_cert" || strings.Contains(lc, "ca") && strings.Contains(lc, "cert"):
			inputs[id] = caPem
		}
	}

	if len(fields) == 0 && len(reqFields) == 0 {
		inputs["url"] = vaultAAPAAPVaultURL
		inputs["default_auth_path"] = vaultAAPJWTMountPath
		inputs["jwt_role"] = cfg.RoleName
		inputs["api_version"] = "v2"
		return inputs, nil
	}

	missing := []string{}
	for req := range required {
		if _, ok := inputs[req]; ok {
			continue
		}
		fieldFound := false
		for _, rawField := range fields {
			field, ok := rawField.(map[string]interface{})
			if !ok {
				continue
			}
			if aapMapString(field, "id") == req {
				fieldFound = true
				if d, ok := field["default"]; ok {
					inputs[req] = d
				}
				if field["internal"] == true {
					inputs[req] = ""
				}
				break
			}
		}
		if _, ok := inputs[req]; !ok && fieldFound {
			missing = append(missing, req)
		}
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("unmapped required lookup inputs: %s", strings.Join(missing, ", "))
	}

	return inputs, nil
}

func buildAAPVaultLookupMetadata(lookupType map[string]interface{}, cfg aapEnvironmentConfig) (map[string]interface{}, error) {
	metadata := map[string]interface{}{}
	required := map[string]bool{}

	inputsDef, _ := lookupType["inputs"].(map[string]interface{})
	metadataFields, _ := inputsDef["metadata"].([]interface{})
	for _, rawField := range metadataFields {
		field, ok := rawField.(map[string]interface{})
		if !ok {
			continue
		}
		if req, ok := field["required"].(bool); ok && req {
			id := aapMapString(field, "id")
			if id != "" {
				required[id] = true
			}
		}
	}

	if len(metadataFields) == 0 {
		metadata["default_auth_path"] = vaultAAPJWTMountPath
		metadata["secret_path"] = cfg.SecretPath
		metadata["secret_key"] = "env_name"
		metadata["secret_backend"] = "secret"
		metadata["secret_version"] = ""
		return metadata, nil
	}

	for _, rawField := range metadataFields {
		field, ok := rawField.(map[string]interface{})
		if !ok {
			continue
		}
		id := aapMapString(field, "id")
		if id == "" {
			continue
		}
		lc := strings.ToLower(id)
		switch {
		case lc == "url" || strings.Contains(lc, "server_url"):
			metadata[id] = vaultAAPAAPVaultURL
		case lc == "jwt_role" || lc == "role" || lc == "role_id":
			metadata[id] = cfg.RoleName
		case lc == "api_version":
			metadata[id] = "v2"
		case lc == "secret_path" || strings.Contains(lc, "path") && strings.Contains(lc, "secret"):
			metadata[id] = cfg.SecretPath
		case lc == "secret_key" || lc == "key_name":
			metadata[id] = "env_name"
		case lc == "auth_path" || strings.Contains(lc, "auth") && strings.Contains(lc, "path"):
			metadata[id] = vaultAAPJWTMountPath
		case lc == "secret_backend" || strings.Contains(lc, "backend"):
			metadata[id] = "secret"
		case lc == "secret_version":
			metadata[id] = ""
		}
	}

	missing := []string{}
	for req := range required {
		if _, ok := metadata[req]; ok {
			continue
		}
		for _, rawField := range metadataFields {
			field, ok := rawField.(map[string]interface{})
			if !ok {
				continue
			}
			if aapMapString(field, "id") == req {
				if d, ok := field["default"]; ok {
					metadata[req] = d
				}
				break
			}
		}
		if _, ok := metadata[req]; !ok {
			if req == "job_template_id" {
				metadata[req] = ""
			} else {
				missing = append(missing, req)
			}
		}
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("unmapped required lookup metadata fields: %s", strings.Join(missing, ", "))
	}

	return metadata, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Sub-Task 2: hal-ssh-target container lifecycle
// ─────────────────────────────────────────────────────────────────────────────

func ensureSSHTargetContainer(engine string) error {
	if global.IsContainerRunning(engine, aapSSHTargetContainer) {
		return nil
	}

	global.EnsureNetwork(engine)

	imageRef := fmt.Sprintf("%s:%s", vaultAAPSSHTargetImage, vaultAAPSSHTargetTag)
	runArgs := []string{
		"run", "-d",
		"--name", aapSSHTargetContainer,
		"--hostname", "ssh-target.demo.local",
		"--network", "hal-net",
		imageRef,
		"sleep", "infinity",
	}

	out, err := exec.Command(engine, runArgs...).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "AlreadyExists") || strings.Contains(string(out), "already in use") {
			_ = exec.Command(engine, "rm", "-f", aapSSHTargetContainer).Run()
			out2, err2 := exec.Command(engine, runArgs...).CombinedOutput()
			if err2 != nil {
				return fmt.Errorf("%s", strings.TrimSpace(string(out2)))
			}
		} else {
			return fmt.Errorf("%s", strings.TrimSpace(string(out)))
		}
	}

	fmt.Printf("⏳ Configuring %s (installing sshd, creating rhel user)...\n", aapSSHTargetContainer)

	steps := [][]string{
		{engine, "exec", aapSSHTargetContainer, "dnf", "install", "-y", "openssh-server"},
		{engine, "exec", aapSSHTargetContainer, "useradd", "-m", "rhel"},
		{engine, "exec", aapSSHTargetContainer, "bash", "-c", "touch /etc/ssh/trusted_user_ca_keys.pub"},
		{engine, "exec", aapSSHTargetContainer, "bash", "-c", "echo 'TrustedUserCAKeys /etc/ssh/trusted_user_ca_keys.pub' >> /etc/ssh/sshd_config"},
		{engine, "exec", aapSSHTargetContainer, "bash", "-c", "ssh-keygen -A && /usr/sbin/sshd"},
	}

	for _, step := range steps {
		if out, err := exec.Command(step[0], step[1:]...).CombinedOutput(); err != nil {
			// Ignore "user already exists" on idempotent re-runs.
			if strings.Contains(string(out), "already exists") {
				continue
			}
			return fmt.Errorf("container setup step %v: %s", step, strings.TrimSpace(string(out)))
		}
	}

	fmt.Printf("✅ %s started and configured.\n", aapSSHTargetContainer)
	return nil
}

func injectVaultCAPublicKey(engine string, client *vault.Client) error {
	secret, err := client.Logical().Read(vaultAAPSSHMountPath + "/config/ca")
	if err != nil {
		return fmt.Errorf("reading ssh/config/ca: %w", err)
	}
	if secret == nil {
		return fmt.Errorf("ssh/config/ca not found in Vault")
	}
	pubKey, ok := secret.Data["public_key"].(string)
	if !ok || strings.TrimSpace(pubKey) == "" {
		return fmt.Errorf("ssh/config/ca returned no public_key")
	}

	writeCmd := fmt.Sprintf("echo %q > /etc/ssh/trusted_user_ca_keys.pub", strings.TrimSpace(pubKey))
	if out, err := exec.Command(engine, "exec", aapSSHTargetContainer, "bash", "-c", writeCmd).CombinedOutput(); err != nil {
		return fmt.Errorf("writing CA public key to container: %s", strings.TrimSpace(string(out)))
	}

	// Reload sshd so it picks up the new trusted CA keys file.
	_ = exec.Command(engine, "exec", aapSSHTargetContainer, "pkill", "-HUP", "sshd").Run()

	fmt.Println("✅ Vault CA public key injected into hal-ssh-target.")
	return nil
}

func ensureSSHTargetInventory(c *aapAPIClient) error {
	inv, err := c.findInventory(vaultAAPSSHTargetInventoryName)
	if err != nil {
		return err
	}

	var inventoryID int
	if inv == nil {
		// Inventory requires an organization. Use the Development org (always
		// ensured before this call) so the inventory is visible to both envs.
		org, err := c.ensureOrganization(vaultAAPDevOrgName)
		if err != nil {
			return fmt.Errorf("resolving org for SSH target inventory: %w", err)
		}
		orgID := aapMapInt(org, "id")

		body, _, err := c.doJSON(http.MethodPost, "/inventories/", nil, map[string]interface{}{
			"name":         vaultAAPSSHTargetInventoryName,
			"description":  "Inventory for Vault-signed SSH demo target",
			"organization": orgID,
		})
		if err != nil {
			return fmt.Errorf("creating SSH target inventory: %w", err)
		}
		inventoryID = aapMapInt(body, "id")
	} else {
		inventoryID = aapMapInt(inv, "id")
	}

	// Ensure the hal-ssh-target host entry exists.
	hostsBody, _, err := c.doJSON(http.MethodGet, "/hosts/", url.Values{
		"name":      []string{aapSSHTargetContainer},
		"inventory": []string{strconv.Itoa(inventoryID)},
	}, nil)
	if err != nil {
		return fmt.Errorf("checking SSH target host: %w", err)
	}

	for _, item := range aapResults(hostsBody) {
		if aapMapString(item, "name") == aapSSHTargetContainer {
			return nil // already exists
		}
	}

	_, _, err = c.doJSON(http.MethodPost, "/hosts/", nil, map[string]interface{}{
		"name":      aapSSHTargetContainer,
		"inventory": inventoryID,
		"variables": "ansible_host: ssh-target.demo.local\nansible_user: rhel",
	})
	if err != nil {
		return fmt.Errorf("creating SSH target host: %w", err)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Sub-Task 3: AAP SSH credential provisioning
// ─────────────────────────────────────────────────────────────────────────────

func generateSSHKeyPair() (privatePEM, publicOpenSSH string, err error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return "", "", fmt.Errorf("generating RSA key: %w", err)
	}

	privBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privBlock := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: privBytes}
	privatePEM = string(pem.EncodeToMemory(privBlock))

	pubKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("encoding SSH public key: %w", err)
	}
	publicOpenSSH = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pubKey)))

	return privatePEM, publicOpenSSH, nil
}

func (c *aapAPIClient) findCredentialTypeForVaultSSH() (map[string]interface{}, error) {
	preferredNames := []string{
		"HashiCorp Vault Signed SSH (OIDC)",
		"HashiCorp Vault SSH (OIDC)",
		"HashiCorp Vault Signed SSH",
	}
	for _, name := range preferredNames {
		body, _, err := c.doJSON(http.MethodGet, "/credential_types/", url.Values{"name": []string{name}}, nil)
		if err != nil {
			continue
		}
		for _, item := range aapResults(body) {
			if aapMapString(item, "name") == name {
				return item, nil
			}
		}
	}
	return nil, fmt.Errorf("could not locate HashiCorp Vault Signed SSH credential type in AAP")
}

func (c *aapAPIClient) findMachineCredentialType() (map[string]interface{}, error) {
	body, _, err := c.doJSON(http.MethodGet, "/credential_types/", url.Values{"kind": []string{"ssh"}}, nil)
	if err != nil {
		return nil, err
	}
	for _, item := range aapResults(body) {
		if strings.ToLower(aapMapString(item, "kind")) == "ssh" {
			return item, nil
		}
	}
	return nil, fmt.Errorf("could not locate Machine (SSH) credential type in AAP")
}

// reconcileAAPSSHResources creates Vault Signed SSH and Machine credentials for each
// environment and links them via a credential input source. Returns the type IDs and
// a map of environment → machine credential ID.

func reconcileAAPSSHResources(
	c *aapAPIClient,
	envCfgs []aapEnvironmentConfig,
	privateKeyPEM, publicKeyOpenSSH string,
) (sshVaultTypeID, machineTypeID int, machineCredIDs map[string]int, err error) {
	sshVaultType, err := c.findCredentialTypeForVaultSSH()
	if err != nil {
		return 0, 0, nil, err
	}
	sshVaultTypeID = aapMapInt(sshVaultType, "id")

	machineType, err := c.findMachineCredentialType()
	if err != nil {
		return 0, 0, nil, err
	}
	machineTypeID = aapMapInt(machineType, "id")

	machineCredIDs = make(map[string]int, len(envCfgs))

	for _, cfg := range envCfgs {
		org, err := c.ensureOrganization(cfg.Organization)
		if err != nil {
			return 0, 0, nil, fmt.Errorf("org %s: %w", cfg.Organization, err)
		}
		orgID := aapMapInt(org, "id")

		// Vault Signed SSH (OIDC) credential fields: url, default_auth_path, jwt_role.
		sshVaultCred, err := c.ensureCredential(cfg.SSHVaultCredName, orgID, sshVaultTypeID, map[string]interface{}{
			"url":               vaultAAPAAPVaultURL,
			"default_auth_path": vaultAAPJWTMountPath,
			"jwt_role":          cfg.SSHJWTRoleName,
		})
		if err != nil {
			return 0, 0, nil, fmt.Errorf("Vault SSH credential %s: %w", cfg.SSHVaultCredName, err)
		}
		sshVaultCredID := aapMapInt(sshVaultCred, "id")

		// Machine credential — username + private key.
		machineCred, err := c.ensureCredential(cfg.SSHMachineCredName, orgID, machineTypeID, map[string]interface{}{
			"username":     "rhel",
			"ssh_key_data": privateKeyPEM,
		})
		if err != nil {
			return 0, 0, nil, fmt.Errorf("Machine credential %s: %w", cfg.SSHMachineCredName, err)
		}
		machineCredID := aapMapInt(machineCred, "id")
		machineCredIDs[cfg.Environment] = machineCredID

		// Credential input source: target field is ssh_public_key_data ("Signed SSH Certificate")
		// on the Machine credential. Metadata field names come from the OIDC type schema.
		if err := c.ensureCredentialInputSource(machineCredID, sshVaultCredID, "ssh_public_key_data", map[string]interface{}{
			"public_key":        publicKeyOpenSSH,
			"secret_path":       "ssh",
			"default_auth_path": vaultAAPJWTMountPath,
			"role":              vaultAAPSSHSignerRoleName,
			"valid_principals":  "rhel",
		}); err != nil {
			return 0, 0, nil, fmt.Errorf("input source signed_key for %s: %w", cfg.Environment, err)
		}
	}

	return sshVaultTypeID, machineTypeID, machineCredIDs, nil
}

func disableAAPSSHResources(c *aapAPIClient, envCfgs []aapEnvironmentConfig) error {
	sshVaultType, err := c.findCredentialTypeForVaultSSH()
	sshVaultTypeID := 0
	if err == nil && sshVaultType != nil {
		sshVaultTypeID = aapMapInt(sshVaultType, "id")
	}

	machineType, err := c.findMachineCredentialType()
	machineTypeID := 0
	if err == nil && machineType != nil {
		machineTypeID = aapMapInt(machineType, "id")
	}

	for _, cfg := range envCfgs {
		org, err := c.findOrganization(cfg.Organization)
		if err != nil || org == nil {
			continue
		}
		orgID := aapMapInt(org, "id")

		// Delete input source first.
		machineCred, err := c.findCredential(cfg.SSHMachineCredName, orgID, machineTypeID)
		if err == nil && machineCred != nil {
			machineCredID := aapMapInt(machineCred, "id")
			_ = c.deleteCredentialInputSource(machineCredID, "ssh_public_key_data")
		}

		if err := c.deleteCredential(cfg.SSHMachineCredName, orgID, machineTypeID); err != nil {
			return err
		}
		if err := c.deleteCredential(cfg.SSHVaultCredName, orgID, sshVaultTypeID); err != nil {
			return err
		}
	}
	return nil
}

func aapSSHResourcesReady(c *aapAPIClient, envCfgs []aapEnvironmentConfig) (bool, error) {
	sshVaultType, err := c.findCredentialTypeForVaultSSH()
	if err != nil {
		return false, nil
	}
	sshVaultTypeID := aapMapInt(sshVaultType, "id")

	machineType, err := c.findMachineCredentialType()
	if err != nil {
		return false, nil
	}
	machineTypeID := aapMapInt(machineType, "id")

	for _, cfg := range envCfgs {
		org, err := c.findOrganization(cfg.Organization)
		if err != nil {
			return false, err
		}
		if org == nil {
			return false, nil
		}
		orgID := aapMapInt(org, "id")

		sshVaultCred, err := c.findCredential(cfg.SSHVaultCredName, orgID, sshVaultTypeID)
		if err != nil {
			return false, err
		}
		if sshVaultCred == nil {
			return false, nil
		}

		machineCred, err := c.findCredential(cfg.SSHMachineCredName, orgID, machineTypeID)
		if err != nil {
			return false, err
		}
		if machineCred == nil {
			return false, nil
		}

		machineCredID := aapMapInt(machineCred, "id")
		inputSource, err := c.findCredentialInputSource(machineCredID, "ssh_public_key_data")
		if err != nil {
			return false, err
		}
		if inputSource == nil {
			return false, nil
		}
	}

	return true, nil
}
