package pki

import (
	"fmt"
	"hal/internal/global"
	"os"
	"os/exec"
	"strings"
	"time"

	vault "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
)

var (
	pkiK8sEnable                bool
	pkiK8sDisable               bool
	pkiK8sUpdate                bool
	pkiK8sKindNodeImage         string
	pkiK8sCertManagerVersion    string
	pkiK8sWebBackendImage       string
	pkiK8sIntMount              string
)

var pkiK8sCmd = &cobra.Command{
	Use:   "k8s [status|enable|disable]",
	Short: "Deploy cert-manager + Vault PKI web demo on KinD",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := parsePKILifecycleAction(args, &pkiK8sEnable, &pkiK8sDisable, &pkiK8sUpdate); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		// Prereq: kind, kubectl, helm
		for _, bin := range []string{"kind", "kubectl", "helm"} {
			if _, err := exec.LookPath(bin); err != nil {
				fmt.Printf("❌ '%s' is not installed or not in PATH.\n", bin)
				return
			}
		}

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}
		isPodman := strings.Contains(engine, "podman")

		client, vaultErr := getVaultClient()

		// ==========================================
		// 1. SMART STATUS (default)
		// ==========================================
		if !pkiK8sEnable && !pkiK8sDisable && !pkiK8sUpdate {
			fmt.Println("🔍 Checking PKI Kubernetes Status...")

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
				fmt.Println("  ✅ KinD Cluster  : Active")
			} else {
				fmt.Println("  ❌ KinD Cluster  : Not running")
			}
			if certManagerInstalled {
				fmt.Println("  ✅ cert-manager  : Deployed")
			} else {
				fmt.Println("  ❌ cert-manager  : Not installed")
			}
			if issuerReady {
				fmt.Println("  ✅ ClusterIssuer : Ready")
			} else {
				fmt.Println("  ❌ ClusterIssuer : Not ready")
			}
			if certIssued {
				fmt.Println("  ✅ Certificate   : Issued")
			} else {
				fmt.Println("  ❌ Certificate   : Not issued")
			}
			if webPodRunning {
				fmt.Println("  ✅ Web Pod       : Running")
				fmt.Println("\n💡 Access:")
				fmt.Println("   kubectl port-forward -n pki-demo svc/hal-web-pki 8089:80")
				fmt.Println("   → http://localhost:8089")
			} else {
				fmt.Println("  ❌ Web Pod       : Not running")
				fmt.Println("\n💡 Next Step:")
				fmt.Println("   hal pki k8s enable")
			}
			return
		}

		// ==========================================
		// 2. TEARDOWN / DISABLE
		// ==========================================
		if pkiK8sDisable || pkiK8sUpdate {
			if global.DryRun {
				fmt.Println("[DRY RUN] Would delete namespace 'pki-demo'")
				fmt.Println("[DRY RUN] Would delete ClusterIssuer 'vault-pki-issuer'")
				fmt.Println("[DRY RUN] Would uninstall cert-manager Helm release")
			} else {
				fmt.Println("🛑 Tearing down PKI Kubernetes environment...")

				fmt.Println("⚙️  Deleting pki-demo namespace...")
				_ = exec.Command("kubectl", "delete", "namespace", "pki-demo", "--ignore-not-found").Run()

				fmt.Println("⚙️  Deleting ClusterIssuer...")
				_ = exec.Command("kubectl", "delete", "clusterissuer", "vault-pki-issuer", "--ignore-not-found").Run()

				fmt.Println("⚙️  Deleting vault-token secret from cert-manager...")
				_ = exec.Command("kubectl", "delete", "secret", "vault-token", "-n", "cert-manager", "--ignore-not-found").Run()

				fmt.Println("⚙️  Uninstalling cert-manager Helm release...")
				_ = exec.Command("helm", "uninstall", "cert-manager", "-n", "cert-manager").Run()

				fmt.Println("⚙️  Deleting cert-manager namespace...")
				_ = exec.Command("kubectl", "delete", "namespace", "cert-manager", "--ignore-not-found").Run()

				fmt.Println("✅ PKI Kubernetes environment removed.")
				fmt.Println("ℹ️  KinD cluster preserved. Run 'hal vault k8s disable' to remove it.")
			}

			if pkiK8sDisable && !global.DryRun {
				return
			}
		}

		// ==========================================
		// 3. ENABLE / UPDATE PATH
		// ==========================================
		if pkiK8sEnable || pkiK8sUpdate {
			if vaultErr != nil {
				fmt.Printf("❌ Vault must be running and healthy: %v\n", vaultErr)
				return
			}

			// Verify PKI is configured
			mounts, _ := client.Sys().ListMounts()
			intMount := pkiK8sIntMount
			if mounts == nil {
				fmt.Println("❌ Could not list Vault mounts.")
				return
			}
			if _, ok := mounts[intMount+"/"]; !ok {
				fmt.Printf("❌ Vault mount '%s' not found. Run 'hal pki create' first.\n", intMount)
				return
			}
			roleResp, err := client.Logical().Read(intMount + "/roles/hal-role")
			if err != nil || roleResp == nil {
				fmt.Printf("❌ Role 'hal-role' not found on '%s'. Run 'hal pki create' first.\n", intMount)
				return
			}

			if global.DryRun {
				fmt.Println("[DRY RUN] Would spin KinD cluster (if needed)")
				fmt.Println("[DRY RUN] Would deploy cert-manager via Helm")
				fmt.Println("[DRY RUN] Would configure ClusterIssuer pointing to Vault pki-int")
				fmt.Println("[DRY RUN] Would deploy web pod with Certificate CR in pki-demo namespace")
				return
			}

			// ---- KinD cluster ----
			clusterOut, _ := exec.Command("kind", "get", "clusters").Output()
			if strings.Contains(string(clusterOut), "kind") {
				fmt.Println("⚡ KinD cluster already running — reusing it.")
				fmt.Println("   ℹ️  Web pod will be accessible via kubectl port-forward (no direct host port mapping).")
			} else {
				fmt.Println("🚀 Booting KinD cluster (attached to hal-net)...")
				kindConfigPath, cfgErr := writePKIKindConfig()
				if cfgErr != nil {
					fmt.Printf("❌ Failed to prepare KinD config: %v\n", cfgErr)
					return
				}
				defer os.Remove(kindConfigPath)

				startCmd := exec.Command("kind", "create", "cluster", "--config", kindConfigPath)
				if strings.TrimSpace(pkiK8sKindNodeImage) != "" {
					startCmd.Args = append(startCmd.Args, "--image", pkiK8sKindNodeImage)
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

			// ---- cert-manager ----
			fmt.Println("⚙️  Deploying cert-manager via Helm...")
			_ = exec.Command("helm", "repo", "add", "jetstack", "https://charts.jetstack.io").Run()
			_ = exec.Command("helm", "repo", "update").Run()

			cmArgs := []string{
				"upgrade", "--install", "cert-manager", "jetstack/cert-manager",
				"-n", "cert-manager", "--create-namespace",
				"--set", "installCRDs=true",
			}
			if strings.TrimSpace(pkiK8sCertManagerVersion) != "" {
				cmArgs = append(cmArgs, "--version", pkiK8sCertManagerVersion)
			}
			cmCmd := exec.Command("helm", cmArgs...)
			cmCmd.Stdout = os.Stdout
			cmCmd.Stderr = os.Stderr
			if err := cmCmd.Run(); err != nil {
				fmt.Printf("❌ Failed to install cert-manager: %v\n", err)
				return
			}

			fmt.Println("⏳ Waiting for cert-manager webhook to become Ready (up to 120s)...")
			_ = exec.Command(
				"kubectl", "wait", "--for=condition=Ready",
				"pod", "-l", "app.kubernetes.io/name=webhook",
				"-n", "cert-manager", "--timeout=120s",
			).Run()
			// Extra buffer for webhook TLS to wire up
			time.Sleep(5 * time.Second)

			// ---- Vault token for cert-manager ----
			fmt.Println("⚙️  Creating Vault token for cert-manager...")
			// Ensure policy exists
			pkiPolicy := fmt.Sprintf(`
path "%s/sign/hal-role"  { capabilities = ["create", "update"] }
path "%s/issue/hal-role" { capabilities = ["create", "update"] }
`, intMount, intMount)
			_ = client.Sys().PutPolicy("hal-pki-issuer", pkiPolicy)

			tokenResp, err := client.Auth().Token().Create(&vault.TokenCreateRequest{
				Policies:  []string{"hal-pki-issuer"},
				TTL:       "8760h",
				Renewable: boolPtr(true),
			})
			if err != nil || tokenResp == nil {
				fmt.Printf("❌ Failed to create Vault token: %v\n", err)
				return
			}
			certManagerToken := tokenResp.Auth.ClientToken

			_ = exec.Command("kubectl", "delete", "secret", "vault-token", "-n", "cert-manager", "--ignore-not-found").Run()
			if err := exec.Command(
				"kubectl", "create", "secret", "generic", "vault-token",
				"--from-literal=token="+certManagerToken,
				"-n", "cert-manager",
			).Run(); err != nil {
				fmt.Printf("❌ Failed to store Vault token as K8s secret: %v\n", err)
				return
			}
			fmt.Println("  ✅ vault-token secret created in cert-manager namespace.")

			// ---- Vault IP on hal-net ----
			vaultIPOut, _ := exec.Command(
				engine, "inspect",
				"-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}",
				"hal-vault",
			).Output()
			vaultIP := strings.TrimSpace(string(vaultIPOut))
			if vaultIP == "" {
				vaultIP = "hal-vault"
			}

			// ---- Apply K8s manifests ----
			fmt.Println("⚙️  Applying ClusterIssuer + pki-demo manifests...")
			manifests := buildPKIManifests(vaultIP, intMount, pkiK8sWebBackendImage)
			applyCmd := exec.Command("kubectl", "apply", "-f", "-")
			applyCmd.Stdin = strings.NewReader(manifests)
			applyCmd.Stdout = os.Stdout
			applyCmd.Stderr = os.Stderr
			if err := applyCmd.Run(); err != nil {
				fmt.Printf("❌ Failed to apply manifests: %v\n", err)
				return
			}

			// ---- Wait for Certificate ----
			fmt.Println("⏳ Waiting for TLS Certificate to be issued (up to 120s)...")
			_ = exec.Command(
				"kubectl", "wait", "--for=condition=Ready",
				"certificate/hal-web-pki-cert",
				"-n", "pki-demo",
				"--timeout=120s",
			).Run()

			// ---- Wait for web pod ----
			fmt.Println("⏳ Waiting for web pod to be Ready (up to 120s)...")
			_ = exec.Command(
				"kubectl", "wait", "--for=condition=Ready",
				"pod", "-l", "app=hal-web-pki",
				"-n", "pki-demo",
				"--timeout=120s",
			).Run()

			fmt.Println("\n✅ PKI Kubernetes demo deployed!")
			fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println("  What was deployed:")
			fmt.Println("    - cert-manager (namespace: cert-manager)")
			fmt.Printf("    - ClusterIssuer vault-pki-issuer → Vault %s/sign/hal-role\n", intMount)
			fmt.Println("    - Certificate hal-web-pki-cert (namespace: pki-demo)")
			fmt.Println("    - Web pod hal-web-pki (httpd:2.4-alpine, TLS cert mounted at /tls)")
			fmt.Println("\n  Access the web pod:")
			fmt.Println("    kubectl port-forward -n pki-demo svc/hal-web-pki 8089:80")
			fmt.Println("    → http://localhost:8089   (shows the issued TLS certificate)")
			fmt.Println("\n  Inspect the certificate:")
			fmt.Println("    kubectl describe certificate hal-web-pki-cert -n pki-demo")
			fmt.Println("    kubectl get secret hal-web-pki-tls -n pki-demo -o jsonpath='{.data.tls\\.crt}' | base64 -d | openssl x509 -noout -text")
			fmt.Println("\n  Issue a standalone cert directly from Vault:")
			fmt.Printf("    vault write %s/issue/hal-role common_name=\"test.hal.local\" ttl=\"24h\"\n", intMount)
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		}
	},
}

// buildPKIManifests returns the complete Kubernetes YAML for the PKI demo.
func buildPKIManifests(vaultIP, intMount, webBackendImage string) string {
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
      tokenSecretRef:
        name: vault-token
        key: token
---
apiVersion: v1
kind: Namespace
metadata:
  name: pki-demo
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
            - containerPort: 80
          volumeMounts:
            - name: tls
              mountPath: /tls
              readOnly: true
          command: ["/bin/sh", "-c"]
          args:
            - |
              while [ ! -f /tls/tls.crt ]; do sleep 2; done
              TLS_CERT=$(cat /tls/tls.crt)
              cat > /usr/local/apache2/htdocs/index.html <<HTMLEOF
              <html>
                <body style='font-family:system-ui;background:#f7fafc;color:#111827;padding:24px;'>
                  <h1>HAL Vault PKI + cert-manager</h1>
                  <p>TLS certificate issued by cert-manager via Vault <code>%s/sign/hal-role</code>.</p>
                  <pre style='background:#111827;color:#34d399;padding:12px;border-radius:8px;font-size:11px;word-break:break-all;'>$TLS_CERT</pre>
                </body>
              </html>
              HTMLEOF
              exec httpd-foreground
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
    - port: 80
      targetPort: 80
      nodePort: 30082
`, intMount, vaultIP, webBackendImage, intMount)
}

// writePKIKindConfig writes a temporary KinD cluster config with port 30082→8089.
func writePKIKindConfig() (string, error) {
	f, err := os.CreateTemp("", "hal-pki-kind-*.yaml")
	if err != nil {
		return "", err
	}
	config := `kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraPortMappings:
    - containerPort: 30082
      hostPort: 8089
      protocol: TCP
`
	if _, err := f.WriteString(config); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func boolPtr(b bool) *bool { return &b }

func init() {
	pkiK8sCmd.Flags().BoolVarP(&pkiK8sEnable, "enable", "e", false, "Deploy cert-manager and PKI web demo")
	pkiK8sCmd.Flags().BoolVarP(&pkiK8sDisable, "disable", "d", false, "Remove cert-manager and pki-demo namespace")
	pkiK8sCmd.Flags().BoolVarP(&pkiK8sUpdate, "update", "u", false, "Recreate cert-manager and PKI demo")
	_ = pkiK8sCmd.Flags().MarkHidden("enable")
	_ = pkiK8sCmd.Flags().MarkHidden("disable")
	_ = pkiK8sCmd.Flags().MarkHidden("update")

	pkiK8sCmd.Flags().StringVar(&pkiK8sKindNodeImage, "kind-node-image", "kindest/node:v1.31.1", "KinD node image (used only when creating a new cluster)")
	pkiK8sCmd.Flags().StringVar(&pkiK8sCertManagerVersion, "cert-manager-version", "", "cert-manager Helm chart version (empty = latest)")
	pkiK8sCmd.Flags().StringVar(&pkiK8sWebBackendImage, "web-backend-image", "httpd:2.4-alpine", "Demo backend container image")
	pkiK8sCmd.Flags().StringVar(&pkiK8sIntMount, "int-mount", "pki-int", "Vault mount path for the Intermediate CA (must match hal pki create)")

	Cmd.AddCommand(pkiK8sCmd)
}
