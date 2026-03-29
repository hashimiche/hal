package vault

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"hal/internal/global"

	vault "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
)

var (
	k8sEnable  bool
	k8sDisable bool
	k8sForce   bool
	csiMode    bool
	jwtAuth    bool
)

func isVaultEnterprise(client *vault.Client) bool {
	health, err := client.Sys().Health()
	if err != nil {
		return false
	}
	return strings.Contains(health.Version, "ent")
}

var vaultK8sCmd = &cobra.Command{
	Use:   "k8s",
	Short: "Deploy KinD and Vault Secrets Operator (Native or CSI Mode)",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}
		isPodman := strings.Contains(engine, "podman")

		if _, err := exec.LookPath("kind"); err != nil {
			fmt.Println("❌ Error: 'kind' is not installed or not in PATH.")
			return
		}
		if _, err := exec.LookPath("kubectl"); err != nil {
			fmt.Println("❌ Error: 'kubectl' is not installed or not in PATH.")
			return
		}
		if _, err := exec.LookPath("helm"); err != nil {
			fmt.Println("❌ Error: 'helm' is not installed or not in PATH.")
			return
		}

		client, vaultErr := GetHealthyClient()

		// ==========================================
		// 1. SMART STATUS MODE (Default behavior)
		// ==========================================
		if !k8sEnable && !k8sDisable && !k8sForce {
			fmt.Println("🔍 Checking Vault / Kubernetes Status...")

			// Check KinD Cluster
			clusterCheck, _ := exec.Command("kind", "get", "clusters").Output()
			clusterRunning := strings.Contains(string(clusterCheck), "kind")

			// Check Vault Mounts (if Vault is alive)
			k8sMounted := false
			jwtMounted := false
			if vaultErr == nil {
				auths, _ := client.Sys().ListAuth()
				_, k8sMounted = auths["kubernetes/"]
				_, jwtMounted = auths["jwt-k8s/"]
			}

			// Check VSO Installation (if KinD is alive)
			vsoInstalled := false
			if clusterRunning {
				vsoCheck, _ := exec.Command("helm", "list", "-n", "vso", "-q").Output()
				vsoInstalled = strings.Contains(string(vsoCheck), "vault-secrets-operator")
			}

			// Output Status
			if clusterRunning {
				fmt.Printf("  ✅ KinD Cluster  : Active (Network: hal-net)\n")
			} else {
				fmt.Printf("  ❌ KinD Cluster  : Not running\n")
			}

			if vsoInstalled {
				fmt.Printf("  ✅ VSO Helm App  : Deployed in namespace 'vso'\n")
			} else {
				fmt.Printf("  ❌ VSO Helm App  : Not installed\n")
			}

			if k8sMounted || jwtMounted {
				authType := "Native"
				if jwtMounted {
					authType = "JWT (OIDC)"
				}
				fmt.Printf("  ✅ Vault Auth    : Configured (%s mode)\n", authType)
			} else {
				fmt.Printf("  ❌ Vault Auth    : Not configured\n")
			}

			// Smart Assistant Logic
			fmt.Println("\n💡 Next Step:")
			if !clusterRunning && !k8sMounted && !jwtMounted {
				fmt.Println("   To deploy KinD, VSO, and wire up Vault, run:")
				fmt.Println("   hal vault k8s --enable [--csi]")
			} else if clusterRunning && vsoInstalled && (k8sMounted || jwtMounted) {
				fmt.Println("   Demo is ready! Port-forward the web app:")
				fmt.Println("   Native Sync: kubectl port-forward deployment/hal-web-native -n app1 8080:80")
				fmt.Println("   CSI Driver : kubectl port-forward deployment/hal-web-csi -n app1 8080:80")
				fmt.Println("\n   To completely remove this cluster and clean Vault, run:")
				fmt.Println("   hal vault k8s --disable")
			} else {
				fmt.Println("   Environment is partially degraded. To safely reset, run:")
				fmt.Println("   hal vault k8s --force [--csi]")
			}
			return
		}

		// ==========================================
		// 2. TEARDOWN / RESET PATH (--disable / --force)
		// ==========================================
		if k8sDisable || k8sForce {
			if global.DryRun {
				fmt.Println("[DRY RUN] Would execute: kind delete cluster")
				fmt.Println("[DRY RUN] Would call API to clean up Vault entities and auth mounts")
			} else {
				if k8sDisable {
					fmt.Println("🛑 Tearing down Kubernetes environment...")
				} else {
					fmt.Println("♻️  Force flag detected. Destroying KinD cluster for reset...")
				}

				fmt.Println("⚙️  Connecting to Vault API for cleanup...")
				if vaultErr == nil && client != nil {
					fmt.Println("   🧹 Cleaning up Vault Identity Entities...")
					authMounts, err := client.Sys().ListAuth()
					if err == nil {
						authPath := "kubernetes/"
						if jwtAuth {
							authPath = "jwt-k8s/"
						}
						if mount, exists := authMounts[authPath]; exists {
							accessor := mount.Accessor
							entitiesList, err := client.Logical().List("identity/entity/id")
							if err == nil && entitiesList != nil && entitiesList.Data != nil {
								if keys, ok := entitiesList.Data["keys"].([]interface{}); ok {
									for _, key := range keys {
										entityID := key.(string)
										entityData, err := client.Logical().Read("identity/entity/id/" + entityID)
										if err == nil && entityData != nil && entityData.Data != nil {
											if aliases, ok := entityData.Data["aliases"].([]interface{}); ok {
												for _, aliasObj := range aliases {
													if alias, ok := aliasObj.(map[string]interface{}); ok {
														if alias["mount_accessor"] == accessor {
															fmt.Printf("      🗑️ Deleted ghost entity: %s\n", entityID)
															_, _ = client.Logical().Delete("identity/entity/id/" + entityID)
															break
														}
													}
												}
											}
										}
									}
								}
							}
						}
					}

					fmt.Println("   🧹 Unmounting KV and Auth Engines...")
					_ = client.Sys().Unmount("kv-k8s")
					if jwtAuth {
						_ = client.Sys().DisableAuth("jwt-k8s")
					} else {
						_ = client.Sys().DisableAuth("kubernetes")
					}
					_ = client.Sys().DeletePolicy("app1-read")
				} else {
					fmt.Println("⚠️  Vault is offline. Skipped Vault-internal cleanup.")
				}

				fmt.Println("⚙️  Destroying KinD Cluster...")
				_ = exec.Command("kind", "delete", "cluster").Run()

				fmt.Println("✅ Kubernetes environment destroyed successfully!")
			}

			if k8sDisable && !global.DryRun {
				return
			}
		}

		// ==========================================
		// 3. DEPLOY / ENABLE PATH (--enable / --force)
		// ==========================================
		if k8sEnable || k8sForce {
			if vaultErr != nil {
				fmt.Printf("❌ Cannot deploy: Vault must be running and healthy. %v\n", vaultErr)
				return
			}

			if global.DryRun {
				fmt.Println("[DRY RUN] Would execute: kind create cluster --network hal-net")
				fmt.Println("[DRY RUN] Would execute: helm install vault-secrets-operator")
				fmt.Println("[DRY RUN] Would call API to configure kubernetes auth and kv engine")
				return
			}

			clusterCheck, _ := exec.Command("kind", "get", "clusters").Output()
			if strings.Contains(string(clusterCheck), "kind") {
				fmt.Println("⚡ KinD cluster already running, skipping boot sequence...")
			} else {
				fmt.Println("🚀 Booting KinD Cluster (attached directly to hal-net)...")
				startCmd := exec.Command("kind", "create", "cluster")
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

			fmt.Println("⚙️  Ensuring Kubernetes Namespaces exist...")
			_ = exec.Command("kubectl", "create", "namespace", "vso").Run()
			_ = exec.Command("kubectl", "create", "namespace", "app1").Run()

			fmt.Println("⚙️  Configuring Vault KV Engine and Secrets...")
			_ = client.Sys().Unmount("kv-k8s")
			_ = client.Sys().Mount("kv-k8s", &vault.MountInput{
				Type:    "kv",
				Options: map[string]string{"version": "2"},
			})
			_, _ = client.Logical().Write("kv-k8s/data/app1", map[string]interface{}{
				"data": map[string]interface{}{"mysecret": "I'm sorry, Dave. I'm afraid I can't do that."},
			})
			policyDef := `
path "kv-k8s/data/app1" { capabilities = ["read"] }
path "sys/license/status" { capabilities = ["read"] }
`
			_ = client.Sys().PutPolicy("app1-read", policyDef)

			fmt.Println("⚙️  Extracting K8s API CA and generating TokenReviewer SA...")
			_ = exec.Command("kubectl", "create", "sa", "vault-reviewer", "-n", "default").Run()
			_ = exec.Command("kubectl", "create", "clusterrolebinding", "vault-reviewer-binding",
				"--clusterrole=system:auth-delegator",
				"--serviceaccount=default:vault-reviewer").Run()

			caOut, _ := exec.Command("kubectl", "config", "view", "--raw", "--minify", "--flatten", "-o", "jsonpath={.clusters[].cluster.certificate-authority-data}").Output()
			decodedCA, _ := base64.StdEncoding.DecodeString(string(caOut))
			caCert := string(decodedCA)

			tokenOut, _ := exec.Command("kubectl", "create", "token", "vault-reviewer", "-n", "default", "--duration=87600h").Output()
			reviewerToken := strings.TrimSpace(string(tokenOut))

			kindIPOut, _ := exec.Command(engine, "inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", "kind-control-plane").Output()
			kindIP := strings.TrimSpace(string(kindIPOut))
			if kindIP == "" {
				kindIP = "kind-control-plane"
			}

			fmt.Println("⚙️  Enabling Native Kubernetes Auth Engine...")
			_ = client.Sys().EnableAuthWithOptions("kubernetes", &vault.EnableAuthOptions{Type: "kubernetes"})

			_, err = client.Logical().Write("auth/kubernetes/config", map[string]interface{}{
				"kubernetes_host":        fmt.Sprintf("https://%s:6443", kindIP),
				"kubernetes_ca_cert":     caCert,
				"token_reviewer_jwt":     reviewerToken,
				"disable_iss_validation": true,
			})
			if err != nil {
				fmt.Printf("❌ Vault rejected the Kubernetes configuration: %v\n", err)
			}

			_, _ = client.Logical().Write("auth/kubernetes/role/app1-role", map[string]interface{}{
				"bound_service_account_names":      "app1-sa",
				"bound_service_account_namespaces": "app1",
				"bound_audiences":                  []string{"vault"},
				"token_policies":                   []string{"app1-read"},
				"token_ttl":                        "1h",
			})

			fmt.Println("⚙️  Deploying Vault Secrets Operator via Helm...")
			_ = exec.Command("helm", "repo", "add", "hashicorp", "https://helm.releases.hashicorp.com").Run()
			_ = exec.Command("helm", "repo", "update").Run()

			helmArgs := []string{"upgrade", "--install", "vault-secrets-operator", "hashicorp/vault-secrets-operator", "-n", "vso"}

			if csiMode {
				if isVaultEnterprise(client) {
					fmt.Println("🛡️  Vault Enterprise detected! Enabling CSI Driver...")
					helmArgs = append(helmArgs, "--set", "csi.enabled=true")
				} else {
					fmt.Println("⚠️  Warning: CSI Driver requested but Vault Enterprise not detected.")
					fmt.Println("⚠️  Downgrading to standard Native Sync deployment to ensure success.")
					csiMode = false
				}
			}

			if err := exec.Command("helm", helmArgs...).Run(); err != nil {
				fmt.Printf("❌ Failed to install VSO: %v\n", err)
			}

			fmt.Println("⏳ Waiting for K8s API to establish VSO CRDs (up to 60s)...")
			crds := []string{
				"crd/vaultconnections.secrets.hashicorp.com",
				"crd/vaultauths.secrets.hashicorp.com",
				"crd/vaultstaticsecrets.secrets.hashicorp.com",
			}
			if csiMode {
				crds = append(crds, "crd/csisecrets.secrets.hashicorp.com")
			}
			for _, crd := range crds {
				_ = exec.Command("kubectl", "wait", "--for=condition=Established", crd, "--timeout=60s").Run()
			}

			fmt.Println("⏳ Waiting for VSO Controller Pods to become Ready (up to 120s)...")
			_ = exec.Command("kubectl", "wait", "--for=condition=Ready", "pod", "-l", "app.kubernetes.io/name=vault-secrets-operator", "-n", "vso", "--timeout=120s").Run()

			fmt.Println("⏳ Giving Webhooks 5 seconds to wire up TLS...")
			time.Sleep(5 * time.Second)

			fmt.Println("⚙️  Applying Kubernetes Manifests...")
			_ = exec.Command("kubectl", "create", "sa", "app1-sa", "-n", "app1").Run()

			ipOut, _ := exec.Command(engine, "inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", "hal-vault").Output()
			vaultIP := strings.TrimSpace(string(ipOut))
			if vaultIP == "" {
				vaultIP = "hal-vault"
			}

			var appManifests string

			if csiMode {
				appManifests = fmt.Sprintf(`
---
apiVersion: secrets.hashicorp.com/v1beta1
kind: VaultConnection
metadata:
  name: default
  namespace: app1
spec:
  address: http://%s:8200
---
apiVersion: secrets.hashicorp.com/v1beta1
kind: VaultAuth
metadata:
  name: default
  namespace: app1
spec:
  method: kubernetes
  mount: kubernetes
  kubernetes:
    role: app1-role
    serviceAccount: app1-sa
    audiences: ["vault"]
  vaultConnectionRef: default
---
apiVersion: secrets.hashicorp.com/v1beta1
kind: CSISecrets
metadata:
  name: hal-csi-secrets
  namespace: app1
spec:
  vaultAuthRef:
    name: default
  secrets:
    vaultStaticSecrets:
      - mount: kv-k8s
        path: app1
        type: kv-v2
  accessControl:
    serviceAccountPattern: "app1-sa"
    namespacePatterns: ["app1"]
    podNamePatterns: ["^hal-web-csi.*"]
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hal-web-csi
  namespace: app1
spec:
  replicas: 1
  selector:
    matchLabels:
      app: hal-web-csi
  template:
    metadata:
      labels:
        app: hal-web-csi
    spec:
      serviceAccountName: app1-sa
      volumes:
        - name: secrets
          csi:
            driver: csi.vso.hashicorp.com
            volumeAttributes:
              csiSecretsName: hal-csi-secrets
              csiSecretsNamespace: app1
      containers:
        - name: web
          image: nginx:alpine
          ports:
            - containerPort: 80
          volumeMounts:
            - name: secrets
              mountPath: /mnt/secrets
              readOnly: true
          command: ["/bin/sh", "-c"]
          args:
            - |
              # Wait for the exact key file to be projected by the CSI driver
              while [ ! -f /mnt/secrets/static_secret_0_mysecret ]; do sleep 1; done
              
              # Read the raw string directly
              HAL_SECRET=$(cat /mnt/secrets/static_secret_0_mysecret)
              
              echo "<html><body style='background-color:#0d1a26;color:#00ffff;font-family:monospace;text-align:center;padding-top:20%%;'>
              <h1>HAL 9000 Vault Systems</h1>
              <h2>[CSI] Ephemeral Mount Successful!</h2>
              <p style='color:#aaaaaa;'>Secret loaded securely from memory. Zero footprint in etcd.</p>
              <p style='font-size:24px;color:#ff3333;'>$HAL_SECRET</p>
              </body></html>" > /usr/share/nginx/html/index.html;
              
              nginx -g 'daemon off;'
`, vaultIP)
			} else {
				appManifests = fmt.Sprintf(`
---
apiVersion: secrets.hashicorp.com/v1beta1
kind: VaultConnection
metadata:
  name: default
  namespace: app1
spec:
  address: http://%s:8200
---
apiVersion: secrets.hashicorp.com/v1beta1
kind: VaultAuth
metadata:
  name: default
  namespace: app1
spec:
  method: kubernetes
  mount: kubernetes
  kubernetes:
    role: app1-role
    serviceAccount: app1-sa
    audiences: ["vault"]
  vaultConnectionRef: default
---
apiVersion: secrets.hashicorp.com/v1beta1
kind: VaultStaticSecret
metadata:
  name: vso-mysecret
  namespace: app1
spec:
  type: kv-v2
  mount: kv-k8s
  path: app1
  destination:
    name: k8s-native-secret
    create: true
  rolloutRestartTargets:
    - kind: Deployment
      name: hal-web-native
  vaultAuthRef: default
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hal-web-native
  namespace: app1
spec:
  replicas: 1
  selector:
    matchLabels:
      app: hal-web-native
  template:
    metadata:
      labels:
        app: hal-web-native
    spec:
      serviceAccountName: app1-sa
      containers:
        - name: web
          image: nginx:alpine
          ports:
            - containerPort: 80
          env:
            - name: HAL_SECRET
              valueFrom:
                secretKeyRef:
                  name: k8s-native-secret
                  key: mysecret
          command: ["/bin/sh", "-c"]
          args:
            - |
              echo "<html><body style='background-color:#1a1a1a;color:#00ff00;font-family:monospace;text-align:center;padding-top:20%%;'>
              <h1>HAL 9000 Vault Systems</h1>
              <h2>[NATIVE] K8s Sync (Auto-Reload Active)</h2>
              <p style='color:#aaaaaa;'>Secret injected directly into standard Kubernetes etcd via VSO.</p>
              <p style='font-size:24px;color:#ff3333;'>$HAL_SECRET</p>
              </body></html>" > /usr/share/nginx/html/index.html;
              nginx -g 'daemon off;'
`, vaultIP)
			}

			applyK8s(appManifests)

			fmt.Println("\n✅ Kubernetes Secret Zero Environment Ready!")
			fmt.Println("---------------------------------------------------------")
			if csiMode {
				fmt.Println("🛡️  [CSI DRIVER DEMO]")
				fmt.Println("   kubectl port-forward deployment/hal-web-csi -n app1 8080:80")
			} else {
				fmt.Println("🌐 [NATIVE SYNC DEMO]")
				fmt.Println("   kubectl port-forward deployment/hal-web-native -n app1 8080:80")
			}
			fmt.Println("\n   Then open your browser to: http://localhost:8080")
			fmt.Println("---------------------------------------------------------")
		}
	},
}

// -----------------------------------------------------------------------------
// Helper Functions
// -----------------------------------------------------------------------------

func applyK8s(yamlContent string) {
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(yamlContent)
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("❌ Failed to apply K8s manifests: %v\nOutput: %s\n", err, string(out))
	} else {
		fmt.Println("✅ Successfully applied Kubernetes manifests.")
	}
}

func init() {
	// Standard Lifecycle Flags
	vaultK8sCmd.Flags().BoolVarP(&k8sEnable, "enable", "e", false, "Deploy KinD and configure Vault Secrets Operator")
	vaultK8sCmd.Flags().BoolVarP(&k8sDisable, "disable", "d", false, "Destroy KinD and clean up Vault configurations")
	vaultK8sCmd.Flags().BoolVarP(&k8sForce, "force", "f", false, "Force a clean redeployment of the cluster")

	// Feature-Specific Flags
	vaultK8sCmd.Flags().BoolVar(&csiMode, "csi", false, "Use the VSO CSI Driver (Requires Vault Enterprise)")
	vaultK8sCmd.Flags().BoolVar(&jwtAuth, "jwt", false, "Use the advanced jwt-k8s OIDC architecture (experimental)")

	Cmd.AddCommand(vaultK8sCmd)
}
