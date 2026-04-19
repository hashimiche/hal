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
	k8sEnable       bool
	k8sDisable      bool
	k8sUpdate       bool
	csiMode         bool
	jwtAuth         bool
	kindNodeImage   string
	vsoChartVersion string
	webBackendImage string
	webProxyImage   string
)

func isVaultEnterprise(client *vault.Client) bool {
	health, err := client.Sys().Health()
	if err != nil {
		return false
	}
	return strings.Contains(health.Version, "ent")
}

var vaultK8sCmd = &cobra.Command{
	Use:   "k8s [status|enable|disable|update]",
	Short: "Deploy KinD and Vault Secrets Operator (Native or CSI Mode)",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := parseLifecycleAction(args, &k8sEnable, &k8sDisable, &k8sUpdate); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

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
		if !k8sEnable && !k8sDisable && !k8sUpdate {
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

			proxyServiceReady := false
			if clusterRunning {
				proxyCheck, _ := exec.Command("kubectl", "get", "svc", "hal-web-proxy", "-n", "app1", "-o", "name").Output()
				proxyServiceReady = strings.TrimSpace(string(proxyCheck)) == "service/hal-web-proxy"
			}

			demoMode := "unknown"
			if clusterRunning {
				demoMode = detectK8sDemoMode()
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

			if proxyServiceReady {
				fmt.Printf("  ✅ Demo Endpoint : http://web.localhost:8088\n")
			} else {
				fmt.Printf("  ❌ Demo Endpoint : Not exposed yet\n")
			}

			switch demoMode {
			case "native":
				fmt.Printf("  ✅ Demo Mode     : Native (VaultStaticSecret -> env var)\n")
			case "csi":
				fmt.Printf("  ✅ Demo Mode     : CSI (csi.vso.hashicorp.com projection)\n")
			case "none":
				fmt.Printf("  ❌ Demo Mode     : Not deployed\n")
			default:
				fmt.Printf("  ⚠️  Demo Mode     : Unknown/Partial\n")
			}

			// Smart Assistant Logic
			fmt.Println("\n💡 Next Step:")
			if !clusterRunning && !k8sMounted && !jwtMounted {
				fmt.Println("   To deploy KinD, VSO, and wire up Vault, run:")
				fmt.Println("   hal vault k8s enable [--csi]")
			} else if clusterRunning && vsoInstalled && (k8sMounted || jwtMounted) && proxyServiceReady {
				fmt.Println("   Demo is ready at: http://web.localhost:8088")
				fmt.Println("   No kubectl port-forward needed.")
				fmt.Println("\n   To completely remove this cluster and clean Vault, run:")
				fmt.Println("   hal vault k8s disable")
			} else {
				fmt.Println("   Environment is partially degraded. To safely reset, run:")
				fmt.Println("   hal vault k8s update [--csi]")
				fmt.Println("   Then run: hal vault k8s enable")
			}
			return
		}

		// ==========================================
		// 2. TEARDOWN / RESET PATH (--disable / --update)
		// ==========================================
		if k8sDisable || k8sUpdate {
			if global.DryRun {
				fmt.Println("[DRY RUN] Would execute: kind delete cluster")
				fmt.Println("[DRY RUN] Would call API to clean up Vault entities and auth mounts")
			} else {
				if k8sDisable {
					fmt.Println("🛑 Tearing down Kubernetes environment...")
				} else {
					fmt.Println("♻️  Update requested. Destroying KinD cluster for reconciliation...")
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
		// 3. DEPLOY / ENABLE PATH (--enable / --update)
		// ==========================================
		if k8sEnable || k8sUpdate {
			if vaultErr != nil {
				fmt.Printf("❌ Cannot deploy: Vault must be running and healthy. %v\n", vaultErr)
				return
			}

			global.WarnIfEngineResourcesTight(engine, "vault-k8s")
			if !global.DryRun {
				proceed, err := global.ConfirmScenarioProceed(engine, "vault-k8s")
				if err != nil && global.Debug {
					fmt.Printf("[DEBUG] Capacity confirmation unavailable: %v\n", err)
				}
				if err == nil && !proceed {
					fmt.Printf("🛑 Vault K8s deployment aborted to protect your %s engine.\n", engine)
					return
				}
			}

			if global.DryRun {
				fmt.Println("[DRY RUN] Would execute: kind create cluster --network hal-net (host 8088 -> cluster 30080)")
				fmt.Println("[DRY RUN] Would execute: helm install vault-secrets-operator")
				fmt.Println("[DRY RUN] Would call API to configure kubernetes auth and kv engine")
				return
			}

			clusterCheck, _ := exec.Command("kind", "get", "clusters").Output()
			if strings.Contains(string(clusterCheck), "kind") {
				fmt.Println("⚡ KinD cluster already running, skipping boot sequence...")
				fmt.Println("   ℹ️  Existing clusters may not expose host port 8088. Use --update once to recreate with HAL ingress mapping.")
			} else {
				fmt.Println("🚀 Booting KinD Cluster (attached directly to hal-net)...")
				kindConfigPath, cfgErr := writeKindConfigWithIngress()
				if cfgErr != nil {
					fmt.Printf("❌ Failed to prepare KinD config: %v\n", cfgErr)
					return
				}
				defer os.Remove(kindConfigPath)

				startCmd := exec.Command("kind", "create", "cluster", "--config", kindConfigPath)
				if strings.TrimSpace(kindNodeImage) != "" {
					startCmd.Args = append(startCmd.Args, "--image", kindNodeImage)
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

			fmt.Println("⚙️  Ensuring Kubernetes Namespaces exist...")
			_ = exec.Command("kubectl", "create", "namespace", "vso").Run()
			_ = exec.Command("kubectl", "create", "namespace", "app1").Run()

			fmt.Println("⚙️  Configuring Vault KV Engine and Secrets...")
			_ = client.Sys().Unmount("kv-k8s")
			_ = client.Sys().Mount("kv-k8s", &vault.MountInput{
				Type:    "kv",
				Options: map[string]string{"version": "2"},
			})
			defaultSecret := "I'm sorry, Dave. I'm afraid I can't do that."
			_, _ = client.Logical().Write("kv-k8s/data/app1", map[string]interface{}{
				"data": map[string]interface{}{
					"mysecret": defaultSecret,
				},
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
			if strings.TrimSpace(vsoChartVersion) != "" {
				helmArgs = append(helmArgs, "--version", vsoChartVersion)
			}

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

			modeTitle := "VSO ROLLING UPDATE DEMO"
			modeDetail := "Secret is synced by VaultStaticSecret and injected as HAL_SECRET env var"
			modeDetail2 := "index.html is rendered locally in the pod on startup"

			var appManifests string
			if csiMode {
				modeTitle = "VSO CSI EPHEMERAL DEMO"
				modeDetail = "Secret is mounted through csi.vso.hashicorp.com (no Kubernetes Secret sync)"
				modeDetail2 = "index.html is rendered locally in the pod from the CSI file"

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
		podNamePatterns: ["^hal-web-backend.*"]
---
apiVersion: apps/v1
kind: Deployment
metadata:
	name: hal-web-backend
	namespace: app1
spec:
	replicas: 2
	strategy:
		type: RollingUpdate
		rollingUpdate:
			maxUnavailable: 0
			maxSurge: 1
	selector:
		matchLabels:
			app: hal-web-backend
	template:
		metadata:
			labels:
				app: hal-web-backend
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
				- name: app
					image: %s
					ports:
						- containerPort: 80
					volumeMounts:
						- name: secrets
							mountPath: /mnt/secrets
							readOnly: true
					command: ["/bin/sh", "-c"]
					args:
						- |
							while [ ! -f /mnt/secrets/static_secret_0_mysecret ]; do sleep 1; done
							HAL_SECRET=$(cat /mnt/secrets/static_secret_0_mysecret)
							cat > /usr/local/apache2/htdocs/index.html <<EOF
							<html>
								<body style='font-family:system-ui;background:#f7fafc;color:#111827;padding:24px;'>
									<h1>HAL Vault + VSO CSI</h1>
									<p>Rendered in-pod. Secret projected by CSI driver.</p>
									<pre style='background:#111827;color:#34d399;padding:12px;border-radius:8px;'>${HAL_SECRET}</pre>
								</body>
							</html>
							EOF
							exec httpd-foreground

---
apiVersion: v1
kind: Service
metadata:
	name: hal-web-backend
	namespace: app1
spec:
	selector:
		app: hal-web-backend
	ports:
		- port: 80
			targetPort: 80
---
apiVersion: v1
kind: ConfigMap
metadata:
	name: hal-web-proxy-conf
	namespace: app1
data:
	default.conf: |
		upstream hal_backend {
			server hal-web-backend.app1.svc.cluster.local:80;
		}
		server {
			listen 80;
			location / {
				proxy_http_version 1.1;
				proxy_set_header Host $host;
				proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
				proxy_set_header X-Forwarded-Proto $scheme;
				proxy_pass http://hal_backend;
			}
		}
---
apiVersion: apps/v1
kind: Deployment
metadata:
	name: hal-web-proxy
	namespace: app1
spec:
	replicas: 1
	selector:
		matchLabels:
			app: hal-web-proxy
	template:
		metadata:
			labels:
				app: hal-web-proxy
		spec:
			containers:
				- name: nginx
					image: %s
					ports:
						- containerPort: 80
					volumeMounts:
						- name: proxy-conf
							mountPath: /etc/nginx/conf.d/default.conf
							subPath: default.conf
			volumes:
				- name: proxy-conf
					configMap:
						name: hal-web-proxy-conf
---
apiVersion: v1
kind: Service
metadata:
	name: hal-web-proxy
	namespace: app1
spec:
	type: NodePort
	selector:
		app: hal-web-proxy
	ports:
		- name: http
			port: 80
			targetPort: 80
			nodePort: 30080
`, vaultIP, webBackendImage, webProxyImage)
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
	refreshAfter: 15s
	destination:
		name: hal-web-secret
		create: true
	rolloutRestartTargets:
		- kind: Deployment
			name: hal-web-backend
  vaultAuthRef: default
---
apiVersion: apps/v1
kind: Deployment
metadata:
	name: hal-web-backend
  namespace: app1
spec:
	replicas: 2
	strategy:
		type: RollingUpdate
		rollingUpdate:
			maxUnavailable: 0
			maxSurge: 1
  selector:
    matchLabels:
			app: hal-web-backend
  template:
    metadata:
      labels:
				app: hal-web-backend
    spec:
      serviceAccountName: app1-sa
      containers:
				- name: app
					image: %s
          ports:
            - containerPort: 80
					env:
						- name: HAL_SECRET
							valueFrom:
								secretKeyRef:
									name: hal-web-secret
									key: mysecret
					command: ["/bin/sh", "-c"]
					args:
						- |
							cat > /usr/local/apache2/htdocs/index.html <<EOF
							<html>
								<body style='font-family:system-ui;background:#f7fafc;color:#111827;padding:24px;'>
									<h1>HAL Vault + VSO</h1>
									<p>Rendered in-pod. Secret injected via environment variable.</p>
									<pre style='background:#111827;color:#34d399;padding:12px;border-radius:8px;'>${HAL_SECRET}</pre>
								</body>
							</html>
							EOF
							exec httpd-foreground
---
apiVersion: v1
kind: Service
metadata:
	name: hal-web-backend
	namespace: app1
spec:
	selector:
		app: hal-web-backend
	ports:
		- port: 80
			targetPort: 80
---
apiVersion: v1
kind: ConfigMap
metadata:
	name: hal-web-proxy-conf
	namespace: app1
data:
	default.conf: |
		upstream hal_backend {
			server hal-web-backend.app1.svc.cluster.local:80;
		}
		server {
			listen 80;
			location / {
				proxy_http_version 1.1;
				proxy_set_header Host $host;
				proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
				proxy_set_header X-Forwarded-Proto $scheme;
				proxy_pass http://hal_backend;
			}
		}
---
apiVersion: apps/v1
kind: Deployment
metadata:
	name: hal-web-proxy
	namespace: app1
spec:
	replicas: 1
	selector:
		matchLabels:
			app: hal-web-proxy
	template:
		metadata:
			labels:
				app: hal-web-proxy
		spec:
			containers:
				- name: nginx
					image: %s
					ports:
						- containerPort: 80
					volumeMounts:
						- name: proxy-conf
							mountPath: /etc/nginx/conf.d/default.conf
							subPath: default.conf
			volumes:
				- name: proxy-conf
					configMap:
						name: hal-web-proxy-conf
---
apiVersion: v1
kind: Service
metadata:
	name: hal-web-proxy
	namespace: app1
spec:
	type: NodePort
	selector:
		app: hal-web-proxy
	ports:
		- name: http
			port: 80
			targetPort: 80
			nodePort: 30080
`, vaultIP, webBackendImage, webProxyImage)
			}

			if !applyK8s(appManifests) {
				fmt.Println("⚠️  Deployment stopped because Kubernetes manifests failed to apply.")
				return
			}

			_ = exec.Command("kubectl", "rollout", "status", "deployment/hal-web-backend", "-n", "app1", "--timeout=180s").Run()
			_ = exec.Command("kubectl", "rollout", "status", "deployment/hal-web-proxy", "-n", "app1", "--timeout=180s").Run()

			fmt.Println("\n✅ Kubernetes Secret Zero Environment Ready!")
			fmt.Println("---------------------------------------------------------")
			fmt.Printf("🌐 [%s]\n", modeTitle)
			fmt.Println("   Endpoint: http://web.localhost:8088")
			fmt.Println("   Backend:  2 replicas behind nginx reverse proxy")
			fmt.Printf("   %s\n", modeDetail)
			fmt.Printf("   %s\n", modeDetail2)
			fmt.Println("---------------------------------------------------------")
		}
	},
}

// -----------------------------------------------------------------------------
// Helper Functions
// -----------------------------------------------------------------------------

func applyK8s(yamlContent string) bool {
	yamlContent = strings.ReplaceAll(yamlContent, "\t", "  ")

	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(yamlContent)
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("❌ Failed to apply K8s manifests: %v\nOutput: %s\n", err, string(out))
		return false
	} else {
		fmt.Println("✅ Successfully applied Kubernetes manifests.")
		return true
	}
}

func writeKindConfigWithIngress() (string, error) {
	tempFile, err := os.CreateTemp("", "hal-kind-*.yaml")
	if err != nil {
		return "", err
	}

	config := `kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraPortMappings:
    - containerPort: 30080
      hostPort: 8088
      protocol: TCP
`

	if _, err := tempFile.WriteString(config); err != nil {
		_ = tempFile.Close()
		return "", err
	}

	if err := tempFile.Close(); err != nil {
		return "", err
	}

	return tempFile.Name(), nil
}

func detectK8sDemoMode() string {
	out, err := exec.Command(
		"kubectl",
		"get",
		"deploy",
		"hal-web-backend",
		"-n",
		"app1",
		"-o",
		"jsonpath={.spec.template.spec.volumes[?(@.csi.driver==\"csi.vso.hashicorp.com\")].name}",
	).Output()
	if err == nil && strings.TrimSpace(string(out)) != "" {
		return "csi"
	}

	out, err = exec.Command("kubectl", "get", "vaultstaticsecret", "vso-mysecret", "-n", "app1", "-o", "name").Output()
	if err == nil && strings.TrimSpace(string(out)) == "vaultstaticsecret.secrets.hashicorp.com/vso-mysecret" {
		return "native"
	}

	out, err = exec.Command("kubectl", "get", "csisecrets", "hal-csi-secrets", "-n", "app1", "-o", "name").Output()
	if err == nil && strings.TrimSpace(string(out)) == "csisecrets.secrets.hashicorp.com/hal-csi-secrets" {
		return "csi"
	}

	return "none"
}

func init() {
	// Standard Lifecycle Flags
	vaultK8sCmd.Flags().BoolVarP(&k8sEnable, "enable", "e", false, "Deploy KinD and configure Vault Secrets Operator")
	vaultK8sCmd.Flags().BoolVarP(&k8sDisable, "disable", "d", false, "Destroy KinD and clean up Vault configurations")
	vaultK8sCmd.Flags().BoolVarP(&k8sUpdate, "update", "u", false, "Reconcile cluster and VSO configuration")
	_ = vaultK8sCmd.Flags().MarkHidden("enable")
	_ = vaultK8sCmd.Flags().MarkHidden("disable")
	_ = vaultK8sCmd.Flags().MarkHidden("update")

	// Feature-Specific Flags
	vaultK8sCmd.Flags().BoolVar(&csiMode, "csi", false, "Use the VSO CSI Driver (Requires Vault Enterprise)")
	vaultK8sCmd.Flags().BoolVar(&jwtAuth, "jwt", false, "Use the advanced jwt-k8s OIDC architecture (experimental)")
	vaultK8sCmd.Flags().StringVar(&kindNodeImage, "kind-node-image", "kindest/node:v1.31.1", "KinD node image used when creating the cluster")
	vaultK8sCmd.Flags().StringVar(&vsoChartVersion, "vso-chart-version", "", "Helm chart version for hashicorp/vault-secrets-operator (empty uses latest)")
	vaultK8sCmd.Flags().StringVar(&webBackendImage, "web-backend-image", "httpd:2.4-alpine", "Demo backend container image")
	vaultK8sCmd.Flags().StringVar(&webProxyImage, "web-proxy-image", "nginx:alpine", "Demo reverse proxy container image")

	Cmd.AddCommand(vaultK8sCmd)
}
