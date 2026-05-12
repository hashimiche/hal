package vault

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hal/internal/global"

	vault "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
)

var (
	pkiEnable  bool
	pkiDisable bool
	pkiUpdate  bool

	// PKI engine config
	pkiRootMount      string
	pkiIntMount       string
	pkiRootTTL        string
	pkiIntTTL         string
	pkiAllowedDomains string
	pkiMaxCertTTL     string

	// K8s / cert-manager integration
	pkiK8s                bool
	pkiACME               bool
	pkiForce              bool
	pkiKindNodeImage      string
	pkiCertManagerVersion string
	pkiWebBackendImage    string
	pkiWebBackendTag      string
	pkiCaddyImage         string
	pkiCaddyTag           string
	pkiACMECertTTL        string
)

var vaultPKICmd = &cobra.Command{
	Use:   "pki [status|enable|disable|update]",
	Short: "Manage Vault PKI engines (Root CA, Intermediate CA, cert-manager K8s or ACME/Caddy demo)",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := parseLifecycleAction(args, &pkiEnable, &pkiDisable, &pkiUpdate); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}
		isPodman := strings.Contains(engine, "podman")

		client, vaultErr := GetHealthyClient()

		// ==========================================
		// 1. SMART STATUS (default)
		// ==========================================
		if !pkiEnable && !pkiDisable && !pkiUpdate {
			fmt.Println("🔍 Checking Vault PKI Status...")

			if vaultErr != nil {
				fmt.Printf("  ❌ Vault         : Unreachable (%v)\n", vaultErr)
				fmt.Println("\n💡 Next Step: hal vault create")
				return
			}

			fmt.Println("  [ Vault PKI Engines ]")
			mounts, _ := client.Sys().ListMounts()
			rootMounted := mounts != nil && mounts[pkiRootMount+"/"] != nil
			intMounted := mounts != nil && mounts[pkiIntMount+"/"] != nil

			if rootMounted {
				fmt.Printf("  ✅ %-14s : Mounted\n", pkiRootMount)
				caResp, _ := client.Logical().Read(pkiRootMount + "/cert/ca")
				if caResp != nil && caResp.Data["certificate"] != "" {
					fmt.Println("  ✅ Root CA       : Generated")
				} else {
					fmt.Println("  ⚠️  Root CA       : Not yet generated")
				}
			} else {
				fmt.Printf("  ❌ %-14s : Not mounted\n", pkiRootMount)
			}

			if intMounted {
				fmt.Printf("  ✅ %-14s : Mounted\n", pkiIntMount)
				caResp, _ := client.Logical().Read(pkiIntMount + "/cert/ca")
				if caResp != nil && caResp.Data["certificate"] != "" {
					fmt.Println("  ✅ Intermediate  : Signed and installed")
				} else {
					fmt.Println("  ⚠️  Intermediate  : CSR not yet signed")
				}
				roleResp, _ := client.Logical().Read(pkiIntMount + "/roles/hal-role")
				if roleResp != nil {
					fmt.Println("  ✅ Role hal-role : Configured")
				} else {
					fmt.Println("  ⚠️  Role hal-role : Not found")
				}
			} else {
				fmt.Printf("  ❌ %-14s : Not mounted\n", pkiIntMount)
			}

			// K8s / cert-manager layer
			clusterOut, _ := exec.Command("kind", "get", "clusters").Output()
			clusterRunning := strings.Contains(string(clusterOut), "kind")

			if clusterRunning {
				fmt.Println("\n  [ Kubernetes / cert-manager ]")
				cmOut, _ := exec.Command("helm", "list", "-n", "cert-manager", "-q").Output()
				certManagerInstalled := strings.Contains(string(cmOut), "cert-manager")

				if certManagerInstalled {
					fmt.Println("  ✅ cert-manager  : Deployed")

					issuerOut, _ := exec.Command(
						"kubectl", "get", "clusterissuer", "vault-pki-issuer",
						"-o", "jsonpath={.status.conditions[0].type}",
					).Output()
					if strings.TrimSpace(string(issuerOut)) == "Ready" {
						fmt.Println("  ✅ ClusterIssuer : Ready (vault-pki-issuer)")
					} else {
						fmt.Println("  ⚠️  ClusterIssuer : Not ready")
					}

					certOut, _ := exec.Command(
						"kubectl", "get", "certificate", "hal-web-pki-cert",
						"-n", "pki-demo", "-o", "jsonpath={.status.conditions[0].type}",
					).Output()
					if strings.TrimSpace(string(certOut)) == "Ready" {
						fmt.Println("  ✅ Certificate   : Issued (hal-web-pki-cert)")
					} else {
						fmt.Println("  ⚠️  Certificate   : Not yet issued")
					}

					podOut, _ := exec.Command(
						"kubectl", "get", "pods", "-n", "pki-demo",
						"-l", "app=hal-web-pki", "-o", "jsonpath={.items[0].status.phase}",
					).Output()
					if strings.TrimSpace(string(podOut)) == "Running" {
						fmt.Println("  ✅ Web Pod       : Running (pki-demo/hal-web-pki)")
						fmt.Println("\n  Access (cert-manager):")
						fmt.Println("    → https://pki.localhost:8089")
					} else {
						fmt.Println("  ⚠️  Web Pod       : Not running")
					}
				} else {
					fmt.Println("  ⚪ cert-manager  : Not installed (hal vault pki enable --k8s)")
				}

				// ACME / Caddy layer
				fmt.Println("\n  [ ACME / Caddy ]")
				acmeNsOut, _ := exec.Command("kubectl", "get", "namespace", "pki-acme-demo", "--ignore-not-found").Output()
				if strings.Contains(string(acmeNsOut), "pki-acme-demo") {
					acmePodOut, _ := exec.Command(
						"kubectl", "get", "pods", "-n", "pki-acme-demo",
						"-l", "app=hal-caddy-acme", "-o", "jsonpath={.items[0].status.phase}",
					).Output()
					if strings.TrimSpace(string(acmePodOut)) == "Running" {
						fmt.Println("  ✅ Caddy Pod     : Running (pki-acme-demo/hal-caddy-acme)")
						fmt.Println("\n  Access (ACME):")
						fmt.Println("    → https://acme.localhost:8090")
					} else {
						fmt.Println("  ⚠️  Caddy Pod     : Not running")
					}
				} else {
					fmt.Println("  ⚪ ACME/Caddy    : Not installed (hal vault pki enable --acme)")
				}
			}

			fmt.Println("\n💡 Next Step:")
			if !rootMounted || !intMounted {
				fmt.Println("   hal vault pki enable")
				fmt.Println("   hal vault pki enable --k8s    → also deploy cert-manager + web demo")
				fmt.Println("   hal vault pki enable --acme   → also deploy Vault ACME + Caddy demo")
			} else {
				fmt.Println("   vault write " + pkiIntMount + "/issue/hal-role common_name=\"test.hal.local\" ttl=\"24h\"")
				if clusterRunning {
					cmOut, _ := exec.Command("helm", "list", "-n", "cert-manager", "-q").Output()
					if !strings.Contains(string(cmOut), "cert-manager") {
						fmt.Println("   hal vault pki enable --k8s    → add cert-manager integration")
					}
					acmeNsOut, _ := exec.Command("kubectl", "get", "namespace", "pki-acme-demo", "--ignore-not-found").Output()
					if !strings.Contains(string(acmeNsOut), "pki-acme-demo") {
						fmt.Println("   hal vault pki enable --acme   → add ACME/Caddy integration")
					}
				} else {
					fmt.Println("   hal vault pki enable --k8s    → add cert-manager integration")
					fmt.Println("   hal vault pki enable --acme   → add ACME/Caddy integration")
				}
			}
			return
		}

		// ==========================================
		// 2. DISABLE
		// ==========================================
		// update --k8s/--acme (without --force) skips PKI teardown entirely —
		// they only reconcile the demo layer on top of existing engines.
		// Teardown runs for: explicit disable, plain update, update --k8s/--acme --force.
		skipTeardown := pkiUpdate && (pkiK8s || pkiACME) && !pkiForce
		if (pkiDisable || pkiUpdate) && !skipTeardown {
			if global.DryRun {
				fmt.Printf("[DRY RUN] Would unmount '%s' and '%s' from Vault\n", pkiRootMount, pkiIntMount)
				fmt.Println("[DRY RUN] Would disable Vault auth mounts 'kubernetes-pki' and 'kubernetes-acme' and delete policy 'hal-pki-issuer'")
				fmt.Println("[DRY RUN] Would uninstall cert-manager and delete pki-demo namespace (if deployed)")
				fmt.Println("[DRY RUN] Would delete pki-acme-demo namespace (if deployed)")
				fmt.Println("[DRY RUN] Would delete KinD cluster if hal vault k8s is not active")
			} else {
				fmt.Println("🛑 Tearing down Vault PKI...")

				if vaultErr == nil && client != nil {
					if err := client.Sys().Unmount(pkiRootMount); err == nil {
						fmt.Printf("  ✅ Unmounted '%s'\n", pkiRootMount)
					} else {
						fmt.Printf("  ⚠️  %s: %v\n", pkiRootMount, err)
					}
					if err := client.Sys().Unmount(pkiIntMount); err == nil {
						fmt.Printf("  ✅ Unmounted '%s'\n", pkiIntMount)
					} else {
						fmt.Printf("  ⚠️  %s: %v\n", pkiIntMount, err)
					}
					if err := client.Sys().DisableAuth("kubernetes-pki"); err == nil {
						fmt.Println("  ✅ Disabled auth mount 'kubernetes-pki'.")
					}
					if err := client.Sys().DisableAuth("kubernetes-acme"); err == nil {
						fmt.Println("  ✅ Disabled auth mount 'kubernetes-acme'.")
					}
					_ = client.Sys().DeletePolicy("hal-pki-issuer")
					fmt.Println("  ✅ Policy 'hal-pki-issuer' removed.")
				} else {
					fmt.Println("  ⚠️  Vault unreachable — skipping Vault-side cleanup.")
				}

				// cert-manager demo
				cmOut, _ := exec.Command("helm", "list", "-n", "cert-manager", "-q").Output()
				if strings.Contains(string(cmOut), "cert-manager") {
					fmt.Println("⚙️  Removing cert-manager (detected from previous --k8s enable)...")
					_ = exec.Command("kubectl", "delete", "clusterissuer", "vault-pki-issuer", "--ignore-not-found").Run()
					_ = exec.Command("kubectl", "delete", "namespace", "pki-demo", "--ignore-not-found").Run()
					_ = exec.Command("helm", "uninstall", "cert-manager", "-n", "cert-manager").Run()
					_ = exec.Command("kubectl", "delete", "namespace", "cert-manager", "--ignore-not-found").Run()
					fmt.Println("  ✅ cert-manager and pki-demo namespace removed.")
				}

				// ACME/Caddy demo
				acmeNsOut, _ := exec.Command("kubectl", "get", "namespace", "pki-acme-demo", "--ignore-not-found").Output()
				if strings.Contains(string(acmeNsOut), "pki-acme-demo") {
					fmt.Println("⚙️  Removing ACME/Caddy demo...")
					_ = exec.Command("kubectl", "delete", "namespace", "pki-acme-demo", "--ignore-not-found").Run()
					fmt.Println("  ✅ pki-acme-demo namespace removed.")
				}
				// Remove the CoreDNS sidecar used for ACME challenge DNS resolution.
				_ = exec.Command(engine, "rm", "-f", "hal-acme-dns").Run()

				// Conditionally delete KinD cluster
				vsoOut, _ := exec.Command("helm", "list", "-n", "vso", "-q").Output()
				if strings.Contains(string(vsoOut), "vault-secrets-operator") {
					fmt.Println("ℹ️  KinD cluster preserved (hal vault k8s is still active).")
					fmt.Println("   Run 'hal vault k8s disable' to remove it.")
				} else {
					clusterOut, _ := exec.Command("kind", "get", "clusters").Output()
					if strings.Contains(string(clusterOut), "kind") {
						fmt.Println("ℹ️  No active VSO deployment — removing KinD cluster.")
						_ = exec.Command("kind", "delete", "cluster").Run()
						fmt.Println("  ✅ KinD cluster deleted.")
					}
				}

				fmt.Println("\n✅ Vault PKI teardown complete.")
				fmt.Println("💡 Next Step: hal vault pki enable")
			}

			if pkiDisable && !global.DryRun {
				return
			}
		}

		// ==========================================
		// 3. ENABLE / UPDATE
		// ==========================================
		if pkiEnable || pkiUpdate {
			if vaultErr != nil {
				fmt.Printf("❌ Vault must be running and healthy: %v\n", vaultErr)
				return
			}

			if pkiK8s || pkiACME {
				for _, bin := range []string{"kind", "kubectl", "helm"} {
					if _, err := exec.LookPath(bin); err != nil {
						fmt.Printf("❌ '%s' not found in PATH (required for --k8s / --acme).\n", bin)
						return
					}
				}
			}

			if global.DryRun {
				if pkiUpdate && (pkiK8s || pkiACME) && !pkiForce {
					if pkiK8s {
						fmt.Println("[DRY RUN] Would reconcile cert-manager layer only (PKI engines left intact)")
					}
					if pkiACME {
						fmt.Println("[DRY RUN] Would reconcile ACME/Caddy layer only (PKI engines left intact)")
					}
					fmt.Println("[DRY RUN] Use --force to also rebuild Root CA and Intermediate CA")
				} else {
					fmt.Printf("[DRY RUN] Would enable PKI engines at '%s' (5y) and '%s' (2y)\n", pkiRootMount, pkiIntMount)
					fmt.Printf("[DRY RUN] Would generate Root CA (TTL %s) and Intermediate CA (TTL %s)\n", pkiRootTTL, pkiIntTTL)
					fmt.Printf("[DRY RUN] Would create role 'hal-role' (domains: %s, max TTL: %s)\n", pkiAllowedDomains, pkiMaxCertTTL)
					if pkiK8s {
						fmt.Println("[DRY RUN] Would deploy cert-manager (Jetstack) + ClusterIssuer + nginx web demo pod")
					}
					if pkiACME {
						fmt.Println("[DRY RUN] Would enable Vault ACME endpoint on pki-int + deploy Caddy demo pod")
					}
				}
				return
			}

			// update --k8s/--acme (without --force): only reconcile the demo layer.
			// The PKI engines are left completely intact — no CA rebuild.
			if pkiUpdate && !pkiForce {
				if pkiK8s || pkiACME {
					mounts, _ := client.Sys().ListMounts()
					if mounts == nil || mounts[pkiIntMount+"/"] == nil {
						fmt.Printf("❌ Vault mount '%s' not found. Run 'hal vault pki enable' first.\n", pkiIntMount)
						fmt.Println("   Use 'hal vault pki update --force' to rebuild everything from scratch.")
						return
					}
					if pkiK8s {
						fmt.Println("♻️  Reconciling cert-manager layer (PKI engines preserved)...")
						runPKIK8sEnable(client, engine, isPodman, pkiIntMount)
					}
					if pkiACME {
						fmt.Println("♻️  Reconciling ACME/Caddy layer (PKI engines preserved)...")
						runPKIACMEEnable(client, engine, isPodman, pkiIntMount)
					}
					return
				}
			}

			// enable, plain update, or update --force: full CA rebuild.
			runVaultPKISetup(client, pkiUpdate)

			if pkiK8s {
				runPKIK8sEnable(client, engine, isPodman, pkiIntMount)
			}
			if pkiACME {
				runPKIACMEEnable(client, engine, isPodman, pkiIntMount)
			}
		}
	},
}

// runVaultPKISetup creates the Root CA and Intermediate CA chain in Vault.
func runVaultPKISetup(client *vault.Client, isUpdate bool) {
	if isUpdate {
		fmt.Printf("♻️  Reconciling PKI — unmounting '%s' and '%s'...\n", pkiRootMount, pkiIntMount)
		_ = client.Sys().Unmount(pkiRootMount)
		_ = client.Sys().Unmount(pkiIntMount)
	}

	// ---- Root CA ----
	fmt.Printf("🔐 Enabling PKI engine at '%s' (max TTL: %s = 5y)...\n", pkiRootMount, pkiRootTTL)
	if err := client.Sys().Mount(pkiRootMount, &vault.MountInput{
		Type:   "pki",
		Config: vault.MountConfigInput{MaxLeaseTTL: pkiRootTTL},
	}); err != nil {
		fmt.Printf("  ⚠️  Mount error (may already exist) — tuning TTL and continuing...\n")
		_ = client.Sys().TuneMount(pkiRootMount, vault.MountConfigInput{MaxLeaseTTL: pkiRootTTL})
	}
	fmt.Println("📜 Generating Root CA (internal RSA-4096 key)...")
	rootResp, err := client.Logical().Write(pkiRootMount+"/root/generate/internal", map[string]interface{}{
		"common_name": "HAL Root CA",
		"ttl":         pkiRootTTL,
		"key_type":    "rsa",
		"key_bits":    4096,
	})
	if err != nil || rootResp == nil {
		fmt.Printf("❌ Failed to generate Root CA: %v\n", err)
		return
	}
	fmt.Println("  ✅ Root CA generated.")
	_, _ = client.Logical().Write(pkiRootMount+"/config/urls", map[string]interface{}{
		"issuing_certificates":    "http://vault.localhost:8200/v1/" + pkiRootMount + "/ca",
		"crl_distribution_points": "http://vault.localhost:8200/v1/" + pkiRootMount + "/crl",
	})

	// ---- Intermediate CA ----
	fmt.Printf("🔐 Enabling PKI engine at '%s' (max TTL: %s = 2y)...\n", pkiIntMount, pkiIntTTL)
	if err := client.Sys().Mount(pkiIntMount, &vault.MountInput{
		Type:   "pki",
		Config: vault.MountConfigInput{MaxLeaseTTL: pkiIntTTL},
	}); err != nil {
		fmt.Printf("  ⚠️  Mount error (may already exist) — tuning TTL and continuing...\n")
		_ = client.Sys().TuneMount(pkiIntMount, vault.MountConfigInput{MaxLeaseTTL: pkiIntTTL})
	}
	if err := tunePKIACMEHeaders(client, pkiIntMount); err != nil {
		fmt.Printf("  ⚠️  Could not tune ACME headers on '%s': %v\n", pkiIntMount, err)
	} else {
		fmt.Printf("  ✅ Tuned ACME response/request headers on '%s'.\n", pkiIntMount)
	}
	fmt.Println("📝 Generating Intermediate CA CSR (RSA-4096)...")
	csrResp, err := client.Logical().Write(pkiIntMount+"/intermediate/generate/internal", map[string]interface{}{
		"common_name": "HAL Intermediate CA",
		"key_type":    "rsa",
		"key_bits":    4096,
	})
	if err != nil || csrResp == nil {
		fmt.Printf("❌ Failed to generate Intermediate CSR: %v\n", err)
		return
	}
	csr, _ := csrResp.Data["csr"].(string)
	fmt.Println("  ✅ CSR generated.")

	fmt.Println("✍️  Signing Intermediate CA with Root CA...")
	signResp, err := client.Logical().Write(pkiRootMount+"/root/sign-intermediate", map[string]interface{}{
		"csr":    csr,
		"format": "pem_bundle",
		"ttl":    pkiIntTTL,
	})
	if err != nil || signResp == nil {
		fmt.Printf("❌ Failed to sign Intermediate CA: %v\n", err)
		return
	}
	signedCert, _ := signResp.Data["certificate"].(string)
	fmt.Println("  ✅ Intermediate CA signed by Root CA.")

	if _, err := client.Logical().Write(pkiIntMount+"/intermediate/set-signed", map[string]interface{}{
		"certificate": signedCert,
	}); err != nil {
		fmt.Printf("❌ Failed to set signed certificate: %v\n", err)
		return
	}
	fmt.Println("  ✅ Intermediate CA certificate installed.")
	_, _ = client.Logical().Write(pkiIntMount+"/config/urls", map[string]interface{}{
		"issuing_certificates":    "http://vault.localhost:8200/v1/" + pkiIntMount + "/ca",
		"crl_distribution_points": "http://vault.localhost:8200/v1/" + pkiIntMount + "/crl",
	})

	// ---- Role ----
	fmt.Printf("⚙️  Creating role 'hal-role' on '%s'...\n", pkiIntMount)
	_, _ = client.Logical().Write(pkiIntMount+"/roles/hal-role", map[string]interface{}{
		"allowed_domains":     pkiAllowedDomains,
		"allow_subdomains":    true,
		"allow_bare_domains":  false,
		"allow_ip_sans":       true,
		"use_csr_common_name": true,
		"max_ttl":             pkiMaxCertTTL,
		"key_type":            "rsa",
		"key_bits":            2048,
	})
	fmt.Println("  ✅ Role 'hal-role' created.")

	// ---- Policy ----
	pkiPolicy := fmt.Sprintf(`
path "%s/sign/hal-role"  { capabilities = ["create", "update"] }
path "%s/issue/hal-role" { capabilities = ["create", "update"] }
`, pkiIntMount, pkiIntMount)
	_ = client.Sys().PutPolicy("hal-pki-issuer", pkiPolicy)
	fmt.Println("  ✅ Policy 'hal-pki-issuer' written.")

	// ---- ACME endpoint + short-lived demo role ----
	// Enable the built-in Vault ACME directory on pki-int so Caddy (or any
	// ACME client) can request certs directly from Vault without cert-manager.
	// We also create a dedicated 'acme-demo' role scoped to pkiACMECertTTL so
	// the Caddy demo page can show live auto-renewal within minutes.
	fmt.Printf("⚙️  Enabling ACME endpoint on '%s'...\n", pkiIntMount)
	_, _ = client.Logical().Write(pkiIntMount+"/config/acme", map[string]interface{}{
		"enabled": true,
	})
	// config/acme max_ttl caps the TTL of all certs issued via ACME on this mount.
	// Without this, Vault imposes a high default (2160h) which overrides the role TTL.
	_, _ = client.Logical().Write(pkiIntMount+"/config/acme", map[string]interface{}{
		"enabled": true,
		"max_ttl": pkiACMECertTTL,
	})
	_, _ = client.Logical().Write(pkiIntMount+"/config/cluster", map[string]interface{}{
		"path": "http://vault.localhost:8200/v1/" + pkiIntMount,
	})
	_, _ = client.Logical().Write(pkiIntMount+"/roles/acme-demo", map[string]interface{}{
		"allowed_domains":     pkiAllowedDomains,
		"allow_subdomains":    true,
		"allow_bare_domains":  true,
		"allow_any_name":      true,
		"allow_ip_sans":       true,
		"use_csr_common_name": true,
		"ttl":                 pkiACMECertTTL,
		"max_ttl":             pkiACMECertTTL,
		"key_type":            "rsa",
		"key_bits":            2048,
		"no_store":            false,
	})
	fmt.Println("  ✅ ACME directory enabled.")
	fmt.Printf("     Role 'acme-demo' TTL: %s (Caddy will auto-renew at ~1/3 lifetime)\n", pkiACMECertTTL)
	fmt.Printf("     http://vault.localhost:8200/v1/%s/roles/acme-demo/acme/directory\n", pkiIntMount)

	fmt.Println("\n✅ Vault PKI setup complete!")
	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  Key storage: Vault-internal (private keys never leave Vault)")
	fmt.Println("  To remove: hal vault pki disable")
	fmt.Println("\n  Engine Layout:")
	fmt.Printf("    Root CA  : %s  (max TTL %s = 5y)\n", pkiRootMount, pkiRootTTL)
	fmt.Printf("    Int  CA  : %s   (max TTL %s = 2y)\n", pkiIntMount, pkiIntTTL)
	fmt.Printf("    Role     : %s/roles/hal-role\n", pkiIntMount)
	fmt.Println("\n  Issue a certificate (root token):")
	fmt.Printf("    vault write %s/issue/hal-role \\\n", pkiIntMount)
	fmt.Println(`      common_name="test.hal.local" \`)
	fmt.Println(`      ttl="24h"`)
	fmt.Println("\n  Read Root CA certificate:")
	fmt.Printf("    vault read -field=certificate %s/cert/ca\n", pkiRootMount)
	fmt.Println("\n  Read Intermediate CA certificate:")
	fmt.Printf("    vault read -field=certificate %s/cert/ca\n", pkiIntMount)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	if !pkiK8s && !pkiACME {
		fmt.Println("\n💡 Next Step:")
		fmt.Println("   hal vault pki enable --k8s    → deploy cert-manager + web demo")
		fmt.Println("   hal vault pki enable --acme   → deploy Vault ACME + Caddy demo")
	}
}

// runPKIK8sEnable deploys cert-manager (Jetstack) + ClusterIssuer + web demo pod.
func runPKIK8sEnable(client *vault.Client, engine string, isPodman bool, intMount string) {
	// Verify role exists before continuing
	roleResp, err := client.Logical().Read(intMount + "/roles/hal-role")
	if err != nil || roleResp == nil {
		fmt.Printf("❌ Role 'hal-role' not found on '%s'. Run 'hal vault pki enable' first.\n", intMount)
		return
	}

	// ---- KinD cluster ----
	clusterOut, _ := exec.Command("kind", "get", "clusters").Output()
	if strings.Contains(string(clusterOut), "kind") {
		fmt.Println("⚡ KinD cluster already running — reusing it.")
	} else {
		fmt.Println("🚀 Booting KinD cluster (attached to hal-net)...")
		kindConfigPath, err := writeHALKindConfig()
		if err != nil {
			fmt.Printf("❌ Failed to prepare KinD config: %v\n", err)
			return
		}
		defer os.Remove(kindConfigPath)

		startCmd := exec.Command("kind", "create", "cluster", "--config", kindConfigPath)
		if strings.TrimSpace(pkiKindNodeImage) != "" {
			startCmd.Args = append(startCmd.Args, "--image", pkiKindNodeImage)
		}
		env := os.Environ()
		if isPodman {
			env = append(env, "KIND_EXPERIMENTAL_PROVIDER=podman")
		}
		env = append(env, "KIND_EXPERIMENTAL_DOCKER_NETWORK=hal-net")
		startCmd.Env = env
		startCmd.Stdout = os.Stdout
		startCmd.Stderr = os.Stderr
		if err := startCmd.Run(); err != nil {
			fmt.Printf("❌ Failed to start KinD: %v\n", err)
			return
		}
	}

	// ---- cert-manager (Jetstack OCI chart) ----
	// Use the official Jetstack OCI chart (no helm repo add required) with
	// webhook.hostNetwork=true so the API server inside the kind-control-plane
	// container can reach the webhook pod. Without hostNetwork, kube-proxy
	// iptables routing is used and the API server gets i/o timeout or
	// connection refused because it can't route to cluster-internal pod IPs.
	// securePort=10260 avoids a conflict with the kubelet on port 10250.
	fmt.Println("⚙️  Deploying cert-manager via Helm (Jetstack OCI, hostNetwork for KinD)...")
	cmArgs := []string{
		"upgrade", "--install", "cert-manager",
		"oci://quay.io/jetstack/charts/cert-manager",
		"-n", "cert-manager", "--create-namespace",
		"--set", "installCRDs=true",
		"--set", "webhook.hostNetwork=true",
		"--set", "webhook.securePort=10260",
	}
	if strings.TrimSpace(pkiCertManagerVersion) != "" {
		cmArgs = append(cmArgs, "--version", pkiCertManagerVersion)
	}
	cmCmd := exec.Command("helm", cmArgs...)
	cmCmd.Stdout = os.Stdout
	cmCmd.Stderr = os.Stderr
	if err := cmCmd.Run(); err != nil {
		fmt.Printf("❌ Failed to install cert-manager: %v\n", err)
		return
	}
	// Wait for the webhook rollout. With hostNetwork the webhook is on the
	// node's IP so reachability is no longer the issue, but TLS cert init
	// still takes a few seconds.
	fmt.Println("⏳ Waiting for cert-manager-webhook rollout (up to 60s)...")
	_ = exec.Command(
		"kubectl", "rollout", "status",
		"deployment/cert-manager-webhook",
		"-n", "cert-manager", "--timeout=60s",
	).Run()
	fmt.Println("⏳ Allowing webhook TLS to initialise (5s)...")
	time.Sleep(5 * time.Second)

	// ---- Vault kubernetes-pki/ auth mount ----
	fmt.Println("⚙️  Configuring dedicated Vault Kubernetes auth mount 'kubernetes-pki/'...")
	pkiAuthPolicy := fmt.Sprintf(`
path "%s/sign/hal-role"  { capabilities = ["create", "update"] }
path "%s/issue/hal-role" { capabilities = ["create", "update"] }
`, intMount, intMount)
	_ = client.Sys().PutPolicy("hal-pki-issuer", pkiAuthPolicy)

	if err := configurePKIKubeAuth(client, engine); err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}

	// ---- Vault IP ----
	vaultIPOut, _ := exec.Command(
		engine, "inspect",
		"-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}",
		"hal-vault",
	).Output()
	vaultIP := strings.TrimSpace(string(vaultIPOut))
	if vaultIP == "" {
		vaultIP = "hal-vault"
	}
	// Ensure ACME endpoint links (new-nonce/new-account/new-order) advertised by Vault
	// are reachable from in-cluster ACME clients (Caddy) during --acme reconcile.
	_, _ = client.Logical().Write(intMount+"/config/cluster", map[string]interface{}{
		"path": "http://" + vaultIP + ":8200/v1/" + intMount,
	})

	// ---- Apply manifests in two phases ----
	// Phase 1: Namespace, Deployment, Service — standard K8s resources, no cert-manager
	// webhook validation. Apply immediately with no retry needed.
	fmt.Println("⚙️  Applying core manifests (Namespace, Deployment, Service)...")
	coreManifests := buildPKIK8sCoreManifests(intMount, pkiWebBackendImage+":"+pkiWebBackendTag)
	coreCmd := exec.Command("kubectl", "apply", "-f", "-")
	coreCmd.Stdin = strings.NewReader(coreManifests)
	coreCmd.Stdout = os.Stdout
	coreCmd.Stderr = os.Stderr
	if err := coreCmd.Run(); err != nil {
		fmt.Printf("❌ Failed to apply core manifests: %v\n", err)
		return
	}

	// Phase 2: ClusterIssuer, Certificate — validated by the cert-manager webhook.
	// The webhook TLS server may still be warming up even after rollout completes,
	// so retry until it accepts the connection.
	fmt.Println("⚙️  Applying cert-manager CRDs (ClusterIssuer, Certificate) — retrying if webhook not ready...")
	crdManifests := buildPKIK8sCRDManifests(vaultIP, intMount)
	var applyErr error
	for attempt := 1; attempt <= 10; attempt++ {
		applyCmd := exec.Command("kubectl", "apply", "-f", "-")
		applyCmd.Stdin = strings.NewReader(crdManifests)
		applyCmd.Stdout = os.Stdout
		applyCmd.Stderr = os.Stderr
		applyErr = applyCmd.Run()
		if applyErr == nil {
			break
		}
		if attempt < 10 {
			fmt.Printf("  ⚠️  Attempt %d/10 failed (webhook warming up) — retrying in 10s...\n", attempt)
			time.Sleep(10 * time.Second)
		}
	}
	if applyErr != nil {
		fmt.Printf("❌ Failed to apply cert-manager CRDs after 10 attempts: %v\n", applyErr)
		return
	}

	fmt.Println("⏳ Waiting for TLS Certificate to be issued (up to 60s)...")
	certWaitErr := exec.Command(
		"kubectl", "wait", "--for=condition=Ready",
		"certificate/hal-web-pki-cert", "-n", "pki-demo", "--timeout=60s",
	).Run()
	if certWaitErr != nil {
		fmt.Println("❌ Certificate was not issued within 60s. Diagnosis:")
		// Condition message from the Certificate CR
		condOut, _ := exec.Command(
			"kubectl", "get", "certificate", "hal-web-pki-cert", "-n", "pki-demo",
			"-o", `jsonpath={range .status.conditions[*]}  {.type}: {.reason} - {.message}\n{end}`,
		).Output()
		if len(condOut) > 0 {
			fmt.Println(string(condOut))
		}
		// Most recent CertificateRequest failure
		crOut, _ := exec.Command(
			"kubectl", "get", "certificaterequest", "-n", "pki-demo",
			"-o", `jsonpath={range .items[*]}  CertificateRequest {.metadata.name}: {range .status.conditions[*]}{.type}={.reason} ({.message})\n{end}{end}`,
		).Output()
		if len(crOut) > 0 {
			fmt.Println(string(crOut))
		}
		// Recent events in pki-demo
		evtOut, _ := exec.Command(
			"kubectl", "get", "events", "-n", "pki-demo",
			"--sort-by=.lastTimestamp",
			"--field-selector=reason!=Scheduled,reason!=Pulled,reason!=Created,reason!=Started",
		).Output()
		if len(evtOut) > 0 {
			fmt.Println(string(evtOut))
		}
		fmt.Println("\n💡 Run 'hal vault pki status' to recheck once the issue is resolved.")
		return
	}

	fmt.Println("⏳ Waiting for web pod to be Ready (up to 30s)...")
	_ = exec.Command(
		"kubectl", "wait", "--for=condition=Ready",
		"pod", "-l", "app=hal-web-pki", "-n", "pki-demo", "--timeout=30s",
	).Run()

	fmt.Println("\n✅ PKI Kubernetes demo deployed!")
	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  What was deployed:")
	fmt.Println("    - cert-manager (namespace: cert-manager, Jetstack chart)")
	fmt.Printf("    - ClusterIssuer vault-pki-issuer → %s/sign/hal-role\n", intMount)
	fmt.Println("    - Certificate hal-web-pki-cert (namespace: pki-demo)")
	fmt.Printf("    - Web pod hal-web-pki (%s, TLS cert mounted at /tls)\n", pkiWebBackendImage+":"+pkiWebBackendTag)
	fmt.Println("\n  Access:")
	fmt.Println("    → https://pki.localhost:8089")
	fmt.Println("\n  Inspect the certificate:")
	fmt.Println("    kubectl describe certificate hal-web-pki-cert -n pki-demo")
	fmt.Println("    kubectl get secret hal-web-pki-tls -n pki-demo -o jsonpath='{.data.tls\\.crt}' | base64 -d | openssl x509 -noout -text")
	fmt.Println("\n  Issue a cert directly from Vault:")
	fmt.Printf("    vault write %s/issue/hal-role common_name=\"test.hal.local\" ttl=\"24h\"\n", intMount)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}

// configurePKIKubeAuth sets up the dedicated 'kubernetes-pki/' auth mount in Vault.
// It is always recreated fresh to stay independent of the 'kubernetes/' mount
// that may be used by hal vault k8s.
func configurePKIKubeAuth(client *vault.Client, engine string) error {
	const authMount = "kubernetes-pki"
	_ = client.Sys().DisableAuth(authMount)

	// vault-reviewer SA — shared infra, idempotent
	_ = exec.Command("kubectl", "create", "sa", "vault-reviewer", "-n", "default").Run()
	_ = exec.Command("kubectl", "create", "clusterrolebinding", "vault-reviewer-binding",
		"--clusterrole=system:auth-delegator",
		"--serviceaccount=default:vault-reviewer").Run()

	caOut, _ := exec.Command("kubectl", "config", "view", "--raw", "--minify", "--flatten",
		"-o", "jsonpath={.clusters[].cluster.certificate-authority-data}").Output()
	decodedCA, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(string(caOut)))

	tokenOut, _ := exec.Command("kubectl", "create", "token", "vault-reviewer",
		"-n", "default", "--duration=87600h").Output()
	reviewerToken := strings.TrimSpace(string(tokenOut))

	kindIPOut, _ := exec.Command(engine, "inspect",
		"-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}",
		"kind-control-plane").Output()
	kindIP := strings.TrimSpace(string(kindIPOut))
	if kindIP == "" {
		kindIP = "kind-control-plane"
	}

	if err := client.Sys().EnableAuthWithOptions(authMount, &vault.EnableAuthOptions{Type: "kubernetes"}); err != nil {
		return fmt.Errorf("failed to enable %s auth: %w", authMount, err)
	}
	if _, err := client.Logical().Write("auth/"+authMount+"/config", map[string]interface{}{
		"kubernetes_host":        fmt.Sprintf("https://%s:6443", kindIP),
		"kubernetes_ca_cert":     string(decodedCA),
		"token_reviewer_jwt":     reviewerToken,
		"disable_iss_validation": true,
	}); err != nil {
		return fmt.Errorf("failed to configure %s auth: %w", authMount, err)
	}
	fmt.Printf("  ✅ Auth mount '%s/' configured.\n", authMount)

	// Dedicated SA for cert-manager → Vault
	_ = exec.Command("kubectl", "create", "sa", "cert-manager-vault", "-n", "cert-manager").Run()
	cmTokenOut, cmErr := exec.Command("kubectl", "create", "token", "cert-manager-vault",
		"-n", "cert-manager", "--duration=8760h").Output()
	if cmErr != nil || strings.TrimSpace(string(cmTokenOut)) == "" {
		return fmt.Errorf("failed to generate cert-manager SA token: %v", cmErr)
	}

	_ = exec.Command("kubectl", "delete", "secret", "vault-k8s-token", "-n", "cert-manager", "--ignore-not-found").Run()
	if err := exec.Command("kubectl", "create", "secret", "generic", "vault-k8s-token",
		"--from-literal=token="+strings.TrimSpace(string(cmTokenOut)),
		"-n", "cert-manager").Run(); err != nil {
		return fmt.Errorf("failed to store K8s token secret: %w", err)
	}

	_, _ = client.Logical().Write("auth/"+authMount+"/role/cert-manager-role", map[string]interface{}{
		"bound_service_account_names":      "cert-manager-vault",
		"bound_service_account_namespaces": "cert-manager",
		"token_policies":                   []string{"hal-pki-issuer"},
		"token_ttl":                        "1h",
	})
	fmt.Printf("  ✅ Vault role 'cert-manager-role' created on '%s/'.\n", authMount)
	fmt.Println("  ✅ K8s secret 'vault-k8s-token' created in cert-manager namespace.")
	return nil
}

// buildPKIK8sCoreManifests returns Namespace, Deployment, and Service YAML.
// These are standard K8s resources — no cert-manager webhook validation.
func buildPKIK8sCoreManifests(intMount, webBackendImage string) string {
	return fmt.Sprintf(`---
apiVersion: v1
kind: Namespace
metadata:
  name: pki-demo
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hal-web-pki
  namespace: pki-demo
spec:
  replicas: 1
  selector:
    matchLabels:
      app: hal-web-pki
  template:
    metadata:
      labels:
        app: hal-web-pki
    spec:
      volumes:
        - name: tls
          secret:
            secretName: hal-web-pki-tls
      containers:
        - name: app
          image: %s
          ports:
            - containerPort: 443
          volumeMounts:
            - name: tls
              mountPath: /tls
              readOnly: true
          command: ["/bin/sh", "-c"]
          args:
            - |
              apk add --no-cache openssl >/dev/null 2>&1
              while [ ! -f /tls/tls.crt ]; do sleep 2; done
              TLS_CERT=$(cat /tls/tls.crt)
              CERT_TEXT=$(openssl x509 -noout -text -in /tls/tls.crt 2>&1)
              mkdir -p /usr/share/nginx/html
              cat > /etc/nginx/conf.d/default.conf <<'NGINXEOF'
              server {
                  listen 443 ssl;
                  ssl_certificate     /tls/tls.crt;
                  ssl_certificate_key /tls/tls.key;
                  root /usr/share/nginx/html;
                  index index.html;
              }
              NGINXEOF
              cat > /usr/share/nginx/html/index.html <<HTMLEOF
              <html>
                <head>
                  <meta charset='utf-8'>
                  <title>HAL Vault PKI</title>
                  <style>
                    body{font-family:system-ui;background:#f7fafc;color:#111827;padding:24px;max-width:960px;margin:0 auto;}
                    h1{margin-bottom:4px;}
                    h2{margin-top:28px;margin-bottom:6px;font-size:1rem;color:#374151;}
                    pre{background:#111827;color:#34d399;padding:14px;border-radius:8px;font-size:11px;overflow-x:auto;white-space:pre-wrap;word-break:break-all;}
                    pre.text{color:#a5f3fc;}
                  </style>
                </head>
                <body>
                  <h1>HAL Vault PKI + cert-manager</h1>
                  <p>TLS certificate issued by cert-manager via Vault <code>%s/sign/hal-role</code>.</p>
                  <h2>openssl x509 -noout -text</h2>
                  <pre class='text'>$CERT_TEXT</pre>
                  <h2>PEM (raw)</h2>
                  <pre>$TLS_CERT</pre>
                </body>
              </html>
              HTMLEOF
              exec nginx -g 'daemon off;'
---
apiVersion: v1
kind: Service
metadata:
  name: hal-web-pki
  namespace: pki-demo
spec:
  type: NodePort
  selector:
    app: hal-web-pki
  ports:
    - port: 443
      targetPort: 443
      nodePort: 30082
`, webBackendImage, intMount)
}

// buildPKIK8sCRDManifests returns ClusterIssuer and Certificate YAML.
// These are cert-manager CRDs validated by the admission webhook — apply only
// after the webhook TLS server is confirmed to be accepting connections.
func buildPKIK8sCRDManifests(vaultIP, intMount string) string {
	return fmt.Sprintf(`---
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: vault-pki-issuer
spec:
  vault:
    path: %s/sign/hal-role
    server: http://%s:8200
    auth:
      kubernetes:
        role: cert-manager-role
        mountPath: /v1/auth/kubernetes-pki
        secretRef:
          name: vault-k8s-token
          key: token
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: hal-web-pki-cert
  namespace: pki-demo
spec:
  secretName: hal-web-pki-tls
  duration: 24h
  renewBefore: 1h
  commonName: hal-web-pki.hal.local
  subject:
    organizations:
      - HAL Lab
  privateKey:
    algorithm: RSA
    encoding: PKCS1
    size: 2048
  usages:
    - server auth
    - client auth
  dnsNames:
    - hal-web-pki.hal.local
    - hal-web-pki.pki-demo.svc.cluster.local
  issuerRef:
    name: vault-pki-issuer
    kind: ClusterIssuer
    group: cert-manager.io
`, intMount, vaultIP)
}

// runPKIACMEEnable deploys a Caddy pod that uses Vault's built-in ACME endpoint
// to obtain its TLS certificate — no cert-manager, no Kubernetes CRDs.
// Caddy speaks the ACME protocol directly to Vault's pki-int/acme/directory.
func runPKIACMEEnable(client *vault.Client, engine string, isPodman bool, intMount string) {
	if err := tunePKIACMEHeaders(client, intMount); err != nil {
		fmt.Printf("❌ Failed to tune ACME headers on '%s': %v\n", intMount, err)
		return
	}

	// Verify ACME endpoint is live (config readable means ACME has been enabled).
	acmeResp, err := client.Logical().Read(intMount + "/config/acme")
	if err != nil || acmeResp == nil {
		fmt.Printf("❌ ACME config not readable on '%s'. Run 'hal vault pki enable' first.\n", intMount)
		return
	}

	// Always sync the acme-demo role TTL and ACME min-cert-TTL with the current
	// flag value so that 'update --acme --acme-cert-ttl X' takes effect without --force.
	_, _ = client.Logical().Write(intMount+"/roles/acme-demo", map[string]interface{}{
		"allowed_domains":     pkiAllowedDomains,
		"allow_subdomains":    true,
		"allow_bare_domains":  true,
		"allow_any_name":      true,
		"allow_ip_sans":       true,
		"use_csr_common_name": true,
		"ttl":                 pkiACMECertTTL,
		"max_ttl":             pkiACMECertTTL,
		"key_type":            "any",
		"key_bits":            0,
		"no_store":            false,
	})
	// Vault 2.x: config/acme max_ttl caps ALL certs issued via ACME on this mount.
	// Without this, Vault defaults to 2160h regardless of the role TTL.
	_, _ = client.Logical().Write(intMount+"/config/acme", map[string]interface{}{
		"enabled": true,
		"max_ttl": pkiACMECertTTL,
	})
	fmt.Printf("⚙️  Role 'acme-demo' TTL set to %s.\n", pkiACMECertTTL)

	// ---- KinD cluster (reuse if already running) ----
	clusterOut, _ := exec.Command("kind", "get", "clusters").Output()
	if strings.Contains(string(clusterOut), "kind") {
		fmt.Println("⚡ KinD cluster already running — reusing it.")
	} else {
		fmt.Println("🚀 Booting KinD cluster (attached to hal-net)...")
		kindConfigPath, err := writeHALKindConfig()
		if err != nil {
			fmt.Printf("❌ Failed to prepare KinD config: %v\n", err)
			return
		}
		defer os.Remove(kindConfigPath)

		startCmd := exec.Command("kind", "create", "cluster", "--config", kindConfigPath)
		if strings.TrimSpace(pkiKindNodeImage) != "" {
			startCmd.Args = append(startCmd.Args, "--image", pkiKindNodeImage)
		}
		env := os.Environ()
		if isPodman {
			env = append(env, "KIND_EXPERIMENTAL_PROVIDER=podman")
		}
		env = append(env, "KIND_EXPERIMENTAL_DOCKER_NETWORK=hal-net")
		startCmd.Env = env
		startCmd.Stdout = os.Stdout
		startCmd.Stderr = os.Stderr
		if err := startCmd.Run(); err != nil {
			fmt.Printf("❌ Failed to start KinD: %v\n", err)
			return
		}
	}

	// ---- Vault IP reachable from within the cluster ----
	vaultIPOut, _ := exec.Command(
		engine, "inspect",
		"-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}",
		"hal-vault",
	).Output()
	vaultIP := strings.TrimSpace(string(vaultIPOut))
	if vaultIP == "" {
		vaultIP = "hal-vault"
	}

	// Make Vault resolve acme.localhost to the KinD node IP for HTTP-01 callbacks.
	kindIPOut, _ := exec.Command(
		engine, "inspect",
		"-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}",
		"kind-control-plane",
	).Output()
	kindIP := strings.TrimSpace(string(kindIPOut))
	if kindIP != "" {
		// The Vault container's /etc/hosts is read-only in rootless Podman and the
		// .localhost TLD always resolves to 127.0.0.1 via the container DNS.
		// Spin up a CoreDNS sidecar on hal-net that serves acme.localhost → kindIP,
		// then point Vault's ACME validator at it via dns_resolver.
		fmt.Println("⚙️  Starting ACME DNS resolver (CoreDNS) on hal-net...")
		dnsIP, dnsErr := ensureACMEDNS(engine, kindIP)
		if dnsErr != nil {
			fmt.Printf("⚠️  Could not start ACME DNS: %v\n", dnsErr)
			fmt.Println("   HTTP-01 validation may fail if acme.localhost resolves to loopback inside Vault.")
		}

		// challenge_permitted_ip_ranges: tell Vault this private IP is a valid target.
		// dns_resolver: tell Vault to use our CoreDNS instead of the default container DNS
		// (which maps .localhost → 127.0.0.1, an address Vault can never reach externally).
		acmeCfg := map[string]interface{}{
			"enabled":                       true,
			"max_ttl":                       pkiACMECertTTL,
			"challenge_permitted_ip_ranges": []string{kindIP + "/32"},
		}
		if dnsIP != "" {
			acmeCfg["dns_resolver"] = dnsIP + ":53"
		}
		_, _ = client.Logical().Write(intMount+"/config/acme", acmeCfg)
		if dnsIP != "" {
			fmt.Printf("⚙️  Vault ACME: challenge_permitted_ip_ranges=[%s/32], dns_resolver=%s:53\n", kindIP, dnsIP)
		} else {
			fmt.Printf("⚙️  Vault ACME: challenge_permitted_ip_ranges=[%s/32]\n", kindIP)
		}
	}

	// ---- Auto-tidy: remove expired certs from PKI storage automatically ----
	// With short-TTL ACME certs (e.g. 5m) Vault accumulates a large number of expired
	// certificate entries. auto-tidy runs on an interval inside Vault itself — no CronJob needed.
	_, _ = client.Logical().Write(intMount+"/config/auto-tidy", map[string]interface{}{
		"enabled":            true,
		"interval_duration":  "2m",
		"safety_buffer":      "30s",
		"tidy_cert_store":    true,
		"tidy_revoked_certs": true,
	})
	fmt.Println("⚙️  Vault PKI auto-tidy: enabled (interval=2m, safety_buffer=30s)")

	// ---- Apply Caddy manifests ----
	fmt.Println("⚙️  Applying ACME/Caddy manifests (Namespace, ConfigMaps, Deployments, Services)...")
	manifests := buildPKIACMEManifests(vaultIP, intMount, pkiCaddyImage+":"+pkiCaddyTag)
	// Guard against accidental tab indentation in embedded YAML blocks.
	manifests = strings.ReplaceAll(manifests, "\t", "  ")
	applyCmd := exec.Command("kubectl", "apply", "-f", "-")
	applyCmd.Stdin = strings.NewReader(manifests)
	applyCmd.Stdout = os.Stdout
	applyCmd.Stderr = os.Stderr
	if err := applyCmd.Run(); err != nil {
		fmt.Printf("❌ Failed to apply ACME manifests: %v\n", err)
		return
	}

	// Force a pod restart so /data/caddy (emptyDir) is cleared.
	// Without this, kubectl apply is idempotent — the existing pod keeps its
	// cached cert (potentially with the old TTL) and Caddy won't renew until
	// ~1/3 of the original lifetime remains (could be hours for a 24h cert).
	fmt.Println("♻️  Restarting Caddy pod to clear cert cache (forces fresh ACME exchange)...")
	_ = exec.Command("kubectl", "rollout", "restart",
		"deployment/hal-caddy-acme", "-n", "pki-acme-demo").Run()

	fmt.Println("⏳ Waiting for Caddy rollout to complete (up to 90s)...")
	podWaitErr := exec.Command(
		"kubectl", "rollout", "status",
		"deployment/hal-caddy-acme", "-n", "pki-acme-demo", "--timeout=90s",
	).Run()
	if podWaitErr != nil {
		fmt.Println("❌ Caddy deployment did not roll out within 90s. Diagnosis:")
		logOut, _ := exec.Command(
			"kubectl", "logs", "-l", "app=hal-caddy-acme",
			"-n", "pki-acme-demo", "--tail=40",
		).Output()
		if len(logOut) > 0 {
			fmt.Println(string(logOut))
		}
		evtOut, _ := exec.Command(
			"kubectl", "get", "events", "-n", "pki-acme-demo", "--sort-by=.lastTimestamp",
		).Output()
		if len(evtOut) > 0 {
			fmt.Println(string(evtOut))
		}
		fmt.Println("\n💡 Run 'hal vault pki status' to recheck once the issue is resolved.")
		return
	}

	fmt.Println("\n✅ Vault PKI + ACME/Caddy demo deployed!")
	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  What was deployed:")
	fmt.Println("    - Caddy pod (namespace: pki-acme-demo) requesting certs via Vault ACME")
	fmt.Println("    - hostNetwork ACME gateway on Kind node :80 for Vault HTTP-01 callbacks")
	fmt.Printf("    - ACME directory: http://vault.localhost:8200/v1/%s/roles/acme-demo/acme/directory\n", intMount)
	fmt.Printf("    - Cert TTL: %s\n", pkiACMECertTTL)
	fmt.Printf("    - Caddy image: %s\n", pkiCaddyImage+":"+pkiCaddyTag)
	fmt.Println("\n  Access:")
	fmt.Println("    → https://acme.localhost:8090")
	fmt.Println("\n  Inspect the certificate:")
	fmt.Println("    kubectl exec -n pki-acme-demo deploy/hal-caddy-acme -c caddy -- ls /data/caddy/certificates/")
	fmt.Println("    kubectl logs -n pki-acme-demo deploy/hal-caddy-acme -c caddy --tail=100")
	fmt.Println("    kubectl logs -n pki-acme-demo deploy/hal-acme-gateway --tail=100")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}

func tunePKIACMEHeaders(client *vault.Client, mount string) error {
	_, err := client.Logical().Write("sys/mounts/"+mount+"/tune", map[string]interface{}{
		"passthrough_request_headers": []string{"If-Modified-Since"},
		"allowed_response_headers":    []string{"Last-Modified", "Location", "Replay-Nonce", "Link"},
	})
	return err
}

// ensureACMEDNS ensures a minimal CoreDNS container is running on hal-net that
// serves a static A record for acme.localhost → kindIP. Vault cannot write to
// its own /etc/hosts (read-only in rootless Podman), and the .localhost TLD
// always resolves to 127.0.0.1 via the container DNS — so we give Vault a
// custom dns_resolver that returns the real KinD node IP instead.
// Returns the DNS container IP (to be set as Vault ACME dns_resolver).
func ensureACMEDNS(engine, kindIP string) (string, error) {
	// Return early if already running with the correct mapping.
	runningOut, _ := exec.Command(engine, "inspect", "-f", "{{.State.Running}}", "hal-acme-dns").Output()
	if strings.TrimSpace(string(runningOut)) == "true" {
		ipOut, _ := exec.Command(engine, "inspect",
			"-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", "hal-acme-dns").Output()
		if ip := strings.TrimSpace(string(ipOut)); ip != "" {
			return ip, nil
		}
	}
	// Remove any stopped/stale container.
	_ = exec.Command(engine, "rm", "-f", "hal-acme-dns").Run()

	// Write a minimal Corefile to a temp directory.
	tmpDir, err := os.MkdirTemp("", "hal-acme-dns-*")
	if err != nil {
		return "", fmt.Errorf("temp dir: %w", err)
	}
	corefile := fmt.Sprintf(`. {
    hosts {
        %s acme.localhost
        fallthrough
    }
    forward . 8.8.8.8:53 8.8.4.4:53
    errors
    cache
}
`, kindIP)
	if err := os.WriteFile(filepath.Join(tmpDir, "Corefile"), []byte(corefile), 0644); err != nil {
		return "", fmt.Errorf("write Corefile: %w", err)
	}

	startOut, startErr := exec.Command(engine, "run", "-d",
		"--name", "hal-acme-dns",
		"--network", "hal-net",
		"-v", tmpDir+"/Corefile:/Corefile:ro",
		"coredns/coredns:latest", "-conf", "/Corefile",
	).CombinedOutput()
	if startErr != nil {
		return "", fmt.Errorf("start hal-acme-dns: %s", strings.TrimSpace(string(startOut)))
	}

	// Give CoreDNS a moment to bind.
	time.Sleep(2 * time.Second)

	ipOut, _ := exec.Command(engine, "inspect",
		"-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", "hal-acme-dns").Output()
	dnsIP := strings.TrimSpace(string(ipOut))
	if dnsIP == "" {
		return "", fmt.Errorf("hal-acme-dns started but could not determine its IP")
	}
	return dnsIP, nil
}

// buildPKIACMEManifests returns Namespace, ConfigMaps, Deployments, and Services for ACME demo.
// Host access stays unprivileged via NodePort 30083 -> host:8090, while Vault callback traffic
// is handled by a hostNetwork gateway that listens on Kind node port 80.
func buildPKIACMEManifests(vaultIP, intMount, caddyImage string) string {
	acmeDir := fmt.Sprintf("http://vault.localhost:8200/v1/%s/roles/acme-demo/acme/directory", intMount)
	return fmt.Sprintf(`---
apiVersion: v1
kind: Namespace
metadata:
  name: pki-acme-demo
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: caddy-config
  namespace: pki-acme-demo
data:
  Caddyfile: |
    {
      email lab@hal.local
			renew_interval 30s
    }
    acme.localhost {
      root * /srv
      file_server
      tls {
        issuer acme {
					dir %s
          trusted_roots /etc/caddy/vault-ca.pem
          disable_tlsalpn_challenge
        }
      }
    }
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hal-caddy-acme
  namespace: pki-acme-demo
spec:
  replicas: 1
  selector:
    matchLabels:
      app: hal-caddy-acme
  template:
    metadata:
      labels:
        app: hal-caddy-acme
    spec:
			hostAliases:
				- ip: %s
					hostnames:
						- vault.localhost
      initContainers:
        - name: fetch-ca
          image: curlimages/curl:latest
          command: ["/bin/sh", "-c"]
          args:
            - curl -sS http://%s:8200/v1/%s/ca/pem -o /shared/vault-ca.pem
          volumeMounts:
            - name: shared-ca
              mountPath: /shared
        - name: build-page
          image: alpine:latest
          command: ["/bin/sh", "-c"]
          args:
            - |
              mkdir -p /srv
              cat > /srv/index.html <<'HTMLEOF'
              <html>
                <head>
                  <meta charset='utf-8'>
                  <title>HAL Vault ACME + Caddy</title>
                  <style>
                    *{box-sizing:border-box;}
                    body{font-family:system-ui;background:#0f172a;color:#e2e8f0;padding:24px;max-width:960px;margin:0 auto;}
                    h1{margin-bottom:2px;color:#f8fafc;}
                    .subtitle{color:#64748b;font-size:.9rem;margin-bottom:16px;}
                    .subtitle a{color:#60a5fa;}
                    h2{margin-top:24px;margin-bottom:6px;font-size:.9rem;font-weight:600;color:#94a3b8;text-transform:uppercase;letter-spacing:.05em;}
                    .card{background:#1e293b;border-radius:10px;padding:16px;margin-bottom:12px;}
                    .countdown{font-size:2.4rem;font-weight:700;letter-spacing:.02em;color:#f8fafc;}
                    .countdown.warning{color:#fbbf24;}
                    .countdown.critical{color:#f87171;animation:pulse 1s infinite;}
                    @keyframes pulse{0%%,100%%{opacity:1;}50%%{opacity:.5;}}
                    .badge{display:inline-block;padding:2px 10px;border-radius:99px;font-size:.75rem;font-weight:600;margin-left:10px;vertical-align:middle;}
                    .badge.renewed{background:#065f46;color:#6ee7b7;}
                    .badge.live{background:#1e3a5f;color:#93c5fd;}
                    .meta{font-size:.8rem;color:#64748b;margin-top:6px;}
                    pre{background:#0f172a;padding:14px;border-radius:8px;font-size:11px;overflow-x:auto;white-space:pre-wrap;word-break:break-all;margin:0;}
                    pre.info{color:#a5f3fc;}
                    pre.pem{color:#34d399;}
                    .progress-bar{height:6px;background:#1e293b;border-radius:3px;overflow:hidden;margin-top:10px;}
                    .progress-fill{height:100%%;background:#3b82f6;transition:width .5s linear,background .5s;}
                  </style>
                  <script>
                    let lastSerial = null;
                    let notAfter = null;
                    let notBefore = null;
                    let renewalBadgeTimer = null;

										function toDate(value) {
											return value ? new Date(value) : null;
										}

                    function parseNotAfter(infoText) {
                      const m = infoText.match(/Not After\s*:\s*(.+)/i);
                      return m ? new Date(m[1].trim()) : null;
                    }
                    function parseNotBefore(infoText) {
                      const m = infoText.match(/Not Before\s*:\s*(.+)/i);
                      return m ? new Date(m[1].trim()) : null;
                    }
                    function parseSerial(infoText) {
                      const m = infoText.match(/Serial Number:\s*(?:\n\s*)?([^\n]+)/i);
                      return m ? m[1].trim() : null;
                    }

                    function updateCountdown() {
                      if (!notAfter) return;
                      const now = new Date();
                      const secsLeft = Math.max(0, Math.floor((notAfter - now) / 1000));
                      const total = notAfter - (notBefore || notAfter - 120000);
                      const elapsed = now - (notBefore || now);
                      const pct = Math.max(0, Math.min(100, 100 - (elapsed / total * 100)));

                      const m = Math.floor(secsLeft / 60);
                      const s = secsLeft %% 60;
                      const el = document.getElementById('countdown');
                      el.textContent = m + 'm ' + String(s).padStart(2,'0') + 's';
                      el.className = 'countdown' + (secsLeft < 30 ? ' critical' : secsLeft < 60 ? ' warning' : '');

                      const fill = document.getElementById('progress-fill');
                      fill.style.width = pct + '%%';
                      fill.style.background = secsLeft < 30 ? '#ef4444' : secsLeft < 60 ? '#f59e0b' : '#3b82f6';

                      document.getElementById('meta-expires').textContent =
                        'Expires: ' + notAfter.toLocaleTimeString() + '  ·  Issued: ' + (notBefore ? notBefore.toLocaleTimeString() : '?');
                    }

                    async function poll() {
                      try {
												const metaResp = await fetch('/cert-meta.json?t=' + Date.now());
												if (metaResp.ok) {
													const meta = await metaResp.json();
													const serial = meta.serial || null;
													notAfter = toDate(meta.not_after);
													notBefore = toDate(meta.not_before);

													if (serial && serial !== lastSerial) {
														if (lastSerial !== null) {
															const chip = document.getElementById('renewed-badge');
															chip.style.opacity = '1';
															clearTimeout(renewalBadgeTimer);
															const fadeMs = notAfter && notBefore
																? Math.max(5000, (notAfter - notBefore) * 0.1)
																: 30000;
															renewalBadgeTimer = setTimeout(() => { chip.style.opacity = '0'; }, fadeMs);
														}
														lastSerial = serial;
													}
													document.getElementById('serial').textContent = serial ? ('Serial: ' + serial) : 'Serial: unavailable';
												}

                        const r = await fetch('/cert-info.txt?t=' + Date.now());
                        if (!r.ok) return;
                        const text = await r.text();
                        document.getElementById('info').textContent = text;

												if (!notAfter) {
													notAfter = parseNotAfter(text);
												}
												if (!notBefore) {
													notBefore = parseNotBefore(text);
												}
												if (!lastSerial) {
													const serial = parseSerial(text);
													if (serial) {
														lastSerial = serial;
														document.getElementById('serial').textContent = 'Serial: ' + serial;
													}
                        }

                        const pr = await fetch('/cert-pem.txt?t=' + Date.now());
                        document.getElementById('pem').textContent = pr.ok ? await pr.text() : '';
                      } catch(_) {}
                      updateCountdown();
                    }

                    window.onload = function() {
                      poll();
                      setInterval(poll, 5000);
                      setInterval(updateCountdown, 500);
                    };
                  </script>
                </head>
                <body>
                  <h1>HAL Vault ACME + Caddy</h1>
									<p class='subtitle'>Certificate via <a href='%s'>Vault ACME (role: acme-demo)</a> · auto-renewed by Caddy</p>

                  <div class='card'>
                    <h2>Time until expiry <span class='badge live'>Live</span><span id='renewed-badge' class='badge renewed' style='opacity:0;transition:opacity 1.5s ease'>Renewed!</span></h2>
                    <div class='countdown' id='countdown'>--:--</div>
                    <div class='progress-bar'><div class='progress-fill' id='progress-fill' style='width:100%%'></div></div>
                    <div class='meta' id='meta-expires'></div>
                    <div class='meta' id='serial'></div>
                  </div>

                  <div class='card'><pre class='info' id='info'>Waiting for Caddy to complete ACME exchange...</pre></div>

                  <div class='card'><pre class='pem' id='pem'></pre></div>
                </body>
              </html>
              HTMLEOF
          volumeMounts:
            - name: web-root
              mountPath: /srv
      containers:
				- name: vault-localhost-proxy
					image: alpine/socat:latest
					command: ["/bin/sh", "-c"]
					args:
						- |
							exec socat TCP-LISTEN:8200,bind=127.0.0.1,reuseaddr,fork TCP:%s:8200
        - name: caddy
          image: %s
          command: ["/bin/sh", "-c"]
          args:
            - |
              apk add --no-cache openssl >/dev/null 2>&1
              caddy start --config /etc/caddy/Caddyfile
              while true; do
                certfile=$(find /data/caddy/certificates -name '*.crt' 2>/dev/null | grep -v '\.issuer' | head -1)
								if [ -n "$certfile" ]; then
                  openssl x509 -noout -text -in "$certfile" > /srv/cert-info.txt 2>&1 || true
                  cat "$certfile" > /srv/cert-pem.txt 2>&1 || true
									serial=$(openssl x509 -noout -serial -in "$certfile" 2>/dev/null | cut -d= -f2-)
									not_before=$(openssl x509 -noout -startdate -in "$certfile" 2>/dev/null | cut -d= -f2-)
									not_after=$(openssl x509 -noout -enddate -in "$certfile" 2>/dev/null | cut -d= -f2-)
									printf '{"serial":"%%s","not_before":"%%s","not_after":"%%s"}\n' "$serial" "$not_before" "$not_after" > /srv/cert-meta.json 2>/dev/null || true
                fi
                sleep 5
              done
          ports:
            - containerPort: 443
            - containerPort: 80
          volumeMounts:
            - name: caddy-config
              mountPath: /etc/caddy/Caddyfile
              subPath: Caddyfile
            - name: shared-ca
              mountPath: /etc/caddy/vault-ca.pem
              subPath: vault-ca.pem
            - name: caddy-data
              mountPath: /data
            - name: web-root
              mountPath: /srv
      volumes:
        - name: caddy-config
          configMap:
            name: caddy-config
        - name: shared-ca
          emptyDir: {}
        - name: caddy-data
          emptyDir: {}
        - name: web-root
          emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: hal-caddy-acme-internal
  namespace: pki-acme-demo
spec:
  type: ClusterIP
  selector:
    app: hal-caddy-acme
  ports:
    - name: http
      port: 80
      targetPort: 80
    - name: https
      port: 443
      targetPort: 443
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: acme-gateway-config
  namespace: pki-acme-demo
data:
  nginx.conf: |
    events {}
    http {
      server {
        listen 80;
        location / {
          proxy_pass http://hal-caddy-acme-internal.pki-acme-demo.svc.cluster.local:80;
          proxy_set_header Host $host;
          proxy_set_header X-Forwarded-Proto http;
          proxy_set_header X-Real-IP $remote_addr;
        }
      }
    }
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hal-acme-gateway
  namespace: pki-acme-demo
spec:
  replicas: 1
  selector:
    matchLabels:
      app: hal-acme-gateway
  template:
    metadata:
      labels:
        app: hal-acme-gateway
    spec:
      hostNetwork: true
      dnsPolicy: ClusterFirstWithHostNet
      containers:
        - name: gateway
          image: nginx:alpine
          ports:
            - containerPort: 80
          volumeMounts:
            - name: nginx-config
              mountPath: /etc/nginx/nginx.conf
              subPath: nginx.conf
      volumes:
        - name: nginx-config
          configMap:
            name: acme-gateway-config
---
apiVersion: v1
kind: Service
metadata:
  name: hal-caddy-acme
  namespace: pki-acme-demo
spec:
  type: NodePort
  selector:
    app: hal-caddy-acme
  ports:
    - port: 443
      targetPort: 443
      nodePort: 30083
`, acmeDir, vaultIP, vaultIP, intMount, acmeDir, vaultIP, caddyImage)
}

func init() {
	vaultPKICmd.Flags().BoolVarP(&pkiEnable, "enable", "e", false, "Enable PKI engines and generate Root CA + Intermediate CA")
	vaultPKICmd.Flags().BoolVarP(&pkiDisable, "disable", "d", false, "Disable PKI engines and remove all PKI resources")
	vaultPKICmd.Flags().BoolVarP(&pkiUpdate, "update", "u", false, "Reconcile PKI engines (recreate CAs)")
	_ = vaultPKICmd.Flags().MarkHidden("enable")
	_ = vaultPKICmd.Flags().MarkHidden("disable")
	_ = vaultPKICmd.Flags().MarkHidden("update")

	// PKI engine flags
	vaultPKICmd.Flags().StringVar(&pkiRootMount, "root-mount", "pki-root", "Vault mount path for the Root CA")
	vaultPKICmd.Flags().StringVar(&pkiIntMount, "int-mount", "pki-int", "Vault mount path for the Intermediate CA")
	vaultPKICmd.Flags().StringVar(&pkiRootTTL, "root-ttl", "43800h", "Max TTL for the Root CA (5 years)")
	vaultPKICmd.Flags().StringVar(&pkiIntTTL, "int-ttl", "17520h", "Max TTL for the Intermediate CA (2 years)")
	vaultPKICmd.Flags().StringVar(&pkiAllowedDomains, "allowed-domains", "hal.local,cluster.local,svc.cluster.local", "Allowed domains for 'hal-role'")
	vaultPKICmd.Flags().StringVar(&pkiMaxCertTTL, "max-cert-ttl", "24h", "Maximum TTL for leaf certificates issued by 'hal-role'")

	// K8s / cert-manager flags
	vaultPKICmd.Flags().BoolVar(&pkiK8s, "k8s", false, "Deploy cert-manager + nginx web demo on KinD (enable/update only)")
	vaultPKICmd.Flags().BoolVar(&pkiACME, "acme", false, "Deploy Vault ACME endpoint + Caddy demo on KinD (enable/update only)")
	vaultPKICmd.Flags().BoolVar(&pkiForce, "force", false, "With --k8s/--acme update: also rebuild Root CA and Intermediate CA from scratch")
	vaultPKICmd.Flags().StringVar(&pkiKindNodeImage, "kind-node-image", "kindest/node:v1.31.1", "KinD node image (used only when creating a new cluster)")
	vaultPKICmd.Flags().StringVar(&pkiCertManagerVersion, "cert-manager-version", "", "Jetstack cert-manager Helm chart version (empty = latest)")
	vaultPKICmd.Flags().StringVar(&pkiWebBackendImage, "vault-pki-web-backend-image", "nginx", "Demo backend container image name (cert-manager/--k8s demo)")
	vaultPKICmd.Flags().StringVar(&pkiWebBackendTag, "vault-pki-web-backend-tag", "alpine", "Demo backend container image tag (cert-manager/--k8s demo)")
	vaultPKICmd.Flags().StringVar(&pkiCaddyImage, "vault-pki-caddy-image", "caddy", "Caddy container image name (ACME/--acme demo)")
	vaultPKICmd.Flags().StringVar(&pkiCaddyTag, "vault-pki-caddy-tag", "alpine", "Caddy container image tag (ACME/--acme demo)")
	vaultPKICmd.Flags().StringVar(&pkiACMECertTTL, "acme-cert-ttl", "5m", "TTL for certs issued to Caddy via ACME (short = visible auto-renewal in the web page)")

	Cmd.AddCommand(vaultPKICmd)
}
