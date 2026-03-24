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
	tfeVersion   string
	tfePassword  string
	pgVersion    string
	redisVersion string
	tfeForce     bool
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy a local Terraform Enterprise 1.x (FDO) instance via Docker",
	Run: func(cmd *cobra.Command, args []string) {

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

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		// Detect if we are using Podman to apply specific "relaxed" security flags
		isPodman := strings.Contains(engine, "podman")

		// 2. FORGE THE TLS CERTIFICATES
		fmt.Println("🔐 Forging local TLS certificates for TFE...")
		homeDir, _ := os.UserHomeDir()
		certDir := filepath.Join(homeDir, ".hal", "tfe-certs")
		if err := ensureCerts(certDir); err != nil {
			fmt.Printf("❌ Failed to generate TLS certificates: %v\n", err)
			return
		}

		if tfeForce {
			if global.Debug {
				fmt.Println("[DEBUG] --force flag detected. Purging existing TFE resources...")
			}
			_ = exec.Command(engine, "rm", "-f", "hal-tfe", "hal-tfe-db", "hal-tfe-redis", "hal-tfe-minio").Run()
		}

		fmt.Printf(" Deploying Terraform Enterprise %s (PG: %s, Redis: %s) via %s...\n", tfeVersion, pgVersion, redisVersion, engine)

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
		fmt.Printf("  Provisioning TFE PostgreSQL Database...\n")
		_ = exec.Command(engine, "run", "-d", "--name", "hal-tfe-db", "--network", "hal-net",
			"-e", "POSTGRES_USER=tfe", "-e", "POSTGRES_PASSWORD=tfe_password", "-e", "POSTGRES_DB=tfe",
			fmt.Sprintf("postgres:%s-alpine", pgVersion)).Run()

		// 6. Deploy Redis
		fmt.Printf(" Provisioning TFE Redis Cache...\n")
		_ = exec.Command(engine, "run", "-d", "--name", "hal-tfe-redis", "--network", "hal-net",
			fmt.Sprintf("redis:%s-alpine", redisVersion)).Run()

		// 7. Deploy MinIO (S3 Mock)
		fmt.Println("  Provisioning TFE Object Storage (MinIO)...")
		_ = exec.Command(engine, "run", "-d", "--name", "hal-tfe-minio", "--network", "hal-net",
			"-p", "9000:9000", "-p", "9001:9001",
			"-e", "MINIO_ROOT_USER=minioadmin", "-e", "MINIO_ROOT_PASSWORD=minioadmin",
			"minio/minio", "server", "/data", "--console-address", ":9001").Run()

		time.Sleep(3 * time.Second)
		_ = exec.Command(engine, "exec", "hal-tfe-minio", "sh", "-c", "mkdir -p /data/tfe-data").Run()

		// 8. Deploy TFE Core
		fmt.Println("  Booting TFE Core Application (This requires heavy compute)...")
		tfeArgs := []string{
			"run", "-d",
			"--name", "hal-tfe",
			"--network", "hal-net",
			"--privileged",
			"--add-host", "hal-tfe:127.0.0.1",
			"-p", "8080:8080",
			"-p", "8443:8443",
			"-v", "/var/run/docker.sock:/var/run/docker.sock",
		}

		// 🎯 PODMAN/MAC SPECIFIC HARDENING BYPASS
		// If using Podman, we must disable label confinement so the TFE user (1000)
		// can actually read the mounted certs and the socket.
		if isPodman {
			tfeArgs = append(tfeArgs, "--security-opt", "label=disable")
			tfeArgs = append(tfeArgs, "--security-opt", "seccomp=unconfined")
		}

		// Add the volumes with the ':Z' flag for Podman/SELinux compatibility
		// (Docker ignores ':Z' safely on Mac)
		tfeArgs = append(tfeArgs, "-v", fmt.Sprintf("%s:/etc/ssl/tfe:Z", certDir))

		// Append the environment variables
		tfeArgs = append(tfeArgs,
			"-e", "TFE_OPERATIONAL_MODE=external",
			"-e", "TFE_HOSTNAME=tfe.localhost",
			"-e", "TFE_IA_HOSTNAME=hal-tfe",
			"-e", "TFE_VAULT_ADDR=http://127.0.0.1:8200",
			"-e", "TFE_VAULT_DISABLE_MLOCK=true", // Required for both Docker/Podman on Mac
			"-e", "TFE_IA_INTERNAL_VAULT_ADDR=http://127.0.0.1:8200",
			"-e", "TFE_RUN_PIPELINE_DOCKER_NETWORK=hal-net",
			"-e", "TFE_HTTP_PORT=8080",
			"-e", "TFE_HTTPS_PORT=8443",
			"-e", "TFE_TLS_CERT_FILE=/etc/ssl/tfe/cert.pem",
			"-e", "TFE_TLS_KEY_FILE=/etc/ssl/tfe/key.pem",
			"-e", "TFE_DISK_CACHE_VOLUME_NAME=hal-tfe-cache",

			// Secrets
			"-e", "TFE_LICENSE",
			"-e", "TFE_ENCRYPTION_PASSWORD",

			// Database Connection
			"-e", "TFE_DATABASE_USER=tfe",
			"-e", "TFE_DATABASE_PASSWORD",
			"-e", "TFE_DATABASE_HOST=hal-tfe-db",
			"-e", "TFE_DATABASE_NAME=tfe",
			"-e", "TFE_DATABASE_PARAMETERS=sslmode=disable",

			// Redis Connection
			"-e", "TFE_REDIS_HOST=hal-tfe-redis",
			"-e", "TFE_REDIS_USE_TLS=false",
			"-e", "TFE_REDIS_USE_AUTH=false",

			// S3 (MinIO) Connection
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

		// 9. THE HEALTH CHECK PHASE
		fmt.Println("⏳ Waiting for TFE to initialize (WARNING: This can take 3-5 minutes!)...")

		// Since it's on HTTPS now, we hit the 8443 health endpoint!
		if err := waitForService("TFE", "https://tfe.localhost:8443/_health_check", 60); err != nil {
			handleDockerFailure("hal-tfe", engine)
			return
		}

		fmt.Println("✅ Terraform Enterprise 1.x is UP!")
		fmt.Println("   🔗 UI Address:   https://tfe.localhost:8443")
		fmt.Println("   🔐 Initial Setup: You will need to create the initial admin user in the UI.")
		fmt.Println("   ⚠️  Note: Accept the browser warning for your self-signed certificate.")
	},
}

// ensureCerts generates a local self-signed RSA certificate natively in Go
func ensureCerts(certDir string) error {
	certPath := filepath.Join(certDir, "cert.pem")
	keyPath := filepath.Join(certDir, "key.pem")

	// If they already exist, we skip generation!
	if _, err := os.Stat(certPath); err == nil {
		return nil
	}

	os.MkdirAll(certDir, 0755)

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"HAL Local Dev Environment"},
			CommonName:   "localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour), // Valid for 1 year
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost", "hal-tfe", "tfe.localhost"},
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

// waitForService requires a custom HTTP client to skip TLS verification for our self-signed cert
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
	deployCmd.Flags().StringVarP(&tfePassword, "password", "p", "hal-secret-encryption-password", "TFE Encryption Password")
	deployCmd.Flags().BoolVarP(&tfeForce, "force", "f", false, "Force redeploy")
	Cmd.AddCommand(deployCmd)
}
