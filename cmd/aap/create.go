package aap

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

const aapContainerName = "hal-aap"

var (
	aapImage    string
	aapTag      string
	aapUpdate   bool
	aapHostPort int
)

var aapCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a local AAP container runtime",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		global.EnsureNetwork(engine)

		if aapUpdate {
			if global.Debug {
				fmt.Println("[DEBUG] --update detected. Reconciling existing AAP runtime...")
			}
			_ = exec.Command(engine, "rm", "-f", aapContainerName).Run()
		}

		imageRef := resolveAAPImageRef(engine)
		runArgs := []string{
			"run", "-d",
			"--name", aapContainerName,
			"--hostname", "aap.demo.local",
			"--network", "hal-net",
			"--privileged",
			"--cgroupns=host",
			"-v", "/sys/fs/cgroup:/sys/fs/cgroup:rw",
			"--tmpfs", "/run",
			"--tmpfs", "/run/lock",
			"-p", fmt.Sprintf("%d:443", aapHostPort),
			imageRef,
		}

		if global.DryRun {
			fmt.Printf("[DRY RUN] Would execute: %s %s\n", engine, strings.Join(runArgs, " "))
			return
		}

		out, err := exec.Command(engine, runArgs...).CombinedOutput()
		if err != nil {
			if strings.Contains(string(out), "AlreadyExists") || strings.Contains(string(out), "already in use") {
				fmt.Println("⚠️  AAP runtime already exists. Use '--update' to reconcile it.")
				return
			}
			fmt.Printf("❌ Failed to start AAP runtime: %s\n", strings.TrimSpace(string(out)))
			return
		}

		aapURL := aapBaseURL()
		healthURL := aapHealthURL()
		fmt.Println("⏳ Waiting for AAP to initialize (this can take a few minutes)...")
		if err := waitForAAPService(healthURL+"/api/controller/v2/ping/", 600); err != nil {
			handleAAPFailure(engine)
			return
		}

		fmt.Println("✅ AAP runtime created successfully.")
		global.RefreshHalHealth(engine)
		fmt.Printf("   🔗 Endpoint: %s\n", aapURL)
		fmt.Println("   ⚠️  Note:        Accept the browser warning for the self-signed certificate.")
		fmt.Println("")
		fmt.Println("   👤 Admin User:   admin")
		fmt.Println("   🔑 Admin Pass:   admin")
	},
}

func resolveAAPImageRef(engine string) string {
	candidates := []string{
		fmt.Sprintf("%s:%s", aapImage, aapTag),
	}
	if !strings.HasPrefix(aapImage, "local/") {
		candidates = append(candidates, fmt.Sprintf("local/%s:%s", aapImage, aapTag))
	}

	for _, candidate := range candidates {
		if imageExists(engine, candidate) {
			return candidate
		}
	}

	return candidates[0]
}

func imageExists(engine, image string) bool {
	return exec.Command(engine, "image", "inspect", image).Run() == nil
}

func aapBaseURL() string {
	if aapHostPort == 443 {
		return "https://aap.localhost"
	}
	return fmt.Sprintf("https://aap.localhost:%d", aapHostPort)
}

func aapHealthURL() string {
	if aapHostPort == 443 {
		return "https://127.0.0.1"
	}
	return fmt.Sprintf("https://127.0.0.1:%d", aapHostPort)
}

func waitForAAPService(url string, maxRetries int) error {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	transport.Proxy = nil
	client := http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
	}

	for i := 0; i < maxRetries; i++ {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("timeout waiting for AAP at %s", url)
}

func handleAAPFailure(engine string) {
	fmt.Println("❌ AAP failed to become healthy in time.")
	fmt.Println("❌ Note that AAP consumes significant resources; ensure your system meets the requirements and check the logs below for troubleshooting.")
	fmt.Println("⚠️ Warning: AAP is *not* intended to run on Mac. There is a chance that it will not start correctly due to a variety of factors. delete/create again if necessary.")
	fmt.Println("📜 Fetching recent container logs...")
	out, _ := exec.Command(engine, "logs", "--tail", "80", aapContainerName).CombinedOutput()
	fmt.Println(strings.TrimSpace(string(out)))
}

var aapUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Reconcile an existing AAP runtime",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		aapUpdate = true
		aapCreateCmd.Run(cmd, args)
	},
}

func bindAAPLifecycleFlags(cmd *cobra.Command, includeUpdate bool) {
	cmd.Flags().StringVar(&aapImage, "aap-image", "ubi9-aap", "AAP container image name (falls back to local/<name> if that image exists locally)")
	cmd.Flags().StringVar(&aapTag, "aap-tag", "latest", "AAP container image tag")
	cmd.Flags().IntVar(&aapHostPort, "host-port", 443, "Host HTTPS port to publish AAP container port 443")
	if includeUpdate {
		cmd.Flags().BoolVarP(&aapUpdate, "update", "u", false, "Reconcile an existing AAP deployment in place")
	}
}

func init() {
	bindAAPLifecycleFlags(aapCreateCmd, true)
	bindAAPLifecycleFlags(aapUpdateCmd, false)
	Cmd.AddCommand(aapCreateCmd)
	Cmd.AddCommand(aapUpdateCmd)
}
