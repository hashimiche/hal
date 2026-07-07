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
	"hal/internal/ui"

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
		ui.LogoStart("terraform")
		defer ui.LogoStop()
		// Non-fatal warnings raised mid-deploy are collected and printed after the
		// animation stops, so they are not eaten by the logo redraw.
		var warnings []string
		// step updates the activity caption and nudges the logo fill by a fixed
		// number of columns, so each quick step maps to 1–2 columns. The long boot
		// wait below uses ui.LogoCreep so it keeps inching instead of stalling.
		step := func(cols int, format string, args ...any) {
			ui.LogoStep(format, args...)
			ui.LogoAdvance(cols)
		}

		step(1, "Forging local TLS certificates")
		homeDir, _ := os.UserHomeDir()
		certDir := filepath.Join(homeDir, halStateDirName, tfeCertsDirName)

		if tfeUpdate {
			step(1, "Reconciling existing TFE resources (update)")
			// 🎯 Included the proxy in the teardown list
			_ = exec.Command(engine, "rm", "-f", tfeCoreContainer, tfeProxyContainer, tfeDBContainer, tfeRedisContainer, tfeMinioContainer).Run()
			_ = os.Remove(filepath.Join(certDir, "cert.pem"))
			_ = os.Remove(filepath.Join(certDir, "key.pem"))
		}
		if err := ensureCerts(certDir); err != nil {
			ui.Fail("Failed to generate TLS certificates: %v", err)
			return
		}

		step(1, "Deploying Terraform Enterprise %s (PG %s, Redis %s)", tfeVersion, pgVersion, redisVersion)

		// 3. SECURE REGISTRY AUTHENTICATION (only for the default HashiCorp registry)
		if strings.Contains(tfeImage, "images.releases.hashicorp.com") {
			step(1, "Authenticating with HashiCorp image registry")
			loginCmd := exec.Command(engine, "login", "images.releases.hashicorp.com", "-u", "terraform", "--password-stdin")
			loginCmd.Stdin = strings.NewReader(license)
			if err := loginCmd.Run(); err != nil {
				ui.Fail("Failed to authenticate with images.releases.hashicorp.com")
				return
			}
		}

		// 4. Ensure the global HAL network exists
		global.EnsureNetwork(engine)
		// Derive the proxy IP from the actual hal-net subnet so it works on any engine.
		proxyInternalIP := global.HalNetStaticIP(engine, tfePrimaryProxyHostNum)

		// 5. Deploy PostgreSQL
		step(1, "Provisioning PostgreSQL database")
		_ = exec.Command(engine, "run", "-d", "--name", tfeDBContainer, "--network", global.HalNetName,
			"-v", tfeDBVolume+":/var/lib/postgresql/data",
			"-e", "POSTGRES_USER="+tfeDBUser, "-e", "POSTGRES_PASSWORD="+tfeDBPassword, "-e", "POSTGRES_DB="+tfeDBName,
			fmt.Sprintf("%s:%s", pgImage, pgVersion)).Run()

		// 6. Deploy Redis
		step(1, "Provisioning Redis cache")
		_ = exec.Command(engine, "run", "-d", "--name", tfeRedisContainer, "--network", global.HalNetName,
			"-v", tfeRedisVolume+":/data",
			fmt.Sprintf("%s:%s", redisImage, redisVersion)).Run()

		// 7. Deploy MinIO (S3 Mock)
		step(1, "Provisioning object storage (MinIO)")
		_ = exec.Command(engine, "run", "-d", "--name", tfeMinioContainer, "--network", global.HalNetName,
			"-p", fmt.Sprintf("%d:9000", minioAPIPort), "-p", fmt.Sprintf("%d:9001", minioConsolePort),
			"-v", tfeMinioVolume+":/data",
			"-e", "MINIO_ROOT_USER="+tfeMinioRootUser, "-e", "MINIO_ROOT_PASSWORD="+tfeMinioRootPass,
			fmt.Sprintf("%s:%s", minioImage, minioVersion), "server", "/data", "--console-address", ":9001").Run()

		time.Sleep(3 * time.Second)
		_ = exec.Command(engine, "exec", tfeMinioContainer, "sh", "-c", "mkdir -p /data/"+tfeS3Bucket).Run()

		// 8. Deploy TFE Core (NO EXPOSED HOST PORTS!)
		step(2, "Booting TFE core application (heavy compute)")
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
			// NOTE: the VCS commit-status run link TFE posts to GitLab is built from the
			// primary TFE_HOSTNAME, which is intentionally portless (tfe.localhost) so the
			// rest of the deployment works behind hal-tfe-proxy. That link therefore comes
			// out as https://tfe.localhost/... (no :8443) and is not host-reachable. This
			// cannot be fixed via a secondary hostname: TFE_VCS_HOSTNAME_CHOICE=secondary
			// only governs VCS workload-identity federation, not the human-facing run link
			// (verified experimentally — TFE accepted TFE_HOSTNAME_SECONDARY=host:8443 but
			// the commit-status link stayed portless). The proxy can't rewrite it either
			// (outbound TFE->GitLab, not served through the proxy). Accepted limitation.
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
			ui.Fail("Failed to start TFE: %s", string(out))
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
			warnings = append(warnings, fmt.Sprintf("⚠️  Could not refresh TFE trust store automatically: %s", strings.TrimSpace(string(trustOut))))
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
			warnings = append(warnings, fmt.Sprintf("⚠️  Could not patch TFE task-worker cache mount automatically: %s", strings.TrimSpace(string(taskWorkerOut))))
		}

		// 8.5 Deploy the Magic Redirect Fixer (AFTER TFE BOOTS!)
		step(1, "Deploying ingress proxy (redirect fixer)")
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
		ui.LogoStep("Waiting for TFE to initialize (this can take a few minutes)")
		// Long step: creep one column every few seconds so the logo keeps moving
		// instead of stalling. Tuned so a typical ~1 min boot shows clear motion;
		// slower boots fill toward the cap and then hold until readiness.
		ui.LogoCreep(6 * time.Second)

		// This will naturally hit the Proxy, which routes to TFE
		if err := waitForService("TFE", healthURL, 60); err != nil {
			ui.LogoStop()
			handleDockerFailure(tfeCoreContainer, engine)
			return
		}

		step(1, "Bootstrapping TFE foundation (org, project, admin token)")
		token, _, err := ensureTFEFoundation(engine, tfeFoundationConfig{
			BaseURL:       uiURL,
			OrgName:       deployTFEOrg,
			ProjectName:   deployTFEProject,
			AdminUsername: deployTFEAdminUser,
			AdminEmail:    deployTFEAdminEmail,
			AdminPassword: deployTFEAdminPass,
		})

		ui.LogoStop()
		global.RefreshHalHealth(engine)
		for _, w := range warnings {
			fmt.Println(w)
		}

		ui.Success("Terraform Enterprise %s is UP!", tfeVersion)
		ui.Section("Endpoints")
		ui.Field("UI", uiURL)
		ui.Field("MinIO API", fmt.Sprintf("http://127.0.0.1:%d", minioAPIPort))
		ui.Field("MinIO UI", fmt.Sprintf("http://127.0.0.1:%d", minioConsolePort))
		ui.Section("Admin")
		ui.Field("User", deployTFEAdminUser)
		ui.Field("Password", deployTFEAdminPass)

		if err != nil {
			ui.Section("Foundation")
			ui.Item("⚠️  Bootstrap incomplete: %v", err)
			ui.Item("HAL could not mint a usable API token automatically from this TFE instance.")
		} else {
			ui.Section("Foundation")
			ui.Item("✅ Admin token + org/project configured")
			if token != "" {
				ui.Item("Token cache: ~/%s/%s", halStateDirName, tfeAPITokenFileName)
			}
		}

		ui.Item("⚠️  Accept the browser warning for the self-signed certificate.")
		ui.Hint("Next: hal terraform vcs-workflow enable  (org/project/workspace wiring)")

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
