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
	"regexp"
	"strings"
	"time"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var (
	tfeTwinEnable              bool
	tfeTwinDisable             bool
	tfeTwinUpdate              bool
	tfeTwinVersion             string
	tfeTwinPassword            string
	tfeTwinOrg                 string
	tfeTwinProject             string
	tfeTwinAdminUser           string
	tfeTwinAdminEmail          string
	tfeTwinAdminPass           string
	tfeTwinProxyNginxTag       string
	tfeTwinHTTPSPort           int
	tfeTwinHostname            string
	tfeTwinContainerName       string
	tfeTwinProxyInternalIP     string
	tfeTwinConfigureObsOnly    bool
	tfeTwinDatabasePassword    string
	tfeTwinDatabaseName        string
	tfeTwinMinioRootUser       string
	tfeTwinMinioRootPassword   string
	tfeTwinObjectStorageBucket string
)

type tfeTwinLayout struct {
	CoreContainer  string
	ProxyContainer string
	CertDir        string
	ProxyConfPath  string
	UIURL          string
	HealthURL      string
}

var twinCmd = &cobra.Command{
	Use:     "twin [status|enable|disable|update]",
	Aliases: []string{"bis", "dup"},
	Short:   "Manage a second local Terraform Enterprise instance that runs alongside the primary deployment",
	Run: func(cmd *cobra.Command, args []string) {
		if err := parseLifecycleAction(args, &tfeTwinEnable, &tfeTwinDisable, &tfeTwinUpdate); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		layout, err := buildTFETwinLayout()
		if err != nil {
			fmt.Printf("❌ Invalid twin configuration: %v\n", err)
			return
		}

		if tfeTwinDisable {
			if tfeTwinEnable || tfeTwinUpdate || tfeTwinConfigureObsOnly {
				fmt.Println("❌ '--disable' cannot be combined with '--enable', '--update', or '--configure-obs'.")
				return
			}
			destroyTFETwin(engine, layout)
			return
		}

		if tfeTwinConfigureObsOnly {
			if tfeTwinEnable || tfeTwinUpdate {
				fmt.Println("❌ '--configure-obs' cannot be combined with '--enable' or '--update'.")
				return
			}
			if !global.IsContainerRunning(engine, layout.CoreContainer) {
				fmt.Printf("❌ Twin Terraform Enterprise instance '%s' is not running.\n", layout.CoreContainer)
				fmt.Println("   💡 Run 'hal tf create --target twin' first.")
				return
			}
			if err := global.UpsertObsPromTargetIfRunning(engine, "terraform-bis", []string{layout.CoreContainer + ":9090"}); err != nil {
				fmt.Printf("⚠️  Could not refresh twin Prometheus target: %v\n", err)
				return
			}
			fmt.Println("✅ Twin Terraform Enterprise Prometheus target refreshed.")
			return
		}

		if tfeTwinUpdate {
			tfeTwinEnable = true
		}

		if !tfeTwinEnable {
			showTFETwinStatus(engine, layout)
			return
		}

		if !global.IsContainerRunning(engine, "hal-tfe") {
			fmt.Println("❌ Primary Terraform Enterprise instance is not running (hal-tfe).")
			fmt.Println("   💡 Run 'hal tf create' first, then retry 'hal tf create --target twin'.")
			return
		}

		if err := ensureSharedTFEEcosystemRunning(engine); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		license := os.Getenv("TFE_LICENSE")
		if license == "" {
			fmt.Println("❌ Error: TFE requires a valid license to boot.")
			fmt.Println("   💡 Export your license to your environment before running this command:")
			fmt.Println("      export TFE_LICENSE='your_massive_ibm_hashicorp_license_string'")
			return
		}

		os.Setenv("TFE_ENCRYPTION_PASSWORD", tfeTwinPassword)
		os.Setenv("TFE_DATABASE_PASSWORD", tfeTwinDatabasePassword)

		global.WarnIfEngineResourcesTight(engine, "terraform-twin-deploy")
		if !global.DryRun {
			proceed, err := global.ConfirmScenarioProceed(engine, "terraform-deploy")
			if err != nil && global.Debug {
				fmt.Printf("[DEBUG] Capacity confirmation unavailable: %v\n", err)
			}
			if err == nil && !proceed {
				fmt.Printf("🛑 Twin Terraform Enterprise deployment aborted to protect your %s engine.\n", engine)
				return
			}
		}

		isPodman := strings.Contains(engine, "podman")

		if tfeTwinUpdate {
			fmt.Println("♻️  Update requested. Purging existing twin TFE resources...")
			destroyTFETwin(engine, layout)
		}

		fmt.Println("🔐 Forging local TLS certificates for twin TFE...")
		if err := ensureCertsForTwin(layout.CertDir, []string{"localhost", layout.CoreContainer, tfeTwinHostname}); err != nil {
			fmt.Printf("❌ Failed to generate TLS certificates: %v\n", err)
			return
		}

		fmt.Printf("🚀 Deploying twin Terraform Enterprise %s using shared PG/Redis/MinIO via %s...\n", tfeTwinVersion, engine)

		fmt.Println("🔑 Authenticating with HashiCorp private image registry...")
		loginCmd := exec.Command(engine, "login", "images.releases.hashicorp.com", "-u", "terraform", "--password-stdin")
		loginCmd.Stdin = strings.NewReader(license)
		if err := loginCmd.Run(); err != nil {
			fmt.Println("❌ Error: Failed to authenticate with images.releases.hashicorp.com.")
			return
		}

		global.EnsureNetwork(engine)

		fmt.Printf("⚙️  Ensuring shared PostgreSQL has twin database '%s'...\n", tfeTwinDatabaseName)
		if err := ensureTwinDatabaseExists(engine, tfeTwinDatabaseName); err != nil {
			fmt.Printf("❌ Failed to ensure twin database exists: %v\n", err)
			return
		}

		fmt.Printf("⚙️  Ensuring shared MinIO has twin bucket '%s'...\n", tfeTwinObjectStorageBucket)
		if err := ensureTwinBucketExists(engine, tfeTwinObjectStorageBucket); err != nil {
			fmt.Printf("❌ Failed to ensure twin object bucket exists: %v\n", err)
			return
		}

		fmt.Println("⚙️  Booting twin TFE Core Application (This requires heavy compute)...")
		tfeArgs := []string{
			"run", "-d",
			"--name", layout.CoreContainer,
			"--network", "hal-net",
			"--privileged",
			"--add-host", fmt.Sprintf("%s:127.0.0.1", layout.CoreContainer),
			"--add-host", fmt.Sprintf("%s:%s", tfeTwinHostname, tfeTwinProxyInternalIP),
			"-v", "/var/run/docker.sock:/var/run/docker.sock",
		}

		if isPodman {
			tfeArgs = append(tfeArgs, "--security-opt", "label=disable")
			tfeArgs = append(tfeArgs, "--security-opt", "seccomp=unconfined")
		}

		tfeArgs = append(tfeArgs, "-v", fmt.Sprintf("%s:/etc/ssl/tfe:Z", layout.CertDir))

		tfeArgs = append(tfeArgs,
			"-e", "TFE_OPERATIONAL_MODE=external",
			"-e", fmt.Sprintf("TFE_HOSTNAME=%s", tfeTwinHostname),
			"-e", fmt.Sprintf("TFE_VCS_HOSTNAME=%s:%d", tfeTwinHostname, tfeTwinHTTPSPort),
			"-e", "VAULT_ADDR=http://127.0.0.1:8200",
			"-e", "TFE_METRICS_ENABLE=true",
			"-e", "TFE_METRICS_HTTP_PORT=9090",
			"-e", "TFE_METRICS_HTTPS_PORT=9091",
			"-e", fmt.Sprintf("TFE_IA_HOSTNAME=%s", layout.CoreContainer),
			"-e", "TFE_VAULT_DISABLE_MLOCK=true",
			"-e", "TFE_VAULT_ADDR=http://127.0.0.1:8200",
			"-e", "TFE_IA_INTERNAL_VAULT_ADDR=http://127.0.0.1:8200",
			"-e", "TFE_RUN_PIPELINE_DOCKER_NETWORK=hal-net",
			"-e", "TFE_HTTP_PORT=8080",
			"-e", "TFE_HTTPS_PORT=8443",
			"-e", "TFE_TLS_CERT_FILE=/etc/ssl/tfe/cert.pem",
			"-e", "TFE_TLS_KEY_FILE=/etc/ssl/tfe/key.pem",
			"-e", fmt.Sprintf("TFE_DISK_CACHE_VOLUME_NAME=%s-cache", layout.CoreContainer),
			"-e", "TFE_LICENSE",
			"-e", "TFE_ENCRYPTION_PASSWORD",
			"-e", "TFE_DATABASE_USER=tfe",
			"-e", "TFE_DATABASE_PASSWORD",
			"-e", "TFE_DATABASE_HOST=hal-tfe-db",
			"-e", fmt.Sprintf("TFE_DATABASE_NAME=%s", tfeTwinDatabaseName),
			"-e", "TFE_DATABASE_PARAMETERS=sslmode=disable",
			"-e", "TFE_REDIS_HOST=hal-tfe-redis",
			"-e", "TFE_REDIS_USE_TLS=false",
			"-e", "TFE_REDIS_USE_AUTH=false",
			"-e", "TFE_OBJECT_STORAGE_TYPE=s3",
			"-e", "TFE_OBJECT_STORAGE_S3_USE_INSTANCE_PROFILE=false",
			"-e", "TFE_OBJECT_STORAGE_S3_ENDPOINT=http://hal-tfe-minio:9000",
			"-e", fmt.Sprintf("TFE_OBJECT_STORAGE_S3_BUCKET=%s", tfeTwinObjectStorageBucket),
			"-e", "TFE_OBJECT_STORAGE_S3_REGION=us-east-1",
			"-e", fmt.Sprintf("TFE_OBJECT_STORAGE_S3_ACCESS_KEY_ID=%s", tfeTwinMinioRootUser),
			"-e", fmt.Sprintf("TFE_OBJECT_STORAGE_S3_SECRET_ACCESS_KEY=%s", tfeTwinMinioRootPassword),
			"-e", "TFE_OBJECT_STORAGE_S3_FORCE_PATH_STYLE=true",
			"-e", "TFE_CAPACITY_CONCURRENCY=5",
			fmt.Sprintf("images.releases.hashicorp.com/hashicorp/terraform-enterprise:%s", tfeTwinVersion),
		)

		out, err := exec.Command(engine, tfeArgs...).CombinedOutput()
		if err != nil {
			fmt.Printf("❌ Failed to start twin TFE: %s\n", string(out))
			return
		}

		if trustOut, trustErr := exec.Command(
			engine,
			"exec",
			"--user",
			"0",
			layout.CoreContainer,
			"sh",
			"-lc",
			"cp /etc/ssl/tfe/cert.pem /usr/local/share/ca-certificates/tfe-twin-localhost.crt && update-ca-certificates >/dev/null 2>&1 && supervisorctl restart tfe:archivist >/dev/null 2>&1",
		).CombinedOutput(); trustErr != nil {
			fmt.Printf("⚠️  Could not refresh twin TFE trust store automatically: %s\n", strings.TrimSpace(string(trustOut)))
		}

		if taskWorkerOut, taskWorkerErr := exec.Command(
			engine,
			"exec",
			"--user",
			"0",
			layout.CoreContainer,
			"sh",
			"-lc",
			"sed -i 's/readonly = \"true\"/readonly = \"false\"/' /run/terraform-enterprise/task-worker/config.hcl && supervisorctl restart tfe:task-worker >/dev/null 2>&1",
		).CombinedOutput(); taskWorkerErr != nil {
			fmt.Printf("⚠️  Could not patch twin TFE task-worker cache mount automatically: %s\n", strings.TrimSpace(string(taskWorkerOut)))
		}

		fmt.Println("⚙️  Deploying twin TFE Ingress Proxy...")
		nginxConfig := fmt.Sprintf(`events {}
http {
	server {
		listen 443 ssl;
		listen %d ssl;
		server_name %s;

		ssl_certificate /etc/ssl/tfe/cert.pem;
		ssl_certificate_key /etc/ssl/tfe/key.pem;

		location / {
			proxy_pass https://%s:8443;

			proxy_set_header Host %s:%d;
			proxy_set_header X-Forwarded-Host %s:%d;
			proxy_set_header X-Forwarded-Port %d;
			proxy_set_header X-Real-IP $remote_addr;
			proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
			proxy_set_header X-Forwarded-Proto https;
			proxy_set_header Accept-Encoding "";

			proxy_ssl_verify off;

			sub_filter_once off;
			sub_filter_types application/json application/vnd.api+json text/html text/plain;
			sub_filter 'https://%s/_archivist/' 'https://%s:%d/_archivist/';

			proxy_redirect ~^https://%s(?::443)?(/.*)$ https://%s:%d$1;
			proxy_redirect ~^https://%s(?::8443)?(/.*)$ https://%s:%d$1;
		}
	}
}
`, tfeTwinHTTPSPort, tfeTwinHostname, layout.CoreContainer, tfeTwinHostname, tfeTwinHTTPSPort, tfeTwinHostname, tfeTwinHTTPSPort, tfeTwinHTTPSPort, tfeTwinHostname, tfeTwinHostname, tfeTwinHTTPSPort, tfeTwinHostname, tfeTwinHostname, tfeTwinHTTPSPort, layout.CoreContainer, tfeTwinHostname, tfeTwinHTTPSPort)

		if err := os.WriteFile(layout.ProxyConfPath, []byte(nginxConfig), 0o644); err != nil {
			fmt.Printf("❌ Failed to write twin proxy configuration: %v\n", err)
			return
		}

		_ = exec.Command(engine, "run", "-d", "--name", layout.ProxyContainer, "--network", "hal-net", "--ip", tfeTwinProxyInternalIP,
			"--network-alias", tfeTwinHostname,
			"-p", fmt.Sprintf("%d:%d", tfeTwinHTTPSPort, tfeTwinHTTPSPort),
			"-v", fmt.Sprintf("%s:/etc/ssl/tfe:ro", layout.CertDir),
			"-v", fmt.Sprintf("%s:/etc/nginx/nginx.conf:ro", layout.ProxyConfPath),
			fmt.Sprintf("nginx:%s", tfeTwinProxyNginxTag)).Run()

		fmt.Println("⏳ Waiting for twin TFE to initialize (WARNING: This can take 3-5 minutes!)...")
		if err := waitForTwinService(layout.HealthURL, 60); err != nil {
			handleDockerFailure(layout.CoreContainer, engine)
			return
		}

		if err := global.UpsertObsPromTargetIfRunning(engine, "terraform-bis", []string{layout.CoreContainer + ":9090"}); err != nil {
			fmt.Printf("⚠️  Could not register twin Prometheus target: %v\n", err)
		}

		token, err := ensureTFETwinFoundation(engine, layout)
		if err != nil {
			fmt.Printf("⚠️  Twin TFE foundation bootstrap incomplete: %v\n", err)
			fmt.Println("   💡 HAL could not mint a usable API token automatically from this twin TFE instance.")
		} else {
			fmt.Println("✅ Twin TFE foundation ready: admin token + org/project are configured.")
			if token != "" {
				fmt.Println("   💡 Token is valid for this twin base URL during this session.")
			}
		}

		fmt.Println("\n✅ Terraform Enterprise twin instance is UP!")
		fmt.Println("---------------------------------------------------------")
		fmt.Printf("🔗 UI Address:   %s\n", layout.UIURL)
		fmt.Printf("🗄️  Shared DB:    hal-tfe-db/%s\n", tfeTwinDatabaseName)
		fmt.Printf("🪣 Shared Bucket: %s (on hal-tfe-minio)\n", tfeTwinObjectStorageBucket)
		fmt.Printf("👤 Admin User:   %s\n", tfeTwinAdminUser)
		fmt.Printf("🔑 Admin Pass:   %s\n", tfeTwinAdminPass)
		fmt.Println("⚠️  Note:         Accept the browser warning for the self-signed certificate.")
		fmt.Println("💡 Dashboard import is intentionally skipped for the twin instance.")
		fmt.Println("---------------------------------------------------------")
	},
}

func ensureTFETwinFoundation(engine string, layout tfeTwinLayout) (string, error) {
	baseURL := strings.TrimSpace(layout.UIURL)
	token := strings.TrimSpace(os.Getenv("TFE_TWIN_API_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("TFE_API_TOKEN"))
	}

	if token != "" && !isTFEAPITokenUsable(baseURL, token) {
		token = ""
	}

	if token == "" {
		// Best-effort warmup to reduce startup races without blocking the CLI for minutes.
		_ = waitForTFECoreReadiness(engine, 30*time.Second)

		autoToken, err := bootstrapTFEAPIToken(engine, baseURL, tfeTwinAdminUser, tfeTwinAdminEmail, tfeTwinAdminPass)
		if err != nil {
			return "", err
		}
		token = autoToken
	}

	if _, err := ensureTFEOrgAndProject(baseURL, token, tfeTwinOrg, tfeTwinProject); err != nil {
		return "", err
	}

	return token, nil
}

func buildTFETwinLayout() (tfeTwinLayout, error) {
	trimmedCore := strings.TrimSpace(tfeTwinContainerName)
	if trimmedCore == "" {
		return tfeTwinLayout{}, fmt.Errorf("container name cannot be empty")
	}
	if strings.Contains(trimmedCore, " ") {
		return tfeTwinLayout{}, fmt.Errorf("container name cannot contain spaces")
	}

	hostname := strings.TrimSpace(tfeTwinHostname)
	if hostname == "" {
		return tfeTwinLayout{}, fmt.Errorf("hostname cannot be empty")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return tfeTwinLayout{}, err
	}

	return tfeTwinLayout{
		CoreContainer:  trimmedCore,
		ProxyContainer: trimmedCore + "-proxy",
		CertDir:        filepath.Join(homeDir, ".hal", trimmedCore+"-certs"),
		ProxyConfPath:  filepath.Join(homeDir, ".hal", trimmedCore+"-proxy.conf"),
		UIURL:          fmt.Sprintf("https://%s:%d", hostname, tfeTwinHTTPSPort),
		HealthURL:      fmt.Sprintf("https://%s:%d/_health_check", hostname, tfeTwinHTTPSPort),
	}, nil
}

func showTFETwinStatus(engine string, layout tfeTwinLayout) {
	fmt.Println("🔍 Checking Terraform Enterprise Twin Status...")

	components := []struct {
		Name      string
		Container string
	}{
		{"Shared Database (Postgres)", "hal-tfe-db"},
		{"Shared Cache (Redis)", "hal-tfe-redis"},
		{"Shared Object Storage (MinIO)", "hal-tfe-minio"},
		{"Twin TFE Core", layout.CoreContainer},
		{"Twin Ingress Proxy", layout.ProxyContainer},
	}

	anyRunning := false
	for _, c := range components {
		out, err := exec.Command(engine, "inspect", "-f", "{{.State.Status}}", c.Container).CombinedOutput()
		status := strings.TrimSpace(string(out))
		if err != nil || strings.Contains(status, "No such object") || strings.Contains(status, "no such container") {
			fmt.Printf("  ⚪ %-27s : Not running\n", c.Name)
			continue
		}
		if status == "running" {
			anyRunning = true
			fmt.Printf("  🟢 %-27s : Active (%s)\n", c.Name, c.Container)
		} else {
			fmt.Printf("  🟡 %-27s : %s\n", c.Name, strings.ToUpper(status))
		}
	}

	fmt.Println("\n💡 Tips:")
	if !anyRunning {
		fmt.Println("   Run 'hal tf create' first, then 'hal tf create --target twin'.")
	} else {
		fmt.Printf("   🔗 UI Address: %s\n", layout.UIURL)
		fmt.Println("   Twin reuses hal-tfe-db, hal-tfe-redis, and hal-tfe-minio.")
		fmt.Println("   To remove twin resources, run: hal tf delete --target twin")
	}
}

func destroyTFETwin(engine string, layout tfeTwinLayout) {
	fmt.Printf("⚙️  Destroying Terraform Enterprise twin resources via %s...\n", engine)

	containers := []string{
		layout.ProxyContainer,
		layout.CoreContainer,
	}

	for _, container := range containers {
		if global.DryRun {
			fmt.Printf("[DRY RUN] Would execute: %s rm -f %s\n", engine, container)
			continue
		}
		out, err := exec.Command(engine, "rm", "-f", container).CombinedOutput()
		if err != nil {
			outputStr := strings.ToLower(strings.TrimSpace(string(out)))
			if !strings.Contains(outputStr, "no such container") && !strings.Contains(outputStr, "no container") {
				fmt.Printf("⚠️  Failed to destroy '%s': %s\n", container, strings.TrimSpace(string(out)))
			}
			continue
		}
		if strings.TrimSpace(string(out)) == container {
			fmt.Printf("  ✅ Destroyed container: %s\n", container)
		}
	}

	if !global.DryRun {
		_ = os.RemoveAll(layout.CertDir)
		_ = os.Remove(layout.ProxyConfPath)
	}

	if err := global.RemoveObsPromTargetFile("terraform-bis"); err != nil {
		fmt.Printf("⚠️  Could not remove twin Terraform observability target file: %v\n", err)
	}

	if !global.DryRun {
		fmt.Println("✅ Twin Terraform Enterprise resources removed.")
		fmt.Printf("ℹ️  Shared resources are preserved: hal-tfe-db (%s), hal-tfe-redis, hal-tfe-minio (%s).\n", tfeTwinDatabaseName, tfeTwinObjectStorageBucket)
	}
}

func ensureSharedTFEEcosystemRunning(engine string) error {
	required := []string{"hal-tfe-db", "hal-tfe-redis", "hal-tfe-minio"}
	for _, container := range required {
		if !global.IsContainerRunning(engine, container) {
			return fmt.Errorf("required shared component '%s' is not running; run 'hal tf create' first", container)
		}
	}
	return nil
}

func ensureTwinDatabaseExists(engine, dbName string) error {
	validName := regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
	if !validName.MatchString(dbName) {
		return fmt.Errorf("invalid database name '%s': allowed pattern is [a-zA-Z0-9_]+", dbName)
	}

	checkSQL := fmt.Sprintf("SELECT 1 FROM pg_database WHERE datname='%s';", dbName)
	out, err := exec.Command(engine, "exec", "hal-tfe-db", "psql", "-U", "tfe", "-d", "postgres", "-tAc", checkSQL).CombinedOutput()
	if err != nil {
		return fmt.Errorf("postgres check failed: %s", strings.TrimSpace(string(out)))
	}
	if strings.TrimSpace(string(out)) == "1" {
		return nil
	}

	createSQL := fmt.Sprintf("CREATE DATABASE %s;", dbName)
	createOut, createErr := exec.Command(engine, "exec", "hal-tfe-db", "psql", "-U", "tfe", "-d", "postgres", "-c", createSQL).CombinedOutput()
	if createErr != nil {
		return fmt.Errorf("postgres database creation failed: %s", strings.TrimSpace(string(createOut)))
	}

	return nil
}

func ensureTwinBucketExists(engine, bucketName string) error {
	trimmed := strings.TrimSpace(bucketName)
	if trimmed == "" || strings.Contains(trimmed, "/") || strings.Contains(trimmed, " ") {
		return fmt.Errorf("invalid bucket name '%s'", bucketName)
	}

	out, err := exec.Command(engine, "exec", "hal-tfe-minio", "sh", "-c", fmt.Sprintf("mkdir -p /data/%s", trimmed)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("minio bucket creation failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func ensureCertsForTwin(certDir string, dnsNames []string) error {
	certPath := filepath.Join(certDir, "cert.pem")
	keyPath := filepath.Join(certDir, "key.pem")

	if _, err := os.Stat(certPath); err == nil {
		if !shouldRotateTwinTFECert(certPath, dnsNames) {
			return nil
		}
		_ = os.Remove(certPath)
		_ = os.Remove(keyPath)
	}

	if err := os.MkdirAll(certDir, 0o755); err != nil {
		return err
	}

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

	commonName := "tfe-bis.localhost"
	for _, name := range dnsNames {
		if name != "" && name != "localhost" {
			commonName = name
			break
		}
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"HAL Twin TFE Local Dev Environment"},
			CommonName:   commonName,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              dnsNames,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	certOut, err := os.Create(certPath)
	if err != nil {
		return err
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return err
	}

	keyOut, err := os.Create(keyPath)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}); err != nil {
		return err
	}

	return nil
}

func shouldRotateTwinTFECert(certPath string, dnsNames []string) bool {
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

	hasTwinDNS := false
	for _, desired := range dnsNames {
		if desired == "" || desired == "localhost" {
			continue
		}
		for _, existing := range cert.DNSNames {
			if existing == desired {
				hasTwinDNS = true
				break
			}
		}
		if hasTwinDNS {
			break
		}
	}
	if !hasTwinDNS {
		return true
	}

	legacyIssuer := strings.Contains(strings.Join(cert.Subject.Organization, ","), "HAL Local Dev Environment")
	if legacyIssuer && cert.SerialNumber.Cmp(big.NewInt(1)) == 0 {
		return true
	}

	return false
}

func waitForTwinService(url string, maxRetries int) error {
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
	return fmt.Errorf("timeout waiting for twin TFE at %s", url)
}

func runTFETwinLifecycle(enable, disable, update, configureObsOnly bool) {
	tfeTwinEnable = enable
	tfeTwinDisable = disable
	tfeTwinUpdate = update
	tfeTwinConfigureObsOnly = configureObsOnly
	twinCmd.Run(twinCmd, nil)
}

func bindTwinFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&tfeTwinVersion, "twin-version", "1.2.0", "Terraform Enterprise Docker image tag for the twin instance")
	cmd.Flags().StringVar(&tfeTwinPassword, "twin-password", "hal-secret-encryption-password", "Twin TFE encryption password")
	cmd.Flags().StringVar(&tfeTwinOrg, "twin-tfe-org", "hal-bis", "Terraform Enterprise organization name to auto-bootstrap for the twin instance")
	cmd.Flags().StringVar(&tfeTwinProject, "twin-tfe-project", "Dave-bis", "Terraform Enterprise project name to auto-bootstrap for the twin instance")
	cmd.Flags().StringVar(&tfeTwinAdminUser, "twin-tfe-admin-username", "haladmin", "Initial twin TFE admin username used when bootstrapping via IACT")
	cmd.Flags().StringVar(&tfeTwinAdminEmail, "twin-tfe-admin-email", "haladmin@localhost", "Initial twin TFE admin email used when bootstrapping via IACT")
	cmd.Flags().StringVar(&tfeTwinAdminPass, "twin-tfe-admin-password", "hal9000FTW", "Initial twin TFE admin password used when bootstrapping via IACT")
	cmd.Flags().StringVar(&tfeTwinProxyNginxTag, "twin-proxy-nginx-version", "alpine", "Nginx image tag for the twin ingress proxy")
	cmd.Flags().IntVar(&tfeTwinHTTPSPort, "twin-https-port", 9443, "Host HTTPS port exposed by the twin TFE ingress proxy")
	cmd.Flags().StringVar(&tfeTwinHostname, "twin-hostname", "tfe-bis.localhost", "TLS hostname used by the twin TFE instance")
	cmd.Flags().StringVar(&tfeTwinContainerName, "twin-container-name", "hal-tfe-bis", "Container name used for the twin TFE core application")
	cmd.Flags().StringVar(&tfeTwinProxyInternalIP, "twin-proxy-ip", "10.89.3.55", "Static internal proxy IP on hal-net for twin hostname routing")
	cmd.Flags().StringVar(&tfeTwinDatabasePassword, "twin-db-password", "tfe_password", "PostgreSQL password used by the twin TFE backend")
	cmd.Flags().StringVar(&tfeTwinDatabaseName, "twin-db-name", "tfe_bis", "Database name for the twin TFE schema in shared PostgreSQL")
	cmd.Flags().StringVar(&tfeTwinMinioRootUser, "twin-minio-root-user", "minioadmin", "MinIO root user for shared object storage")
	cmd.Flags().StringVar(&tfeTwinMinioRootPassword, "twin-minio-root-password", "minioadmin", "MinIO root password for shared object storage")
	cmd.Flags().StringVar(&tfeTwinObjectStorageBucket, "twin-s3-bucket", "tfe-bis-data", "S3 bucket name for twin TFE objects in shared MinIO")
}

func init() {
	twinCmd.Flags().BoolVarP(&tfeTwinEnable, "enable", "e", false, "Deploy the twin Terraform Enterprise instance")
	twinCmd.Flags().BoolVar(&tfeTwinDisable, "disable", false, "Destroy the twin Terraform Enterprise instance and remove its local artifacts")
	twinCmd.Flags().BoolVarP(&tfeTwinUpdate, "update", "u", false, "Reconcile the twin Terraform Enterprise instance in place")
	twinCmd.Flags().BoolVar(&tfeTwinConfigureObsOnly, "configure-obs", false, "Refresh only the twin Prometheus target file")
	_ = twinCmd.Flags().MarkHidden("enable")
	_ = twinCmd.Flags().MarkHidden("disable")
	_ = twinCmd.Flags().MarkHidden("update")
	bindTwinFlags(twinCmd)
}
