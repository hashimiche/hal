package pki

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var pkiStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the status of Vault PKI engines, cert-manager, and the K8s demo",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("🔍 Checking Vault PKI Status...")

		client, err := getVaultClient()
		if err != nil {
			fmt.Printf("  ❌ Vault         : Unreachable (%v)\n", err)
			fmt.Println("\n💡 Next Step:")
			fmt.Println("   hal vault create")
			return
		}

		// ==========================================
		// 1. VAULT PKI ENGINES
		// ==========================================
		fmt.Println("  [ Vault PKI Engines ]")

		mounts, _ := client.Sys().ListMounts()
		rootMounted := false
		intMounted := false
		if mounts != nil {
			_, rootMounted = mounts["pki-root/"]
			_, intMounted = mounts["pki-int/"]
		}

		if rootMounted {
			fmt.Println("  ✅ pki-root      : Mounted")
			caResp, cerr := client.Logical().Read("pki-root/cert/ca")
			if cerr == nil && caResp != nil && caResp.Data["certificate"] != "" {
				fmt.Println("  ✅ Root CA       : Generated")
			} else {
				fmt.Println("  ⚠️  Root CA       : Not yet generated")
			}
		} else {
			fmt.Println("  ❌ pki-root      : Not mounted")
		}

		if intMounted {
			fmt.Println("  ✅ pki-int       : Mounted")
			caResp, cerr := client.Logical().Read("pki-int/cert/ca")
			if cerr == nil && caResp != nil && caResp.Data["certificate"] != "" {
				fmt.Println("  ✅ Intermediate  : Signed and installed")
			} else {
				fmt.Println("  ⚠️  Intermediate  : CSR not yet signed")
			}
			roleResp, rerr := client.Logical().Read("pki-int/roles/hal-role")
			if rerr == nil && roleResp != nil {
				fmt.Println("  ✅ Role hal-role : Configured")
			} else {
				fmt.Println("  ⚠️  Role hal-role : Not found")
			}
		} else {
			fmt.Println("  ❌ pki-int       : Not mounted")
		}

		// ==========================================
		// 2. KUBERNETES LAYER
		// ==========================================
		fmt.Println("\n  [ Kubernetes / cert-manager ]")

		clusterOut, _ := exec.Command("kind", "get", "clusters").Output()
		clusterRunning := strings.Contains(string(clusterOut), "kind")

		certManagerInstalled := false
		issuerReady := false
		certIssued := false
		webPodRunning := false

		if clusterRunning {
			cmOut, _ := exec.Command("helm", "list", "-n", "cert-manager", "-q").Output()
			certManagerInstalled = strings.Contains(string(cmOut), "cert-manager")

			if certManagerInstalled {
				issuerOut, _ := exec.Command(
					"kubectl", "get", "clusterissuer", "vault-pki-issuer",
					"-o", "jsonpath={.status.conditions[0].type}",
				).Output()
				issuerReady = strings.TrimSpace(string(issuerOut)) == "Ready"

				certOut, _ := exec.Command(
					"kubectl", "get", "certificate", "hal-web-pki-cert",
					"-n", "pki-demo",
					"-o", "jsonpath={.status.conditions[0].type}",
				).Output()
				certIssued = strings.TrimSpace(string(certOut)) == "Ready"

				podOut, _ := exec.Command(
					"kubectl", "get", "pods", "-n", "pki-demo",
					"-l", "app=hal-web-pki",
					"-o", "jsonpath={.items[0].status.phase}",
				).Output()
				webPodRunning = strings.TrimSpace(string(podOut)) == "Running"
			}
		}

		if clusterRunning {
			fmt.Println("  ✅ KinD Cluster      : Active")
		} else {
			fmt.Println("  ❌ KinD Cluster      : Not running")
		}

		if certManagerInstalled {
			fmt.Println("  ✅ cert-manager      : Deployed")
		} else {
			fmt.Println("  ❌ cert-manager      : Not installed")
		}

		if issuerReady {
			fmt.Println("  ✅ ClusterIssuer     : Ready (vault-pki-issuer)")
		} else if certManagerInstalled {
			fmt.Println("  ⚠️  ClusterIssuer     : Not ready or not created")
		} else {
			fmt.Println("  ❌ ClusterIssuer     : Not configured")
		}

		if certIssued {
			fmt.Println("  ✅ TLS Certificate   : Issued (hal-web-pki-cert)")
		} else if certManagerInstalled {
			fmt.Println("  ⚠️  TLS Certificate   : Not yet issued")
		} else {
			fmt.Println("  ❌ TLS Certificate   : Not configured")
		}

		if webPodRunning {
			fmt.Println("  ✅ Web Pod           : Running (pki-demo/hal-web-pki)")
			fmt.Println("\n  Access (port-forward):")
			fmt.Println("    kubectl port-forward -n pki-demo svc/hal-web-pki 8089:80")
			fmt.Println("    → http://localhost:8089")
		} else if certManagerInstalled {
			fmt.Println("  ⚠️  Web Pod           : Not running")
		} else {
			fmt.Println("  ❌ Web Pod           : Not deployed")
		}

		// ==========================================
		// NEXT STEP
		// ==========================================
		fmt.Println("\n💡 Next Step:")
		if !rootMounted || !intMounted {
			fmt.Println("   hal pki create")
		} else if !clusterRunning || !certManagerInstalled {
			fmt.Println("   hal pki k8s enable   → deploy cert-manager + web demo")
		} else if webPodRunning {
			fmt.Println("   kubectl port-forward -n pki-demo svc/hal-web-pki 8089:80")
			fmt.Println("   vault write pki-int/issue/hal-role common_name=\"test.hal.local\" ttl=\"24h\"")
		} else {
			fmt.Println("   hal pki k8s enable   → finish deploying the demo environment")
		}
	},
}

func init() {
	Cmd.AddCommand(pkiStatusCmd)
}
