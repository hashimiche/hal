package terraform

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var (
	tfeVersion          string
	tfePassword         string
	pgVersion           string
	redisVersion        string
	minioVersion        string
	minioAPIPort        int
	minioConsolePort    int
	tfeProxyNginxTag    string
	tfeForce            bool
	tfeConfigureObs     bool
	deployTFEOrg        string
	deployTFEProject    string
	deployTFEAdminUser  string
	deployTFEAdminEmail string
	deployTFEAdminPass  string
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy a local Terraform Enterprise 1.x (FDO) instance via Docker",
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		if tfeConfigureObs {
			if !global.IsContainerRunning(engine, "hal-tfe") {
				fmt.Println("❌ Terraform Enterprise is not running. Deploy it first before configuring observability artifacts.")
				fmt.Println("   💡 Run 'hal terraform deploy' and then retry with '--configure-obs' if needed.")
				return
			}
			if !global.IsObsReady(engine) {
				fmt.Printf("❌ Observability stack is not ready. Missing: %s\n", strings.Join(global.ObsMissingComponents(engine), ", "))
				fmt.Println("   💡 Run 'hal obs deploy' first, then retry '--configure-obs'.")
				return
			}

			fmt.Println("🩺 Configuring observability artifacts for Terraform Enterprise...")
			for _, warning := range global.RegisterObsArtifacts("terraform", []string{"hal-tfe:9090"}) {
				fmt.Printf("⚠️  %s\n", warning)
			}
			fmt.Println("✅ Terraform Enterprise observability artifacts refreshed.")
			return
		}

		// 1. STRICT LICENSE ENFORCEMENT
		license := os.Getenv("TFE_LICENSE")
		if license == "" {
			fmt.Println("❌ Error: TFE requires a valid license to boot.")
			fmt.Println("   💡 Export your license to your environment before running this command:")
			fmt.Println("      export TFE_LICENSE='your_massive_ibm_hashicorp_license_string'")
			return
		}

		os.Setenv("TFE_ENCRYPTION_PASSWORD", tfePassword)
		os.Setenv("TFE_DATABASE_PASSWORD", "tfe_password")

		global.WarnIfEngineResourcesTight(engine, "terraform-deploy")
		if !global.DryRun {
			proceed, err := global.ConfirmScenarioProceed(engine, "terraform-deploy")
			if err != nil && global.Debug {
				fmt.Printf("[DEBUG] Capacity confirmation unavailable: %v\n", err)
			}
			if err == nil && !proceed {
				fmt.Printf("🛑 Terraform Enterprise deployment aborted to protect your %s engine.\n", engine)
				return
			}
		}

		isPodman := strings.Contains(engine, "podman")

		// Keep an unprivileged HTTPS listener for rootless Podman.
		tfeHostname := "tfe.localhost"
		healthURL := "https://tfe.localhost:8443/_health_check"
		uiURL := "https://tfe.localhost:8443"
		// Stable internal proxy IP on hal-net used to route in-cluster tfe.localhost:443 traffic.
		proxyInternalIP := "10.89.3.54"

		// 2. FORGE THE TLS CERTIFICATES
		fmt.Println("🔐 Forging local TLS certificates for TFE...")
		homeDir, _ := os.UserHomeDir()
		certDir := filepath.Join(homeDir, ".hal", "tfe-certs")

		if tfeForce {
			fmt.Println("♻️  Force flag detected. Purging existing TFE resources...")
			// 🎯 Included the proxy in the teardown list
			_ = exec.Command(engine, "rm", "-f", "hal-tfe", "hal-tfe-proxy", "hal-tfe-db", "hal-tfe-redis", "hal-tfe-minio").Run()
			_ = os.Remove(filepath.Join(certDir, "cert.pem"))
			_ = os.Remove(filepath.Join(certDir, "key.pem"))
		}
		if err := ensureCerts(certDir); err != nil {
			fmt.Printf("❌ Failed to generate TLS certificates: %v\n", err)
			return
		}

		fmt.Printf("🚀 Deploying Terraform Enterprise %s (PG: %s, Redis: %s) via %s...\n", tfeVersion, pgVersion, redisVersion, engine)

		// 3. SECURE REGISTRY AUTHENTICATION
		fmt.Println("🔑 Authenticating with HashiCorp private image registry...")
		loginCmd := exec.Command(engine, "login", "images.releases.hashicorp.com", "-u", "terraform", "--password-stdin")
		loginCmd.Stdin = strings.NewReader(license)
		if err := loginCmd.Run(); err != nil {
			fmt.Println("❌ Error: Failed to authenticate with images.releases.hashicorp.com.")
			return
		}

		// 4. Ensure the global HAL network exists
		global.EnsureNetwork(engine)

		// 5. Deploy PostgreSQL
		fmt.Printf("⚙️  Provisioning TFE PostgreSQL Database...\n")
		_ = exec.Command(engine, "run", "-d", "--name", "hal-tfe-db", "--network", "hal-net",
			"-e", "POSTGRES_USER=tfe", "-e", "POSTGRES_PASSWORD=tfe_password", "-e", "POSTGRES_DB=tfe",
			fmt.Sprintf("postgres:%s-alpine", pgVersion)).Run()

		// 6. Deploy Redis
		fmt.Printf("⚙️  Provisioning TFE Redis Cache...\n")
		_ = exec.Command(engine, "run", "-d", "--name", "hal-tfe-redis", "--network", "hal-net",
			fmt.Sprintf("redis:%s-alpine", redisVersion)).Run()

		// 7. Deploy MinIO (S3 Mock)
		fmt.Println("⚙️  Provisioning TFE Object Storage (MinIO)...")
		_ = exec.Command(engine, "run", "-d", "--name", "hal-tfe-minio", "--network", "hal-net",
			"-p", fmt.Sprintf("%d:9000", minioAPIPort), "-p", fmt.Sprintf("%d:9001", minioConsolePort),
			"-e", "MINIO_ROOT_USER=minioadmin", "-e", "MINIO_ROOT_PASSWORD=minioadmin",
			fmt.Sprintf("minio/minio:%s", minioVersion), "server", "/data", "--console-address", ":9001").Run()

		time.Sleep(3 * time.Second)
		_ = exec.Command(engine, "exec", "hal-tfe-minio", "sh", "-c", "mkdir -p /data/tfe-data").Run()

		// 8. Deploy TFE Core (NO EXPOSED HOST PORTS!)
		fmt.Println("⚙️  Booting TFE Core Application (This requires heavy compute)...")
		tfeArgs := []string{
			"run", "-d",
			"--name", "hal-tfe",
			"--network", "hal-net",
			"--privileged",
			"--add-host", "hal-tfe:127.0.0.1",
			"--add-host", fmt.Sprintf("tfe.localhost:%s", proxyInternalIP),
			"-v", "/var/run/docker.sock:/var/run/docker.sock",
		}

		if isPodman {
			tfeArgs = append(tfeArgs, "--security-opt", "label=disable")
			tfeArgs = append(tfeArgs, "--security-opt", "seccomp=unconfined")
		}

		tfeArgs = append(tfeArgs, "-v", fmt.Sprintf("%s:/etc/ssl/tfe:Z", certDir))

		tfeArgs = append(tfeArgs,
			"-e", "TFE_OPERATIONAL_MODE=external",
			"-e", fmt.Sprintf("TFE_HOSTNAME=%s", tfeHostname),
			"-e", "TFE_VCS_HOSTNAME=tfe.localhost:8443",
			"-e", "VAULT_ADDR=http://127.0.0.1:8200",
			"-e", "TFE_METRICS_ENABLE=true",
			"-e", "TFE_METRICS_HTTP_PORT=9090",
			"-e", "TFE_METRICS_HTTPS_PORT=9091",
			"-e", "TFE_IA_HOSTNAME=hal-tfe",
			"-e", "TFE_VAULT_DISABLE_MLOCK=true",
			"-e", "TFE_VAULT_ADDR=http://127.0.0.1:8200", // 🎯 Sorry Copilot!
			"-e", "TFE_IA_INTERNAL_VAULT_ADDR=http://127.0.0.1:8200", // 🎯 Sorry Copilot!
			"-e", "TFE_RUN_PIPELINE_DOCKER_NETWORK=hal-net",
			"-e", "TFE_HTTP_PORT=8080",
			"-e", "TFE_HTTPS_PORT=8443",
			"-e", "TFE_TLS_CERT_FILE=/etc/ssl/tfe/cert.pem",
			"-e", "TFE_TLS_KEY_FILE=/etc/ssl/tfe/key.pem",
			"-e", "TFE_DISK_CACHE_VOLUME_NAME=hal-tfe-cache",
			"-e", "TFE_LICENSE",
			"-e", "TFE_ENCRYPTION_PASSWORD",
			"-e", "TFE_DATABASE_USER=tfe",
			"-e", "TFE_DATABASE_PASSWORD",
			"-e", "TFE_DATABASE_HOST=hal-tfe-db",
			"-e", "TFE_DATABASE_NAME=tfe",
			"-e", "TFE_DATABASE_PARAMETERS=sslmode=disable",
			"-e", "TFE_REDIS_HOST=hal-tfe-redis",
			"-e", "TFE_REDIS_USE_TLS=false",
			"-e", "TFE_REDIS_USE_AUTH=false",
			"-e", "TFE_OBJECT_STORAGE_TYPE=s3",
			"-e", "TFE_OBJECT_STORAGE_S3_USE_INSTANCE_PROFILE=false",
			"-e", "TFE_OBJECT_STORAGE_S3_ENDPOINT=http://hal-tfe-minio:9000",
			"-e", "TFE_OBJECT_STORAGE_S3_BUCKET=tfe-data",
			"-e", "TFE_OBJECT_STORAGE_S3_REGION=us-east-1",
			"-e", "TFE_OBJECT_STORAGE_S3_ACCESS_KEY_ID=minioadmin",
			"-e", "TFE_OBJECT_STORAGE_S3_SECRET_ACCESS_KEY=minioadmin",
			"-e", "TFE_OBJECT_STORAGE_S3_FORCE_PATH_STYLE=true",
			"-e", "TFE_CAPACITY_CONCURRENCY=5",
			fmt.Sprintf("images.releases.hashicorp.com/hashicorp/terraform-enterprise:%s", tfeVersion),
		)

		out, err := exec.Command(engine, tfeArgs...).CombinedOutput()
		if err != nil {
			fmt.Printf("❌ Failed to start TFE: %s\n", string(out))
			return
		}

		// Ensure in-container components trust the local TLS certificate used by tfe.localhost.
		// Without this, archivist callback uploads can fail with x509 unknown authority and
		// configuration versions remain stuck in "fetching".
		if trustOut, trustErr := exec.Command(
			engine,
			"exec",
			"--user",
			"0",
			"hal-tfe",
			"sh",
			"-lc",
			"cp /etc/ssl/tfe/cert.pem /usr/local/share/ca-certificates/tfe-localhost.crt && update-ca-certificates >/dev/null 2>&1 && supervisorctl restart tfe:archivist >/dev/null 2>&1",
		).CombinedOutput(); trustErr != nil {
			fmt.Printf("⚠️  Could not refresh TFE trust store automatically: %s\n", strings.TrimSpace(string(trustOut)))
		}

		// TFE 1.2.0 on this local Podman flow generates an agent-run task-worker config that
		// mounts /tmp/terraform read-only, but the remote agent still downloads the Terraform
		// binary into that path. Make the cache mount writable so remote runs can start.
		if taskWorkerOut, taskWorkerErr := exec.Command(
			engine,
			"exec",
			"--user",
			"0",
			"hal-tfe",
			"sh",
			"-lc",
			"sed -i 's/readonly = \"true\"/readonly = \"false\"/' /run/terraform-enterprise/task-worker/config.hcl && supervisorctl restart tfe:task-worker >/dev/null 2>&1",
		).CombinedOutput(); taskWorkerErr != nil {
			fmt.Printf("⚠️  Could not patch TFE task-worker cache mount automatically: %s\n", strings.TrimSpace(string(taskWorkerOut)))
		}

		// 8.5 Deploy the Magic Redirect Fixer (AFTER TFE BOOTS!)
		fmt.Println("⚙️  Deploying TFE Ingress Proxy (The Redirect Fixer)...")
		nginxConfig := `events {}
http {
	server {
		listen 443 ssl;
		listen 8443 ssl;
		server_name tfe.localhost;
		
		ssl_certificate /etc/ssl/tfe/cert.pem;
		ssl_certificate_key /etc/ssl/tfe/key.pem;
		
		location / {
			# 🎯 Direct pass. Works perfectly in both Docker and Podman!
			proxy_pass https://hal-tfe:8443;
			
			proxy_set_header Host tfe.localhost:8443;
			proxy_set_header X-Forwarded-Host tfe.localhost:8443;
			proxy_set_header X-Forwarded-Port 8443;
			proxy_set_header X-Real-IP $remote_addr;
			proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
			proxy_set_header X-Forwarded-Proto https;
			proxy_set_header Accept-Encoding "";
			
			# 🎯 Skip validating TFE's internal self-signed cert
			proxy_ssl_verify off;

			# TFE generates archivist object URLs without :8443. Rewrite them in JSON/UI
			# responses so plan/apply log links remain reachable from the host OS.
			sub_filter_once off;
			sub_filter_types application/json application/vnd.api+json text/html text/plain;
			sub_filter 'https://tfe.localhost/_archivist/' 'https://tfe.localhost:8443/_archivist/';
			
			# 🎯 Rewrite canonical redirects to the externally reachable :8443 endpoint.
			proxy_redirect ~^https://tfe\.localhost(?::443)?(/.*)$ https://tfe.localhost:8443$1;
			proxy_redirect ~^https://hal-tfe(?::8443)?(/.*)$ https://tfe.localhost:8443$1;
		}
	}
}`
		proxyConfPath := filepath.Join(homeDir, ".hal", "tfe-proxy.conf")
		_ = os.WriteFile(proxyConfPath, []byte(nginxConfig), 0644)

		_ = exec.Command(engine, "run", "-d", "--name", "hal-tfe-proxy", "--network", "hal-net", "--ip", proxyInternalIP,
			"--network-alias", "tfe.localhost",
			"-p", "8443:8443", // 🎯 Only the proxy exposes port 8443 to the host OS
			"-v", fmt.Sprintf("%s:/etc/ssl/tfe:ro", certDir),
			"-v", fmt.Sprintf("%s:/etc/nginx/nginx.conf:ro", proxyConfPath),
			fmt.Sprintf("nginx:%s", tfeProxyNginxTag)).Run()

		// 9. THE HEALTH CHECK PHASE
		fmt.Println("⏳ Waiting for TFE to initialize (WARNING: This can take 3-5 minutes!)...")

		// This will naturally hit the Proxy, which routes to TFE
		if err := waitForService("TFE", healthURL, 60); err != nil {
			handleDockerFailure("hal-tfe", engine)
			return
		}

		fmt.Println("\n✅ Terraform Enterprise 1.x is UP!")
		fmt.Println("---------------------------------------------------------")
		fmt.Printf("🔗 UI Address:   %s\n", uiURL)
		fmt.Printf("🗂️  MinIO API:    http://127.0.0.1:%d\n", minioAPIPort)
		fmt.Printf("🧭 MinIO Console: http://127.0.0.1:%d\n", minioConsolePort)
		fmt.Printf("👤 Admin User:   %s\n", deployTFEAdminUser)
		fmt.Printf("🔑 Admin Pass:   %s\n", deployTFEAdminPass)
		token, _, err := ensureTFEFoundation(engine, tfeFoundationConfig{
			BaseURL:       uiURL,
			OrgName:       deployTFEOrg,
			ProjectName:   deployTFEProject,
			AdminUsername: deployTFEAdminUser,
			AdminEmail:    deployTFEAdminEmail,
			AdminPassword: deployTFEAdminPass,
		})
		if err != nil {
			fmt.Printf("⚠️  TFE foundation bootstrap incomplete: %v\n", err)
			fmt.Println("   💡 HAL could not mint a usable API token automatically from this TFE instance.")
		} else {
			fmt.Println("✅ TFE foundation ready: admin token + org/project are configured.")
			if token != "" {
				fmt.Println("   📄 Token cache: ~/.hal/tfe-app-api-token")
			}
		}
		for _, warning := range global.RegisterObsArtifacts("terraform", []string{"hal-tfe:9090"}) {
			fmt.Printf("⚠️  %s\n", warning)
		}
		fmt.Println("⚠️  Note:        Accept the browser warning for the self-signed certificate.")
		fmt.Println("\n💡 Next Step:")
		fmt.Println("   Run 'hal terraform workspace -e' to bootstrap org/project/workspace wiring.")
		fmt.Println("---------------------------------------------------------")
	},
}

func ensureCerts(certDir string) error {
	certPath := filepath.Join(certDir, "cert.pem")
	keyPath := filepath.Join(certDir, "key.pem")

	if _, err := os.Stat(certPath); err == nil {
		if !shouldRotatePrimaryTFECert(certPath) {
			return nil
		}
		_ = os.Remove(certPath)
		_ = os.Remove(keyPath)
	}

	os.MkdirAll(certDir, 0755)

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return err
	}
	if serialNumber.Sign() == 0 {
		serialNumber = big.NewInt(time.Now().UnixNano())
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"HAL Primary TFE Local Dev Environment"},
			CommonName:   "tfe.localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost", "hal-tfe", "tfe.localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	certOut, _ := os.Create(certPath)
	defer certOut.Close()
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyOut, _ := os.Create(keyPath)
	defer keyOut.Close()
	pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	return nil
}

func shouldRotatePrimaryTFECert(certPath string) bool {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return true
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return true
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return true
	}

	hasPrimaryDNS := false
	for _, name := range cert.DNSNames {
		if name == "tfe.localhost" {
			hasPrimaryDNS = true
			break
		}
	}
	if !hasPrimaryDNS {
		return true
	}

	legacyIssuer := strings.Contains(strings.Join(cert.Subject.Organization, ","), "HAL Local Dev Environment")
	if legacyIssuer && cert.SerialNumber.Cmp(big.NewInt(1)) == 0 {
		return true
	}

	return false
}

func waitForService(name string, url string, maxRetries int) error {
	customTransport := http.DefaultTransport.(*http.Transport).Clone()
	customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	client := http.Client{
		Timeout:   2 * time.Second,
		Transport: customTransport,
	}

	for i := 0; i < maxRetries; i++ {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout waiting for %s at %s", name, url)
}

func handleDockerFailure(container string, engine string) {
	fmt.Printf("❌ %s failed to start or become healthy.\n", container)
	fmt.Println("📜 Fetching recent container logs...")
	out, _ := exec.Command(engine, "logs", "--tail", "50", container).CombinedOutput()
	fmt.Println(strings.TrimSpace(string(out)))
}

func init() {
	deployCmd.Flags().StringVarP(&tfeVersion, "version", "v", "1.2.0", "Terraform Enterprise Docker image tag")
	deployCmd.Flags().StringVar(&pgVersion, "pg-version", "16", "PostgreSQL version for TFE backend")
	deployCmd.Flags().StringVar(&redisVersion, "redis-version", "7", "Redis version for TFE background jobs")
	deployCmd.Flags().StringVar(&minioVersion, "minio-version", "latest", "MinIO image tag for TFE object storage")
	deployCmd.Flags().IntVar(&minioAPIPort, "minio-api-port", 19000, "Host port mapped to MinIO S3 API container port 9000")
	deployCmd.Flags().IntVar(&minioConsolePort, "minio-console-port", 19001, "Host port mapped to MinIO console container port 9001")
	deployCmd.Flags().StringVar(&tfeProxyNginxTag, "proxy-nginx-version", "alpine", "Nginx image tag for the TFE ingress proxy")
	deployCmd.Flags().StringVarP(&tfePassword, "password", "p", "hal-secret-encryption-password", "TFE Encryption Password")
	deployCmd.Flags().StringVar(&deployTFEOrg, "tfe-org", "hal", "Terraform Enterprise organization name to auto-bootstrap during deploy")
	deployCmd.Flags().StringVar(&deployTFEProject, "tfe-project", "Dave", "Terraform Enterprise project name to auto-bootstrap during deploy")
	deployCmd.Flags().StringVar(&deployTFEAdminUser, "tfe-admin-username", "haladmin", "Initial TFE admin username used when bootstrapping via IACT")
	deployCmd.Flags().StringVar(&deployTFEAdminEmail, "tfe-admin-email", "haladmin@localhost", "Initial TFE admin email used when bootstrapping via IACT")
	deployCmd.Flags().StringVar(&deployTFEAdminPass, "tfe-admin-password", "hal9000FTW", "Initial TFE admin password used when bootstrapping via IACT")
	deployCmd.Flags().BoolVarP(&tfeForce, "force", "f", false, "Force redeploy")
	deployCmd.Flags().BoolVar(&tfeConfigureObs, "configure-obs", false, "Refresh Prometheus target and Grafana dashboard artifacts without redeploying Terraform Enterprise")
	Cmd.AddCommand(deployCmd)
}
