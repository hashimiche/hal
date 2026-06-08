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
	tfeImage            string
	tfePassword         string
	pgVersion           string
	pgImage             string
	redisVersion        string
	redisImage          string
	minioVersion        string
	minioImage          string
	minioAPIPort        int
	minioConsolePort    int
	tfeProxyNginxTag    string
	tfeProxyImage       string
	tfeUpdate           bool
	deployTFEOrg        string
	deployTFEProject    string
	deployTFEAdminUser  string
	deployTFEAdminEmail string
	deployTFEAdminPass  string
)

var deployCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a local Terraform Enterprise 1.x (FDO) instance via Docker",
	Run: func(cmd *cobra.Command, args []string) {
		target, err := normalizeTFETarget(tfeLifecycleTarget)
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		if target == tfeTargetTwin {
			runTFETwinLifecycle(true, false, tfeUpdate)
			return
		}

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		// 1. STRICT LICENSE ENFORCEMENT
		license := os.Getenv("TFE_LICENSE")
		licensePath := os.Getenv("TFE_LICENSE_PATH")
		if license == "" && licensePath != "" {
			licenseBytes, err := os.ReadFile(licensePath)
			if err != nil {
				fmt.Printf("❌ Error: Failed to read license from TFE_LICENSE_PATH: %v\n", err)
				return
			}
			license = strings.TrimSpace(string(licenseBytes))
		}
		if license == "" {
			fmt.Println("❌ Error: TFE requires a valid license to boot.")
			fmt.Println("   💡 Set one of:")
			fmt.Println("      export TFE_LICENSE='your_license_string'")
			fmt.Println("      export TFE_LICENSE_PATH='/path/to/terraform.hclic'")
			return
		}
		os.Setenv("TFE_LICENSE", license)

		os.Setenv("TFE_ENCRYPTION_PASSWORD", tfePassword)
		os.Setenv("TFE_DATABASE_PASSWORD", tfeDBPassword)

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
		tfeHostname := tfePrimaryHostname
		healthURL := tfePrimaryBaseURL + "/api/v1/health/readiness"
		uiURL := tfePrimaryBaseURL

		// 2. FORGE THE TLS CERTIFICATES
		fmt.Println("🔐 Forging local TLS certificates for TFE...")
		homeDir, _ := os.UserHomeDir()
		certDir := filepath.Join(homeDir, halStateDirName, tfeCertsDirName)

		if tfeUpdate {
			fmt.Println("♻️  Update requested. Reconciling existing TFE resources...")
			// 🎯 Included the proxy in the teardown list
			_ = exec.Command(engine, "rm", "-f", tfeCoreContainer, tfeProxyContainer, tfeDBContainer, tfeRedisContainer, tfeMinioContainer).Run()
			_ = os.Remove(filepath.Join(certDir, "cert.pem"))
			_ = os.Remove(filepath.Join(certDir, "key.pem"))
		}
		if err := ensureCerts(certDir); err != nil {
			fmt.Printf("❌ Failed to generate TLS certificates: %v\n", err)
			return
		}

		fmt.Printf("🚀 Deploying Terraform Enterprise %s (PG: %s, Redis: %s) via %s...\n", tfeVersion, pgVersion, redisVersion, engine)

		// 3. SECURE REGISTRY AUTHENTICATION (only for the default HashiCorp registry)
		if strings.Contains(tfeImage, "images.releases.hashicorp.com") {
			fmt.Println("🔑 Authenticating with HashiCorp private image registry...")
			loginCmd := exec.Command(engine, "login", "images.releases.hashicorp.com", "-u", "terraform", "--password-stdin")
			loginCmd.Stdin = strings.NewReader(license)
			if err := loginCmd.Run(); err != nil {
				fmt.Println("❌ Error: Failed to authenticate with images.releases.hashicorp.com.")
				return
			}
		}

		// 4. Ensure the global HAL network exists
		global.EnsureNetwork(engine)
		// Derive the proxy IP from the actual hal-net subnet so it works on any engine.
		proxyInternalIP := global.HalNetStaticIP(engine, tfePrimaryProxyHostNum)

		// 5. Deploy PostgreSQL
		fmt.Printf("⚙️  Provisioning TFE PostgreSQL Database...\n")
		_ = exec.Command(engine, "run", "-d", "--name", tfeDBContainer, "--network", global.HalNetName,
			"-v", tfeDBVolume+":/var/lib/postgresql/data",
			"-e", "POSTGRES_USER="+tfeDBUser, "-e", "POSTGRES_PASSWORD="+tfeDBPassword, "-e", "POSTGRES_DB="+tfeDBName,
			fmt.Sprintf("%s:%s", pgImage, pgVersion)).Run()

		// 6. Deploy Redis
		fmt.Printf("⚙️  Provisioning TFE Redis Cache...\n")
		_ = exec.Command(engine, "run", "-d", "--name", tfeRedisContainer, "--network", global.HalNetName,
			"-v", tfeRedisVolume+":/data",
			fmt.Sprintf("%s:%s", redisImage, redisVersion)).Run()

		// 7. Deploy MinIO (S3 Mock)
		fmt.Println("⚙️  Provisioning TFE Object Storage (MinIO)...")
		_ = exec.Command(engine, "run", "-d", "--name", tfeMinioContainer, "--network", global.HalNetName,
			"-p", fmt.Sprintf("%d:9000", minioAPIPort), "-p", fmt.Sprintf("%d:9001", minioConsolePort),
			"-v", tfeMinioVolume+":/data",
			"-e", "MINIO_ROOT_USER="+tfeMinioRootUser, "-e", "MINIO_ROOT_PASSWORD="+tfeMinioRootPass,
			fmt.Sprintf("%s:%s", minioImage, minioVersion), "server", "/data", "--console-address", ":9001").Run()

		time.Sleep(3 * time.Second)
		_ = exec.Command(engine, "exec", tfeMinioContainer, "sh", "-c", "mkdir -p /data/"+tfeS3Bucket).Run()

		// 8. Deploy TFE Core (NO EXPOSED HOST PORTS!)
		fmt.Println("⚙️  Booting TFE Core Application (This requires heavy compute)...")
		tfeArgs := []string{
			"run", "-d",
			"--name", tfeCoreContainer,
			"--network", global.HalNetName,
			"--privileged",
			"--add-host", tfeCoreContainer + ":127.0.0.1",
			"--add-host", fmt.Sprintf("%s:%s", tfePrimaryHostname, proxyInternalIP),
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
			"-e", fmt.Sprintf("TFE_VCS_HOSTNAME=%s:%d", tfePrimaryHostname, tfeHTTPSPort),
			"-e", "VAULT_ADDR=http://127.0.0.1:8200",
			"-e", "TFE_METRICS_ENABLE=true",
			"-e", fmt.Sprintf("TFE_METRICS_HTTP_PORT=%d", tfeMetricsHTTPPort),
			"-e", fmt.Sprintf("TFE_METRICS_HTTPS_PORT=%d", tfeMetricsHTTPSPort),
			"-e", fmt.Sprintf("TFE_IA_HOSTNAME=%s", tfeCoreContainer),
			"-e", "TFE_VAULT_DISABLE_MLOCK=true",
			"-e", "TFE_VAULT_ADDR=http://127.0.0.1:8200", // 🎯 Sorry Copilot!
			"-e", "TFE_IA_INTERNAL_VAULT_ADDR=http://127.0.0.1:8200", // 🎯 Sorry Copilot!
			"-e", "TFE_RUN_PIPELINE_DOCKER_NETWORK="+global.HalNetName,
			"-e", fmt.Sprintf("TFE_HTTP_PORT=%d", tfeHTTPPort),
			"-e", fmt.Sprintf("TFE_HTTPS_PORT=%d", tfeHTTPSPort),
			"-e", fmt.Sprintf("TFE_ADMIN_HTTPS_PORT=%d", tfeAdminHTTPSPort),
			"-e", "TFE_TLS_CERT_FILE=/etc/ssl/tfe/cert.pem",
			"-e", "TFE_TLS_KEY_FILE=/etc/ssl/tfe/key.pem",
			"-e", "TFE_DISK_CACHE_VOLUME_NAME="+tfeCacheVolume,
			"-e", "TFE_LICENSE",
			"-e", "TFE_ENCRYPTION_PASSWORD",
			"-e", "TFE_DATABASE_USER="+tfeDBUser,
			"-e", "TFE_DATABASE_PASSWORD",
			"-e", "TFE_DATABASE_HOST="+tfeDBContainer,
			"-e", "TFE_DATABASE_NAME="+tfeDBName,
			"-e", "TFE_DATABASE_PARAMETERS=sslmode=disable",
			"-e", "TFE_REDIS_HOST="+tfeRedisContainer,
			"-e", "TFE_REDIS_USE_TLS=false",
			"-e", "TFE_REDIS_USE_AUTH=false",
			"-e", "TFE_OBJECT_STORAGE_TYPE=s3",
			"-e", "TFE_OBJECT_STORAGE_S3_USE_INSTANCE_PROFILE=false",
			"-e", fmt.Sprintf("TFE_OBJECT_STORAGE_S3_ENDPOINT=http://%s:9000", tfeMinioContainer),
			"-e", "TFE_OBJECT_STORAGE_S3_BUCKET="+tfeS3Bucket,
			"-e", "TFE_OBJECT_STORAGE_S3_REGION="+tfeS3Region,
			"-e", "TFE_OBJECT_STORAGE_S3_ACCESS_KEY_ID="+tfeMinioRootUser,
			"-e", "TFE_OBJECT_STORAGE_S3_SECRET_ACCESS_KEY="+tfeMinioRootPass,
			"-e", "TFE_OBJECT_STORAGE_S3_FORCE_PATH_STYLE=true",
			"-e", "TFE_CAPACITY_CONCURRENCY=5",
			fmt.Sprintf("%s:%s", tfeImage, tfeVersion),
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
			tfeCoreContainer,
			"sh",
			"-lc",
			"cp /etc/ssl/tfe/cert.pem /usr/local/share/ca-certificates/tfe-localhost.crt && update-ca-certificates 2>&1",
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
			tfeCoreContainer,
			"sh",
			"-lc",
			"test -f /run/terraform-enterprise/task-worker/config.hcl && sed -i 's/readonly = \"true\"/readonly = \"false\"/' /run/terraform-enterprise/task-worker/config.hcl 2>&1 || true",
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

	server {
		listen 8444 ssl;
		server_name tfe.localhost;

		ssl_certificate /etc/ssl/tfe/cert.pem;
		ssl_certificate_key /etc/ssl/tfe/key.pem;

		location / {
			proxy_pass https://hal-tfe:8444;

			proxy_set_header Host tfe.localhost:8444;
			proxy_set_header X-Forwarded-Host tfe.localhost:8444;
			proxy_set_header X-Forwarded-Port 8444;
			proxy_set_header X-Real-IP $remote_addr;
			proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
			proxy_set_header X-Forwarded-Proto https;
			proxy_set_header Accept-Encoding "";

			proxy_ssl_verify off;
		}
	}
}`
		proxyConfPath := filepath.Join(homeDir, halStateDirName, tfeProxyConfName)
		_ = os.WriteFile(proxyConfPath, []byte(nginxConfig), 0644)

		_ = exec.Command(engine, "run", "-d", "--name", tfeProxyContainer, "--network", global.HalNetName, "--ip", proxyInternalIP,
			"--network-alias", tfePrimaryHostname,
			"-p", fmt.Sprintf("%d:%d", tfeHTTPSPort, tfeHTTPSPort), // 🎯 Only the proxy exposes port 8443 to the host OS
			"-p", fmt.Sprintf("%d:%d", tfeAdminHTTPSPort, tfeAdminHTTPSPort), // 🎯 Expose the TFE admin HTTPS port through the proxy
			"-v", fmt.Sprintf("%s:/etc/ssl/tfe:ro", certDir),
			"-v", fmt.Sprintf("%s:/etc/nginx/nginx.conf:ro", proxyConfPath),
			fmt.Sprintf("%s:%s", tfeProxyImage, tfeProxyNginxTag)).Run()

		// 9. THE HEALTH CHECK PHASE
		fmt.Println("⏳ Waiting for TFE to initialize (WARNING: This can take 3-5 minutes!)...")

		// This will naturally hit the Proxy, which routes to TFE
		if err := waitForService("TFE", healthURL, 60); err != nil {
			handleDockerFailure(tfeCoreContainer, engine)
			return
		}

		fmt.Printf("\n✅ Terraform Enterprise %s is UP!\n", tfeVersion)
		global.RefreshHalHealth(engine)
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
				fmt.Printf("   📄 Token cache: ~/%s/%s\n", halStateDirName, tfeAPITokenFileName)
			}
		}
		fmt.Println("⚠️  Note:        Accept the browser warning for the self-signed certificate.")
		fmt.Println("\n💡 Next Step:")
		fmt.Println("   Run 'hal terraform vcs-workflow enable' to bootstrap org/project/workspace wiring.")
		fmt.Println("---------------------------------------------------------")

		if target == tfeTargetBoth {
			fmt.Println("\n🔁 Target includes twin. Continuing with twin Terraform Enterprise deployment...")
			runTFETwinLifecycle(true, false, tfeUpdate)
		}
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
			CommonName:   tfePrimaryHostname,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost", tfeCoreContainer, tfePrimaryHostname},
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
		if name == tfePrimaryHostname {
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

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Reconcile an existing Terraform Enterprise deployment",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		tfeUpdate = true
		deployCmd.Run(cmd, args)
	},
}

func bindLifecycleFlags(cmd *cobra.Command, includeUpdate bool) {
	cmd.Flags().StringVarP(&tfeVersion, "tfe-tag", "v", defaultTFETag, "TFE container image tag")
	cmd.Flags().StringVar(&tfeImage, "tfe-image", defaultTFEImage, "TFE container image name")
	cmd.Flags().StringVar(&pgVersion, "tfe-pg-tag", defaultTFEPGTag, "PostgreSQL image tag for TFE backend")
	cmd.Flags().StringVar(&pgImage, "tfe-pg-image", defaultTFEPGImage, "PostgreSQL image name for TFE backend")
	cmd.Flags().StringVar(&redisVersion, "tfe-redis-tag", defaultTFERedisTag, "Redis image tag for TFE background jobs")
	cmd.Flags().StringVar(&redisImage, "tfe-redis-image", defaultTFERedisImage, "Redis image name for TFE background jobs")
	cmd.Flags().StringVar(&minioVersion, "tfe-minio-tag", defaultTFEMinioTag, "MinIO image tag for TFE object storage")
	cmd.Flags().StringVar(&minioImage, "tfe-minio-image", defaultTFEMinioImage, "MinIO image name for TFE object storage")
	cmd.Flags().IntVar(&minioAPIPort, "minio-api-port", defaultMinioAPIHostPort, "Host port mapped to MinIO S3 API container port 9000")
	cmd.Flags().IntVar(&minioConsolePort, "minio-console-port", defaultMinioConsoleHostPort, "Host port mapped to MinIO console container port 9001")
	cmd.Flags().StringVar(&tfeProxyNginxTag, "tfe-proxy-tag", defaultTFEProxyTag, "Nginx image tag for the TFE ingress proxy")
	cmd.Flags().StringVar(&tfeProxyImage, "tfe-proxy-image", defaultTFEProxyImage, "Nginx image name for the TFE ingress proxy")
	cmd.Flags().StringVarP(&tfePassword, "password", "p", defaultTFEEncryptionPassword, "TFE Encryption Password")
	cmd.Flags().StringVar(&deployTFEOrg, "tfe-org", defaultTFEOrg, "Terraform Enterprise organization name to auto-bootstrap during deploy")
	cmd.Flags().StringVar(&deployTFEProject, "tfe-project", defaultTFEProject, "Terraform Enterprise project name to auto-bootstrap during deploy")
	cmd.Flags().StringVar(&deployTFEAdminUser, "tfe-admin-username", defaultTFEAdminUsername, "Initial TFE admin username used when bootstrapping via IACT")
	cmd.Flags().StringVar(&deployTFEAdminEmail, "tfe-admin-email", defaultTFEAdminEmail, "Initial TFE admin email used when bootstrapping via IACT")
	cmd.Flags().StringVar(&deployTFEAdminPass, "tfe-admin-password", defaultTFEAdminPassword, "Initial TFE admin password used when bootstrapping via IACT")
	if includeUpdate {
		cmd.Flags().BoolVarP(&tfeUpdate, "update", "u", false, "Reconcile an existing Terraform Enterprise deployment in place")
	}
}

func init() {
	bindLifecycleFlags(deployCmd, true)
	bindLifecycleFlags(updateCmd, false)
	bindTFETargetFlag(deployCmd)
	bindTFETargetFlag(updateCmd)
	bindTwinFlags(deployCmd)
	bindTwinFlags(updateCmd)
	Cmd.AddCommand(deployCmd)
	Cmd.AddCommand(updateCmd)
}
