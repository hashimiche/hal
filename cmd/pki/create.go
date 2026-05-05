package pki

import (
	"fmt"
	"hal/internal/global"

	vault "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
)

var (
	pkiRootMount      string
	pkiIntMount       string
	pkiRootTTL        string
	pkiIntTTL         string
	pkiAllowedDomains string
	pkiMaxCertTTL     string
)

var pkiCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Enable Vault PKI engines, generate Root CA and signed Intermediate CA",
	Run: func(cmd *cobra.Command, args []string) {
		runPKISetup(false)
	},
}

var pkiUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Reconcile Vault PKI engines (unmount existing CAs and recreate)",
	Run: func(cmd *cobra.Command, args []string) {
		runPKISetup(true)
	},
}

func runPKISetup(isUpdate bool) {
	if global.DryRun {
		fmt.Printf("[DRY RUN] Would enable pki at '%s' and '%s'\n", pkiRootMount, pkiIntMount)
		fmt.Printf("[DRY RUN] Would generate Root CA (TTL: %s) and Intermediate CA (TTL: %s)\n", pkiRootTTL, pkiIntTTL)
		fmt.Printf("[DRY RUN] Would create role 'hal-role' (allowed: %s, max TTL: %s)\n", pkiAllowedDomains, pkiMaxCertTTL)
		return
	}

	client, err := getVaultClient()
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}

	if isUpdate {
		fmt.Printf("♻️  Reconciling PKI — unmounting '%s' and '%s'...\n", pkiRootMount, pkiIntMount)
		_ = client.Sys().Unmount(pkiRootMount)
		_ = client.Sys().Unmount(pkiIntMount)
	}

	// ==========================================
	// 1. ROOT CA
	// ==========================================
	fmt.Printf("🔐 Enabling PKI engine at '%s' (max TTL: %s)...\n", pkiRootMount, pkiRootTTL)
	if err := client.Sys().Mount(pkiRootMount, &vault.MountInput{
		Type:   "pki",
		Config: vault.MountConfigInput{MaxLeaseTTL: pkiRootTTL},
	}); err != nil {
		fmt.Printf("  ⚠️  Mount error (may already exist): %v — tuning TTL and continuing...\n", err)
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
		"issuing_certificates":    "http://127.0.0.1:8200/v1/" + pkiRootMount + "/ca",
		"crl_distribution_points": "http://127.0.0.1:8200/v1/" + pkiRootMount + "/crl",
	})

	// ==========================================
	// 2. INTERMEDIATE CA
	// ==========================================
	fmt.Printf("🔐 Enabling PKI engine at '%s' (max TTL: %s)...\n", pkiIntMount, pkiIntTTL)
	if err := client.Sys().Mount(pkiIntMount, &vault.MountInput{
		Type:   "pki",
		Config: vault.MountConfigInput{MaxLeaseTTL: pkiIntTTL},
	}); err != nil {
		fmt.Printf("  ⚠️  Mount error (may already exist): %v — tuning TTL and continuing...\n", err)
		_ = client.Sys().TuneMount(pkiIntMount, vault.MountConfigInput{MaxLeaseTTL: pkiIntTTL})
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
	csr, ok := csrResp.Data["csr"].(string)
	if !ok || csr == "" {
		fmt.Println("❌ CSR not returned from Vault.")
		return
	}
	fmt.Println("  ✅ CSR generated.")

	fmt.Println("✍️  Signing Intermediate CA CSR with Root CA...")
	signResp, err := client.Logical().Write(pkiRootMount+"/root/sign-intermediate", map[string]interface{}{
		"csr":    csr,
		"format": "pem_bundle",
		"ttl":    pkiIntTTL,
	})
	if err != nil || signResp == nil {
		fmt.Printf("❌ Failed to sign Intermediate CA: %v\n", err)
		return
	}
	signedCert, ok := signResp.Data["certificate"].(string)
	if !ok || signedCert == "" {
		fmt.Println("❌ Signed certificate not returned from Vault.")
		return
	}
	fmt.Println("  ✅ Intermediate CA signed by Root CA.")

	fmt.Println("📥 Installing signed certificate on Intermediate CA...")
	if _, err := client.Logical().Write(pkiIntMount+"/intermediate/set-signed", map[string]interface{}{
		"certificate": signedCert,
	}); err != nil {
		fmt.Printf("❌ Failed to set signed certificate: %v\n", err)
		return
	}
	fmt.Println("  ✅ Intermediate CA certificate installed.")

	_, _ = client.Logical().Write(pkiIntMount+"/config/urls", map[string]interface{}{
		"issuing_certificates":    "http://127.0.0.1:8200/v1/" + pkiIntMount + "/ca",
		"crl_distribution_points": "http://127.0.0.1:8200/v1/" + pkiIntMount + "/crl",
	})

	// ==========================================
	// 3. ISSUANCE ROLE
	// ==========================================
	fmt.Printf("⚙️  Creating role 'hal-role' on '%s'...\n", pkiIntMount)
	_, _ = client.Logical().Write(pkiIntMount+"/roles/hal-role", map[string]interface{}{
		"allowed_domains":    pkiAllowedDomains,
		"allow_subdomains":   true,
		"allow_bare_domains": false,
		"allow_ip_sans":      true,
		"max_ttl":            pkiMaxCertTTL,
		"key_type":           "rsa",
		"key_bits":           2048,
	})
	fmt.Println("  ✅ Role 'hal-role' created.")

	// ==========================================
	// 4. POLICY FOR CERT-MANAGER
	// ==========================================
	pkiPolicy := fmt.Sprintf(`
path "%s/sign/hal-role"  { capabilities = ["create", "update"] }
path "%s/issue/hal-role" { capabilities = ["create", "update"] }
`, pkiIntMount, pkiIntMount)
	_ = client.Sys().PutPolicy("hal-pki-issuer", pkiPolicy)
	fmt.Println("  ✅ Policy 'hal-pki-issuer' written.")

	// ==========================================
	// SUMMARY
	// ==========================================
	fmt.Println("\n✅ PKI setup complete!")
	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  Engine Layout:")
	fmt.Printf("    Root CA  : %s  (max TTL %s)\n", pkiRootMount, pkiRootTTL)
	fmt.Printf("    Int  CA  : %s   (max TTL %s)\n", pkiIntMount, pkiIntTTL)
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
	fmt.Println("\n💡 Next Step:")
	fmt.Println("   hal pki k8s enable   → deploy cert-manager + web pod demo with Vault-issued cert")
}

func bindPKICreateFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&pkiRootMount, "root-mount", "pki-root", "Vault mount path for the Root CA")
	cmd.Flags().StringVar(&pkiIntMount, "int-mount", "pki-int", "Vault mount path for the Intermediate CA")
	cmd.Flags().StringVar(&pkiRootTTL, "root-ttl", "87600h", "Max lease TTL for the Root CA (10 years)")
	cmd.Flags().StringVar(&pkiIntTTL, "int-ttl", "43800h", "Max lease TTL for the Intermediate CA (5 years)")
	cmd.Flags().StringVar(&pkiAllowedDomains, "allowed-domains", "hal.local,cluster.local,svc.cluster.local", "Comma-separated list of allowed domains for 'hal-role'")
	cmd.Flags().StringVar(&pkiMaxCertTTL, "max-cert-ttl", "24h", "Maximum TTL for leaf certificates issued by 'hal-role'")
}

func init() {
	bindPKICreateFlags(pkiCreateCmd)
	bindPKICreateFlags(pkiUpdateCmd)
	Cmd.AddCommand(pkiCreateCmd)
	Cmd.AddCommand(pkiUpdateCmd)
}
