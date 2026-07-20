package vault

// database-vso.go — "hal vault database enable --k8s"
//
// Extends the database enable workflow to surface Vault dynamic database
// credentials inside a real Kubernetes application.  After the database secrets
// engine is fully configured, calling enableDatabaseVSO will:
//
//  1. Boot a KinD cluster (or reuse one that is already running).
//  2. Create the "db-app" namespace and service account.
//  3. Configure Vault Kubernetes auth so the app pod can authenticate.
//  4. Create a least-privilege policy that only allows reading database/creds/<role>.
//  5. Install the Vault Secrets Operator via Helm.
//  6. Apply a VaultConnection, VaultAuth, and VaultDynamicSecret manifest so VSO
//     continuously mints fresh short-lived DB credentials and writes them into a
//     Kubernetes Secret.
//  7. Deploy a 2-replica httpd app that reads the credentials from that Secret
//     and renders them in a live HTML page accessible at http://db.localhost:8091.
//
// The VaultDynamicSecret CRD is the key differentiator vs. the existing
// "hal vault k8s enable" command, which uses a static KV secret.  Here the
// credentials in the pod rotate on every VSO refresh cycle, demonstrating the
// full Just-In-Time (JIT) database credential lifecycle inside a real workload.

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"hal/internal/global"

	vault "github.com/hashicorp/vault/api"
)

// enableDatabaseVSO is called at the tail of the database enable path when
// the --k8s flag is set.  engine is "docker" or "podman", client is an
// authenticated Vault API client, backend is "mariadb" or "oracle", and
// roleName is the Vault database role that was just configured.
func enableDatabaseVSO(engine string, client *vault.Client, backend, roleName string) {
	// ---- prerequisite binaries ----
	for _, bin := range []string{"kind", "kubectl", "helm"} {
		if _, err := exec.LookPath(bin); err != nil {
			fmt.Printf("❌ '%s' is not installed or not in PATH — required for --k8s.\n", bin)
			return
		}
	}

	// ---- KinD cluster ----
	clusterOut, _ := exec.Command("kind", "get", "clusters").Output()
	if strings.Contains(string(clusterOut), "kind") {
		fmt.Println("⚡ KinD cluster already running — skipping boot sequence...")
	} else {
		fmt.Println("🚀 Booting KinD cluster for database VSO demo...")
		kindConfigPath, cfgErr := writeHALKindConfig()
		if cfgErr != nil {
			fmt.Printf("❌ Failed to prepare KinD config: %v\n", cfgErr)
			return
		}
		defer func() { _ = os.Remove(kindConfigPath) }()

		startCmd := exec.Command("kind", "create", "cluster", "--config", kindConfigPath)
		if strings.TrimSpace(dbVSOKindNodeImage) != "" {
			startCmd.Args = append(startCmd.Args, "--image", dbVSOKindNodeImage)
		}
		env := os.Environ()
		isPodman := strings.Contains(engine, "podman")
		if isPodman {
			env = append(env, "KIND_EXPERIMENTAL_PROVIDER=podman")
		}
		env = append(env, "KIND_EXPERIMENTAL_DOCKER_NETWORK="+global.HalNetName)
		startCmd.Env = env
		startCmd.Stdout = os.Stdout
		startCmd.Stderr = os.Stderr

		if err := startCmd.Run(); err != nil {
			fmt.Printf("❌ Failed to start KinD: %v\n", err)
			return
		}
	}

	// ---- Namespaces ----
	fmt.Println("⚙️  Ensuring Kubernetes namespaces exist...")
	_ = exec.Command("kubectl", "create", "namespace", "vso").Run()
	_ = exec.Command("kubectl", "create", "namespace", dbVSONS).Run()

	// ---- Vault: kubernetes auth + policy ----
	fmt.Println("⚙️  Extracting K8s API CA and generating TokenReviewer SA...")
	_ = exec.Command("kubectl", "create", "sa", "vault-reviewer", "-n", "default").Run()
	_ = exec.Command("kubectl", "create", "clusterrolebinding", "vault-reviewer-binding",
		"--clusterrole=system:auth-delegator",
		"--serviceaccount=default:vault-reviewer").Run()

	caOut, _ := exec.Command("kubectl", "config", "view", "--raw", "--minify",
		"--flatten", "-o", "jsonpath={.clusters[].cluster.certificate-authority-data}").Output()
	decodedCA, _ := base64.StdEncoding.DecodeString(string(caOut))
	caCert := string(decodedCA)

	tokenOut, _ := exec.Command("kubectl", "create", "token",
		"vault-reviewer", "-n", "default", "--duration=87600h").Output()
	reviewerToken := strings.TrimSpace(string(tokenOut))

	kindIPOut, _ := exec.Command(engine, "inspect",
		"-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}",
		"kind-control-plane").Output()
	kindIP := strings.TrimSpace(string(kindIPOut))
	if kindIP == "" {
		kindIP = "kind-control-plane"
	}

	fmt.Printf("⚙️  Enabling dedicated Kubernetes auth engine at %s/ on Vault...\n", dbVSOAuthMount)
	_ = client.Sys().EnableAuthWithOptions(dbVSOAuthMount, &vault.EnableAuthOptions{Type: "kubernetes"})

	if _, err := client.Logical().Write("auth/"+dbVSOAuthMount+"/config", map[string]interface{}{
		"kubernetes_host":        fmt.Sprintf("https://%s:6443", kindIP),
		"kubernetes_ca_cert":     caCert,
		"token_reviewer_jwt":     reviewerToken,
		"disable_iss_validation": true,
	}); err != nil {
		fmt.Printf("❌ Vault rejected Kubernetes configuration: %v\n", err)
		return
	}

	// Policy: only read database/creds/<role> and check licence status.
	policyDef := fmt.Sprintf(`
path "database/creds/%s" { capabilities = ["read"] }
path "sys/license/status"  { capabilities = ["read"] }
`, roleName)
	_ = client.Sys().PutPolicy(dbVSOPolicyName, policyDef)

	if _, err := client.Logical().Write("auth/"+dbVSOAuthMount+"/role/db-app-role", map[string]interface{}{
		"bound_service_account_names":      dbVSOSAName,
		"bound_service_account_namespaces": dbVSONS,
		"bound_audiences":                  []string{"vault"},
		"token_policies":                   []string{dbVSOPolicyName},
		"token_ttl":                        "1h",
	}); err != nil {
		fmt.Printf("❌ Failed to create Vault auth role for db-app: %v\n", err)
		return
	}

	// ---- VSO Helm install ----
	fmt.Println("⚙️  Deploying Vault Secrets Operator via Helm...")
	_ = exec.Command("helm", "repo", "add", "hashicorp", "https://helm.releases.hashicorp.com").Run()
	_ = exec.Command("helm", "repo", "update").Run()

	helmArgs := []string{
		"upgrade", "--install", "vault-secrets-operator",
		"hashicorp/vault-secrets-operator", "-n", "vso",
	}
	if strings.TrimSpace(dbVSOChartVersion) != "" {
		helmArgs = append(helmArgs, "--version", dbVSOChartVersion)
	}
	if err := exec.Command("helm", helmArgs...).Run(); err != nil {
		fmt.Printf("❌ Failed to install VSO: %v\n", err)
		return
	}

	// ---- Wait for CRDs + controller ----
	fmt.Println("⏳ Waiting for VSO CRDs to be established (up to 60s)...")
	for _, crd := range []string{
		"crd/vaultconnections.secrets.hashicorp.com",
		"crd/vaultauths.secrets.hashicorp.com",
		"crd/vaultdynamicsecrets.secrets.hashicorp.com",
	} {
		_ = exec.Command("kubectl", "wait", "--for=condition=Established", crd, "--timeout=60s").Run()
	}

	fmt.Println("⏳ Waiting for VSO controller pods to become Ready (up to 120s)...")
	_ = exec.Command("kubectl", "wait", "--for=condition=Ready", "pod",
		"-l", "app.kubernetes.io/name=vault-secrets-operator",
		"-n", "vso", "--timeout=120s").Run()

	fmt.Println("⏳ Giving webhooks 5 seconds to wire up TLS...")
	time.Sleep(5 * time.Second)

	// ---- Resolve vault IP inside hal-net ----
	vaultIPOut, _ := exec.Command(engine, "inspect",
		"-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}",
		vaultContainer).Output()
	vaultIP := strings.TrimSpace(string(vaultIPOut))
	if vaultIP == "" {
		vaultIP = vaultContainer
	}

	// ---- Service account ----
	_ = exec.Command("kubectl", "create", "sa", dbVSOSAName, "-n", dbVSONS).Run()

	// ---- Database connection details for the copy button ----
	dbLabel := "MariaDB"
	dbHost := vaultMariaDBHostAlias
	dbPort := fmt.Sprintf("%d", vaultMariaDBPort)
	dbClient := "mysql"
	if backend == "oracle" {
		dbLabel = "Oracle Free"
		dbHost = vaultOracleHostAlias
		dbPort = fmt.Sprintf("%d", vaultOraclePort)
		dbClient = "sqlplus"
	}

	// ---- Kubernetes manifests ----
	// VaultDynamicSecret instructs VSO to mint fresh credentials from the
	// database secrets engine and write them into a Kubernetes Secret.  The
	// Secret is mounted as individual env vars in the httpd pod so the rendered
	// HTML page shows the live, short-lived username/password in real time.
	manifests := buildDBVSOManifests(vaultIP, roleName, dbLabel, dbHost, dbPort, dbClient)

	fmt.Println("⚙️  Applying Kubernetes manifests (VaultDynamicSecret + demo app)...")
	if !applyK8s(manifests) {
		fmt.Println("⚠️  Deployment stopped: Kubernetes manifests failed to apply.")
		return
	}

	_ = exec.Command("kubectl", "rollout", "status",
		"deployment/"+dbVSOAppName, "-n", dbVSONS, "--timeout=180s").Run()
	_ = exec.Command("kubectl", "rollout", "status",
		"deployment/"+dbVSOProxyName, "-n", dbVSONS, "--timeout=180s").Run()

	fmt.Println()
	fmt.Println("✅ Database VSO Environment Ready!")
	global.RefreshHalHealth(engine)
	fmt.Println("---------------------------------------------------------")
	fmt.Println("🌐 [VSO DYNAMIC DATABASE CREDENTIAL DEMO]")
	fmt.Printf("   Endpoint : http://db.localhost:%d\n", dbVSOHostPort)
	fmt.Println("   Backend  : 2 replicas behind nginx reverse proxy")
	fmt.Printf("   Database : %s  |  Role: %s\n", dbLabel, roleName)
	fmt.Println("   Secret   : VSO mints new DB credentials via VaultDynamicSecret")
	fmt.Println("              and writes them into K8s Secret '" + dbVSOSecretName + "'")
	fmt.Println("   App      : reads username + password from env vars at startup")
	fmt.Println()
	fmt.Println("   Reload the page to observe TTL countdown and credential rotation.")
	fmt.Println("---------------------------------------------------------")
}

// buildDBVSOManifests returns the full YAML to apply for the database VSO demo.
// vaultIP is the Vault container IP on hal-net, roleName is the DB role to read.
// dbHost/dbPort/dbClient describe how to connect to the database from the host
// so the app can render a ready-to-paste login command.
func buildDBVSOManifests(vaultIP, roleName, dbLabel, dbHost, dbPort, dbClient string) string {
	appImage := dbVSOBackendImage + ":" + dbVSOBackendTag
	proxyImage := dbVSOProxyImage + ":" + dbVSOProxyTag

	// loginCmd is the template rendered inside the shell heredoc.
	// For MySQL/MariaDB: mysql -h <host> -P <port> -u <user> -p<pass>
	// For Oracle sqlplus: sqlplus <user>/"<pass>"@<host>:<port>/FREEPDB1
	var loginCmdTemplate string
	if dbClient == "sqlplus" {
		loginCmdTemplate = fmt.Sprintf(`sqlplus ${DB_USERNAME}/"${DB_PASSWORD}"@%s:%s/%s`, dbHost, dbPort, vaultOraclePDB)
	} else {
		loginCmdTemplate = fmt.Sprintf(`%s -h %s -P %s -u ${DB_USERNAME} -p${DB_PASSWORD}`, dbClient, dbHost, dbPort)
	}

	return fmt.Sprintf(`---
apiVersion: secrets.hashicorp.com/v1beta1
kind: VaultConnection
metadata:
  name: default
  namespace: %s
spec:
  address: http://%s:8200
---
apiVersion: secrets.hashicorp.com/v1beta1
kind: VaultAuth
metadata:
  name: default
  namespace: %s
spec:
  method: kubernetes
  mount: kubernetes-db
  kubernetes:
    role: db-app-role
    serviceAccount: %s
    audiences: ["vault"]
  vaultConnectionRef: default
---
apiVersion: secrets.hashicorp.com/v1beta1
kind: VaultDynamicSecret
metadata:
  name: hal-db-dynamic
  namespace: %s
spec:
  mount: database
  path: creds/%s
  destination:
    name: %s
    create: true
  renewalPercent: 67
  rolloutRestartTargets:
    - kind: Deployment
      name: %s
  vaultAuthRef: default
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: %s
  namespace: %s
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: %s
spec:
  replicas: 2
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 0
      maxSurge: 1
  selector:
    matchLabels:
      app: %s
  template:
    metadata:
      labels:
        app: %s
    spec:
      serviceAccountName: %s
      containers:
        - name: app
          image: %s
          ports:
            - containerPort: 80
          env:
            - name: DB_USERNAME
              valueFrom:
                secretKeyRef:
                  name: %s
                  key: username
            - name: DB_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: %s
                  key: password
          command: ["/bin/sh", "-c"]
          args:
            - |
              LOGIN_CMD="%s"
              cat > /usr/local/apache2/htdocs/index.html <<EOF
              <html>
                <head>
                  <meta charset='utf-8'>
                  <style>
                    body{font-family:system-ui,sans-serif;background:#f7fafc;color:#111827;padding:24px;max-width:600px;}
                    h1{margin-bottom:4px;}
                    p{margin-top:4px;color:#57606a;}
                    table{border-collapse:collapse;width:100%%;}
                    th,td{text-align:left;padding:8px;}
                    th{border-bottom:2px solid #e5e7eb;}
                    td{border-bottom:1px solid #f0f0f0;}
                    pre{margin:0;padding:8px;border-radius:6px;font-size:13px;white-space:pre-wrap;word-break:break-all;}
                    .green{background:#111827;color:#34d399;}
                    .red{background:#111827;color:#f87171;}
                    .cmd{background:#1e293b;color:#e2e8f0;}
                    .copy-btn{
                      margin-top:16px;display:inline-flex;align-items:center;gap:8px;
                      padding:10px 18px;background:#3b82d4;color:#fff;border:none;
                      border-radius:6px;font-size:14px;cursor:pointer;
                    }
                    .copy-btn:active{background:#2563eb;}
                    .copied{background:#16a34a !important;}
                    .label{font-size:11px;color:#57606a;margin-bottom:2px;}
                  </style>
                </head>
                <body>
                  <h1>HAL Vault + VSO</h1>
                  <p>Dynamic DB credentials minted by Vault, synced via <strong>VaultDynamicSecret</strong>.</p>
                  <table>
                    <tr>
                      <th>Field</th><th>Value</th>
                    </tr>
                    <tr>
                      <td>Database</td>
                      <td><code>%s</code></td>
                    </tr>
                    <tr>
                      <td>Role</td>
                      <td><code>%s</code></td>
                    </tr>
                    <tr>
                      <td>Username</td>
                      <td><pre class='green'>${DB_USERNAME}</pre></td>
                    </tr>
                    <tr>
                      <td>Password</td>
                      <td><pre class='red'>${DB_PASSWORD}</pre></td>
                    </tr>
                  </table>
                  <div style='margin-top:20px;'>
                    <div class='label'>LOGIN COMMAND</div>
                    <pre class='cmd' id='cmd'>${LOGIN_CMD}</pre>
                    <button class='copy-btn' id='btn' onclick='copyCmd()'>
                      <svg width='16' height='16' viewBox='0 0 24 24' fill='none' stroke='currentColor' stroke-width='2'><rect x='9' y='9' width='13' height='13' rx='2'/><path d='M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1'/></svg>
                      Copy login command
                    </button>
                  </div>
                  <script>
                    function copyCmd(){
                      var t=document.getElementById('cmd').innerText;
                      navigator.clipboard.writeText(t).then(function(){
                        var b=document.getElementById('btn');
                        b.textContent='Copied!';
                        b.classList.add('copied');
                        setTimeout(function(){b.textContent='Copy login command';b.classList.remove('copied');},2000);
                      });
                    }
                  </script>
                </body>
              </html>
              EOF
              exec httpd-foreground
---
apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
spec:
  selector:
    app: %s
  ports:
    - port: 80
      targetPort: 80
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: hal-db-proxy-conf
  namespace: %s
data:
  default.conf: |
    upstream hal_db_backend {
      server %s.%s.svc.cluster.local:80;
    }
    server {
      listen 80;
      location / {
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_pass http://hal_db_backend;
      }
    }
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: %s
spec:
  replicas: 1
  selector:
    matchLabels:
      app: %s
  template:
    metadata:
      labels:
        app: %s
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
            name: hal-db-proxy-conf
---
apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
spec:
  type: NodePort
  selector:
    app: %s
  ports:
    - name: http
      port: 80
      targetPort: 80
      nodePort: %d
`,
		// VaultConnection
		dbVSONS, vaultIP,
		// VaultAuth
		dbVSONS, dbVSOSAName,
		// VaultDynamicSecret
		dbVSONS, roleName, dbVSOSecretName, dbVSOAppName,
		// ServiceAccount
		dbVSOSAName, dbVSONS,
		// Deployment (app)
		dbVSOAppName, dbVSONS,
		dbVSOAppName,
		dbVSOAppName,
		dbVSOSAName,
		appImage,
		dbVSOSecretName,
		dbVSOSecretName,
		// shell: LOGIN_CMD="..."
		loginCmdTemplate,
		// HTML table content
		dbLabel, roleName,
		// Service (app)
		dbVSOAppName, dbVSONS, dbVSOAppName,
		// ConfigMap
		dbVSONS, dbVSOAppName, dbVSONS,
		// Deployment (proxy)
		dbVSOProxyName, dbVSONS,
		dbVSOProxyName,
		dbVSOProxyName,
		proxyImage,
		// NodePort Service
		dbVSOProxyName, dbVSONS, dbVSOProxyName, dbVSONodePort,
	)
}

// disableDatabaseVSOVault cleans up the Vault-side resources created by
// enableDatabaseVSO.  It only touches the dedicated "kubernetes-db/" auth
// mount — it never disables "kubernetes/" (owned by hal vault k8s) or
// "kubernetes-pki/" (owned by hal vault pki --k8s).
func disableDatabaseVSOVault(client *vault.Client) {
	fmt.Println("   🧹 Cleaning up Vault resources for database VSO demo...")

	// Remove identity entities bound to the kubernetes-db/ mount accessor only.
	authMounts, err := client.Sys().ListAuth()
	if err == nil {
		mountKey := dbVSOAuthMount + "/"
		if mount, exists := authMounts[mountKey]; exists {
			accessor := mount.Accessor
			if entitiesList, err := client.Logical().List("identity/entity/id"); err == nil &&
				entitiesList != nil && entitiesList.Data != nil {
				if keys, ok := entitiesList.Data["keys"].([]interface{}); ok {
					for _, key := range keys {
						entityID := key.(string)
						if entityData, err := client.Logical().Read("identity/entity/id/" + entityID); err == nil &&
							entityData != nil && entityData.Data != nil {
							if aliases, ok := entityData.Data["aliases"].([]interface{}); ok {
								for _, aliasObj := range aliases {
									if alias, ok := aliasObj.(map[string]interface{}); ok {
										if alias["mount_accessor"] == accessor {
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

	// Disable only our dedicated mount — never "kubernetes/" or "kubernetes-pki/".
	_ = client.Sys().DisableAuth(dbVSOAuthMount)
	_ = client.Sys().DeletePolicy(dbVSOPolicyName)
}

// disableDatabaseVSOCluster conditionally tears down the shared KinD cluster.
// It preserves the cluster if any co-tenant feature is still active:
//   - hal vault k8s       → namespace "app1" present
//   - hal vault pki --k8s → namespace "pki-demo" present
//   - hal vault pki --acme → namespace "pki-acme-demo" present
//
// This mirrors the guard in hal vault pki disable (pki.go) and means any
// combination of --k8s features can coexist safely on the same cluster.
func disableDatabaseVSOCluster() {
	// Check each co-tenant namespace.
	coTenants := []struct {
		ns      string
		feature string
	}{
		{"app1", "hal vault k8s"},
		{"pki-demo", "hal vault pki --k8s"},
		{"pki-acme-demo", "hal vault pki --acme"},
	}

	for _, t := range coTenants {
		out, _ := exec.Command("kubectl", "get", "namespace", t.ns,
			"--ignore-not-found", "-o", "name").Output()
		if strings.TrimSpace(string(out)) != "" {
			fmt.Printf("ℹ️  KinD cluster preserved (%s is still active).\n", t.feature)
			fmt.Printf("   Run '%s disable' to remove it when done.\n", t.feature)
			return
		}
	}

	// No co-tenants — safe to delete.
	fmt.Println("⚙️  Destroying KinD cluster...")
	_ = exec.Command("kind", "delete", "cluster").Run()
}
