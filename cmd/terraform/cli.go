package terraform

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hal/internal/global"
	"hal/internal/integrations"

	"github.com/spf13/cobra"
)

const (
	tfeCLIContainerName = "hal-tfe-cli"
	tfeCLIImageName     = "hal-tfe-cli:latest"
	defaultTFECLIBase   = "ghcr.io/straubt1/tfx:latest"
	tfeCLIManagedWSFile = "/root/.hal-tfe-cli-managed-workspaces"
	tfeCLIRunsSeedFile  = "/root/.hal-tfe-cli-scenario-runs-seeded-v3"
	tfeCLIRunsSeedLock  = "/root/.hal-tfe-cli-scenario-runs-seeding.lock"
)

var (
	tfeCLIEnable         bool
	tfeCLIConsole        bool
	tfeCLIDisable        bool
	tfeCLIForce          bool
	tfeCLILocalDirectory string
	tfeCLIBaseImage      string
	tfeCLIURL            string
	tfeCLIDefaultOrg     string
	tfeCLIAdminUsername  string
	tfeCLIAdminEmail     string
	tfeCLIAdminPassword  string
	tfeCLIProjectSeed    string
	tfeCLIShowBannerOnly bool
	tfeCLIVerbose        bool
)

var cliCmd = &cobra.Command{
	Use:   "cli",
	Short: "Build and run an ephemeral Terraform/TFX helper shell for local TFE",
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		if tfeCLIDisable {
			if tfeCLIEnable || tfeCLIConsole || tfeCLIShowBannerOnly {
				fmt.Println("❌ '--disable' cannot be combined with '--enable', '--console', or '--banner'.")
				return
			}
			if err := disableTFECLI(engine); err != nil {
				fmt.Printf("❌ Failed to disable Terraform CLI helper: %v\n", err)
			}
			return
		}

		if !tfeCLIEnable && !tfeCLIConsole && !tfeCLIShowBannerOnly {
			showTFECLIStatus(engine)
			return
		}

		shouldBuild := tfeCLIEnable || tfeCLIForce || (tfeCLIConsole && !imageExists(engine, tfeCLIImageName))
		if shouldBuild {
			if tfeCLIForce {
				_ = exec.Command(engine, "rm", "-f", tfeCLIContainerName).Run()
			}

			if err := buildTFECLIImage(engine); err != nil {
				fmt.Printf("❌ Failed to build helper image: %v\n", err)
				return
			}
			fmt.Printf("✅ Helper image ready: %s\n", tfeCLIImageName)
		} else if tfeCLIConsole {
			fmt.Printf("✅ Reusing helper image: %s\n", tfeCLIImageName)
		}

		if tfeCLIEnable && !tfeCLIConsole {
			fmt.Println("💡 Next Step:")
			fmt.Println("   Run 'hal tf cli -c' to start the container and open a shell.")
			return
		}

		if tfeCLIConsole {
			if !global.IsContainerRunning(engine, "hal-tfe") {
				fmt.Println("❌ Terraform Enterprise is not running.")
				fmt.Println("   💡 Run 'hal terraform deploy' first.")
				return
			}

			certPath, err := tfeCLICertPath()
			if err != nil {
				fmt.Printf("❌ %v\n", err)
				fmt.Println("   💡 Run 'hal terraform deploy' first to generate local TLS material.")
				return
			}

			global.EnsureNetwork(engine)
			localDir, err := resolveTFECLILocalDirectory()
			if err != nil {
				fmt.Printf("❌ Invalid local directory option: %v\n", err)
				return
			}

			if err := ensureTFECLIContainer(engine, certPath, localDir); err != nil {
				fmt.Printf("❌ Failed to prepare helper container: %v\n", err)
				return
			}
			if err := refreshTFECLITrust(engine); err != nil {
				fmt.Printf("❌ Failed to refresh helper trust store: %v\n", err)
				return
			}

			token, err := ensureTFECLIUserToken(engine)
			if err != nil {
				fmt.Printf("❌ Failed to mint TFE user token for helper session: %v\n", err)
				return
			}

			if err := writeTFECLIAuthFiles(engine, token); err != nil {
				fmt.Printf("❌ Failed to write helper auth files: %v\n", err)
				return
			}

			if err := ensureDefaultScenarioRepos(engine, token); err != nil {
				fmt.Printf("❌ Failed to seed default /workspaces scenario: %v\n", err)
				return
			}

			if err := triggerBackgroundScenarioRunSeeding(engine); err != nil {
				fmt.Printf("⚠️  Could not start background scenario run seeding: %v\n", err)
			} else {
				fmt.Println("🧪 Scenario run seeding started in background (parallel).")
			}

			printTFECLIBanner()
			if tfeCLIShowBannerOnly {
				return
			}

			if err := openTFECLIConsole(engine); err != nil {
				fmt.Printf("❌ Console session failed: %v\n", err)
			}
		}
	},
}

func showTFECLIStatus(engine string) {
	fmt.Println("🔍 Checking Terraform CLI Helper Status...")

	if imageExists(engine, tfeCLIImageName) {
		fmt.Printf("  🟢 image              : present (%s)\n", tfeCLIImageName)
	} else {
		fmt.Printf("  ⚪ image              : missing (%s)\n", tfeCLIImageName)
	}

	if global.IsContainerRunning(engine, tfeCLIContainerName) {
		fmt.Printf("  🟢 container          : running (%s)\n", tfeCLIContainerName)
	} else if containerExists(engine, tfeCLIContainerName) {
		fmt.Printf("  🟡 container          : stopped (%s)\n", tfeCLIContainerName)
	} else {
		fmt.Printf("  ⚪ container          : not created (%s)\n", tfeCLIContainerName)
	}

	if global.IsContainerRunning(engine, "hal-tfe") {
		fmt.Println("  🟢 terraform-enterprise : running (hal-tfe)")
	} else {
		fmt.Println("  ⚪ terraform-enterprise : not running (hal-tfe)")
	}

	fmt.Printf("  🔗 target-url         : %s\n", tfeCLIURL)
	fmt.Println("\n💡 Next Step:")
	fmt.Println("   hal tf cli -e")
	fmt.Println("   hal tf cli -c")
	fmt.Println("   hal tf cli --disable --force")
}

func buildTFECLIImage(engine string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	buildDir := filepath.Join(homeDir, ".hal", "tfe-cli")
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return err
	}

	dockerfilePath := filepath.Join(buildDir, "Dockerfile")
	dockerfile := fmt.Sprintf(`FROM %s
USER root

RUN set -eux; \
	if command -v apk >/dev/null 2>&1; then \
		apk add --no-cache ca-certificates curl git unzip bash ncurses ncurses-terminfo; \
	elif command -v apt-get >/dev/null 2>&1; then \
		apt-get update; \
		DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates curl git unzip bash ncurses-term; \
		rm -rf /var/lib/apt/lists/*; \
	else \
		echo "Unsupported base image package manager"; \
		exit 1; \
	fi; \
	update-ca-certificates || true

ENV TERM=xterm-256color
ENV COLORTERM=truecolor
ENV CLICOLOR=1
ENV CLICOLOR_FORCE=1

RUN set -eux; \
	TERRAFORM_VERSION="$(curl -fsSL https://checkpoint-api.hashicorp.com/v1/check/terraform | sed -n 's/.*"current_version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"; \
	test -n "$TERRAFORM_VERSION"; \
	ARCH="$(uname -m)"; \
	case "$ARCH" in \
		x86_64|amd64) TF_ARCH=amd64 ;; \
		aarch64|arm64) TF_ARCH=arm64 ;; \
		*) echo "Unsupported arch: $ARCH"; exit 1 ;; \
	esac; \
	curl -fsSL -o /tmp/terraform.zip "https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_${TF_ARCH}.zip"; \
	unzip -q /tmp/terraform.zip -d /usr/local/bin; \
	chmod +x /usr/local/bin/terraform; \
	rm -f /tmp/terraform.zip

WORKDIR /workspace
CMD ["sh", "-lc", "mkdir -p /workspace && tail -f /dev/null"]
`, tfeCLIBaseImage)

	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		return err
	}

	cmd := exec.Command(engine, "build", "-t", tfeCLIImageName, "-f", dockerfilePath, buildDir)
	if tfeCLIVerbose || !canRenderProgressAnimation() {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	out, err := runWithProgressAnimation("Building helper image", func() ([]byte, error) {
		return cmd.CombinedOutput()
	})
	if err != nil {
		return fmt.Errorf("%s", tailText(string(out), 80))
	}

	return nil
}

func runWithProgressAnimation(title string, run func() ([]byte, error)) ([]byte, error) {
	type result struct {
		out []byte
		err error
	}

	ticker := time.NewTicker(140 * time.Millisecond)
	defer ticker.Stop()

	resultCh := make(chan result, 1)
	go func() {
		out, err := run()
		resultCh <- result{out: out, err: err}
	}()

	art := tfeCLIASCIIArt()
	maxCols := maxLineLen(art)
	revealCols := 0
	printedLines := 0
	for {
		select {
		case res := <-resultCh:
			frame, lines := renderTFECLIBuildFrame(title, maxCols, maxCols)
			if printedLines > 0 {
				fmt.Printf("\033[%dA", printedLines)
			}
			fmt.Print(frame)
			printedLines = lines
			return res.out, res.err
		case <-ticker.C:
			if revealCols < maxCols {
				revealCols++
			}
			frame, lines := renderTFECLIBuildFrame(title, revealCols, maxCols)
			if printedLines > 0 {
				fmt.Printf("\033[%dA", printedLines)
			}
			fmt.Print(frame)
			printedLines = lines
		}
	}
}

func renderTFECLIBuildFrame(title string, revealCols int, maxCols int) (string, int) {
	if maxCols <= 0 {
		maxCols = 1
	}
	if revealCols < 0 {
		revealCols = 0
	}
	if revealCols > maxCols {
		revealCols = maxCols
	}

	percent := (revealCols * 100) / maxCols
	art := tfeCLIASCIIArt()

	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s  %3d%%\n", title, percent))
	b.WriteString("\n")

	for _, line := range art {
		if len(line) < maxCols {
			line = line + strings.Repeat(" ", maxCols-len(line))
		}
		limit := revealCols
		if limit > len(line) {
			limit = len(line)
		}
		b.WriteString(line[:limit])
		if limit < len(line) {
			b.WriteString(strings.Repeat(" ", len(line)-limit))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")

	return b.String(), len(art) + 3
}

func tfeCLIASCIIArt() []string {
	return []string{
		"   ***                          ",
		"   ******.                      ",
		"   ********..                .  ",
		"   ********..***          .###  ",
		"   ********..******.   .######  ",
		"      *****..******** ########  ",
		"         **..******** ########  ",
		"            .******** ########  ",
		"            .* .***** #####.    ",
		"            .****. ** ##        ",
		"            .*******            ",
		"            .********           ",
		"            .********           ",
		"             .*******           ",
		"                 ****           ",
		"                    *           ",
	}
}

func maxLineLen(lines []string) int {
	maxLen := 0
	for _, line := range lines {
		if len(line) > maxLen {
			maxLen = len(line)
		}
	}
	return maxLen
}

func tailText(input string, maxLines int) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "command failed with no output"
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) <= maxLines {
		return trimmed
	}
	return strings.Join(lines[len(lines)-maxLines:], "\n")
}

func ensureTFECLIContainer(engine, certPath, localDir string) error {
	if global.IsContainerRunning(engine, tfeCLIContainerName) {
		if localDir != "" && !containerHasMount(engine, tfeCLIContainerName, "/workspaces") {
			return fmt.Errorf("existing helper container is running without /workspaces mount; re-run with '--force' to recreate with --local-directory")
		}
		return nil
	}

	if containerExists(engine, tfeCLIContainerName) {
		if localDir != "" && !containerHasMount(engine, tfeCLIContainerName, "/workspaces") {
			_ = exec.Command(engine, "rm", "-f", tfeCLIContainerName).Run()
		} else {
			out, err := exec.Command(engine, "start", tfeCLIContainerName).CombinedOutput()
			if err != nil {
				return fmt.Errorf("%s", strings.TrimSpace(string(out)))
			}

			if global.IsContainerRunning(engine, tfeCLIContainerName) {
				return nil
			}

			// Older helper containers may exit immediately because they were created with
			// a stale command or environment. Recreate transparently for a clean session.
			_ = exec.Command(engine, "rm", "-f", tfeCLIContainerName).Run()
		}
	}

	runArgs := []string{
		"run", "-d",
		"--name", tfeCLIContainerName,
		"--network", "hal-net",
		"--entrypoint", "sh",
		"-v", fmt.Sprintf("%s:/hal/certs/tfe-localhost.crt:ro", certPath),
	}

	if localDir != "" {
		runArgs = append(runArgs, "-v", fmt.Sprintf("%s:/workspaces", localDir))
	}

	runArgs = append(runArgs,
		tfeCLIImageName,
		"-lc",
		"set -e; mkdir -p /workspace /workspaces; tail -f /dev/null",
	)

	out, err := exec.Command(engine, runArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	if !global.IsContainerRunning(engine, tfeCLIContainerName) {
		logs, _ := exec.Command(engine, "logs", "--tail", "40", tfeCLIContainerName).CombinedOutput()
		return fmt.Errorf("helper container exited unexpectedly: %s", strings.TrimSpace(string(logs)))
	}

	return nil
}

func resolveTFECLILocalDirectory() (string, error) {
	trimmed := strings.TrimSpace(tfeCLILocalDirectory)
	if trimmed == "" {
		return "", nil
	}

	expanded := trimmed
	if strings.HasPrefix(expanded, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		expanded = filepath.Join(home, strings.TrimPrefix(expanded, "~/"))
	}

	absPath, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(absPath, 0755); err != nil {
		return "", err
	}

	return absPath, nil
}

func containerHasMount(engine, containerName, destination string) bool {
	out, err := exec.Command(engine, "inspect", "-f", "{{range .Mounts}}{{.Destination}}\n{{end}}", containerName).Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == destination {
			return true
		}
	}
	return false
}

func refreshTFECLITrust(engine string) error {
	out, err := exec.Command(
		engine,
		"exec",
		tfeCLIContainerName,
		"sh",
		"-lc",
		"set -e; cp /hal/certs/tfe-localhost.crt /usr/local/share/ca-certificates/tfe-localhost.crt; update-ca-certificates >/dev/null 2>&1 || true",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

func ensureTFECLIUserToken(engine string) (string, error) {
	apiToken, _, err := ensureTFEFoundation(engine, tfeFoundationConfig{
		BaseURL:       tfeCLIURL,
		OrgName:       tfeCLIDefaultOrg,
		ProjectName:   tfeCLIProjectSeed,
		AdminUsername: tfeCLIAdminUsername,
		AdminEmail:    tfeCLIAdminEmail,
		AdminPassword: tfeCLIAdminPassword,
	})
	if err != nil {
		return "", err
	}

	accountBody, _, err := integrations.TFERequest("GET", fmt.Sprintf("%s/api/v2/account/details", tfeCLIURL), apiToken, nil)
	if err != nil {
		return "", fmt.Errorf("failed to read account details")
	}

	userID := extractTFEDataID(accountBody)
	if userID == "" {
		return "", fmt.Errorf("account details did not include user id")
	}

	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "authentication-tokens",
			"attributes": map[string]interface{}{
				"description": "hal-tfe-cli-session",
			},
		},
	}

	createURL := fmt.Sprintf("%s/api/v2/users/%s/authentication-tokens", tfeCLIURL, userID)
	createBody, _, createErr := integrations.TFERequest("POST", createURL, apiToken, payload)
	if createErr != nil {
		return "", fmt.Errorf("failed to create user authentication token")
	}

	token := extractTFEAuthToken(createBody)
	if token == "" {
		return "", fmt.Errorf("token create response did not include token")
	}

	return token, nil
}

func extractTFEAuthToken(body []byte) string {
	var resp map[string]interface{}
	_ = json.Unmarshal(body, &resp)

	if token, ok := resp["token"].(string); ok && strings.TrimSpace(token) != "" {
		return strings.TrimSpace(token)
	}

	data, _ := resp["data"].(map[string]interface{})
	if token, ok := data["token"].(string); ok && strings.TrimSpace(token) != "" {
		return strings.TrimSpace(token)
	}

	attrs, _ := data["attributes"].(map[string]interface{})
	if token, ok := attrs["token"].(string); ok && strings.TrimSpace(token) != "" {
		return strings.TrimSpace(token)
	}

	return ""
}

func writeTFECLIAuthFiles(engine, token string) error {
	tfxHostname := normalizeTFXHostname(tfeCLIURL)
	terraformCredHost := normalizeTerraformCredentialsHost(tfeCLIURL)

	tfxConfig := fmt.Sprintf("hostname            = \"%s\"\ndefaultOrganization = \"%s\"\ntoken               = \"%s\"\n", tfxHostname, tfeCLIDefaultOrg, token)

	credentials := map[string]interface{}{}
	for _, host := range terraformCredentialsHosts(terraformCredHost) {
		credentials[host] = map[string]string{"token": token}
	}

	tfCreds := map[string]interface{}{
		"credentials": credentials,
	}
	tfCredsBytes, _ := json.MarshalIndent(tfCreds, "", "  ")

	writeScript := strings.Join([]string{
		"set -e",
		"mkdir -p /root/.terraform.d",
		"cat > /root/.tfx.hcl <<'EOF_TFX'",
		tfxConfig,
		"EOF_TFX",
		"cat > /root/.terraform.d/credentials.tfrc.json <<'EOF_TF'",
		string(tfCredsBytes),
		"EOF_TF",
	}, "\n")

	cmd := exec.Command(engine, "exec", tfeCLIContainerName, "sh", "-lc", writeScript)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}

	return nil
}

type tfeCLIAuthConfig struct {
	Hostname     string
	Organization string
	Token        string
}

func disableTFECLI(engine string) error {
	if !tfeCLIForce {
		if !isInteractiveTTY() {
			fmt.Println("⚠️  'hal tf cli --disable' is destructive.")
			fmt.Println("   It removes the helper container and deletes HAL-managed scenario workspaces tracked in TFE.")
			fmt.Println("   Re-run with 'hal tf cli --disable --force' to confirm in non-interactive mode.")
			return nil
		}

		fmt.Println("⚠️  'hal tf cli --disable' is destructive.")
		fmt.Println("   It removes the helper container and deletes HAL-managed scenario workspaces tracked in TFE.")
		confirmed, err := confirmTFECLIDisable()
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Println("ℹ️  Disable cancelled.")
			return nil
		}
	}

	if containerExists(engine, tfeCLIContainerName) {
		if !global.IsContainerRunning(engine, tfeCLIContainerName) {
			_, _ = exec.Command(engine, "start", tfeCLIContainerName).CombinedOutput()
		}

		auth, authErr := readTFECLIAuthConfig(engine)
		managedWorkspaces, _ := readTFECLIManagedList(engine, tfeCLIManagedWSFile)

		if len(managedWorkspaces) > 0 {
			if !global.IsContainerRunning(engine, "hal-tfe") {
				fmt.Println("⚠️  Skipping TFE workspace cleanup because hal-tfe is not running.")
			} else if authErr != nil {
				fmt.Printf("⚠️  Skipping TFE workspace cleanup because helper auth could not be read: %v\n", authErr)
			} else {
				for _, workspaceName := range managedWorkspaces {
					if err := deleteTFECLIManagedWorkspace(auth, workspaceName); err != nil {
						fmt.Printf("⚠️  Could not delete workspace %s: %v\n", workspaceName, err)
						continue
					}
					fmt.Printf("🧹 Deleted managed workspace: %s\n", workspaceName)
				}
			}
		}

		out, err := exec.Command(engine, "rm", "-f", tfeCLIContainerName).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s", strings.TrimSpace(string(out)))
		}

		fmt.Println("✅ Helper container removed.")
	} else {
		fmt.Println("ℹ️  Helper container is already absent.")
	}

	if out, err := exec.Command(engine, "image", "rm", "-f", tfeCLIImageName).CombinedOutput(); err != nil {
		outputStr := strings.ToLower(strings.TrimSpace(string(out)))
		if !strings.Contains(outputStr, "no such image") && !strings.Contains(outputStr, "image not known") {
			return fmt.Errorf("failed to remove helper image %s: %s", tfeCLIImageName, strings.TrimSpace(string(out)))
		}
		fmt.Println("ℹ️  Helper image is already absent.")
	} else {
		fmt.Printf("✅ Helper image removed: %s\n", tfeCLIImageName)
	}

	return nil
}

func confirmTFECLIDisable() (bool, error) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Continue and destroy helper container plus HAL-managed workspaces? [y/N]: ")
	response, err := reader.ReadString('\n')
	if err != nil && len(response) == 0 {
		return false, err
	}

	switch strings.ToLower(strings.TrimSpace(response)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func readTFECLIManagedList(engine, path string) ([]string, error) {
	cmd := exec.Command(engine, "exec", tfeCLIContainerName, "sh", "-lc", fmt.Sprintf("cat %s 2>/dev/null || true", path))
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	items := []string{}
	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		items = append(items, trimmed)
	}
	return items, nil
}

func readTFECLIAuthConfig(engine string) (tfeCLIAuthConfig, error) {
	cmd := exec.Command(engine, "exec", tfeCLIContainerName, "sh", "-lc", "cat /root/.tfx.hcl")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return tfeCLIAuthConfig{}, fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}

	config := tfeCLIAuthConfig{}
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), "\"")
		switch key {
		case "hostname":
			config.Hostname = value
		case "defaultOrganization":
			config.Organization = value
		case "token":
			config.Token = value
		}
	}

	if config.Organization == "" {
		config.Organization = tfeCLIDefaultOrg
	}
	if config.Hostname == "" {
		config.Hostname = normalizeTFXHostname(tfeCLIURL)
	}
	if config.Token == "" {
		return tfeCLIAuthConfig{}, fmt.Errorf("token missing from /root/.tfx.hcl")
	}

	return config, nil
}

func deleteTFECLIManagedWorkspace(auth tfeCLIAuthConfig, workspaceName string) error {
	baseURL := strings.TrimSuffix(tfeCLIURL, "/")
	if baseURL == "" {
		baseURL = "https://" + normalizeTFXHostname(auth.Hostname)
	}
	if strings.Contains(baseURL, "hal-tfe:") {
		baseURL = "https://tfe.localhost:8443"
	}

	deleteURL := fmt.Sprintf("%s/api/v2/organizations/%s/workspaces/%s", baseURL, auth.Organization, workspaceName)
	body, status, err := integrations.TFERequest("DELETE", deleteURL, auth.Token, nil)
	if err != nil && status != 404 {
		return fmt.Errorf("%s", strings.TrimSpace(string(body)))
	}
	return nil
}

func normalizeTFXHostname(raw string) string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "tfe.localhost:8443"
	}

	if !strings.Contains(clean, "://") {
		return strings.TrimSuffix(clean, "/")
	}

	parsed, err := url.Parse(clean)
	if err != nil {
		return strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(clean, "https://"), "http://"), "/")
	}

	if parsed.Host != "" {
		return parsed.Host
	}

	return strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(clean, "https://"), "http://"), "/")
}

func normalizeTerraformCredentialsHost(raw string) string {
	host := normalizeTFXHostname(raw)
	if host == "" {
		return "tfe.localhost:8443"
	}
	return host
}

func terraformCredentialsHosts(primary string) []string {
	seen := map[string]bool{}
	ordered := []string{}

	add := func(host string) {
		h := strings.TrimSpace(host)
		if h == "" || seen[h] {
			return
		}
		seen[h] = true
		ordered = append(ordered, h)
	}

	add(primary)
	add("tfe.localhost:8443")
	add("hal-tfe:8443")

	return ordered
}

func ensureDefaultScenarioRepos(engine, token string) error {
	if err := ensureDefaultScenarioWorkspaces(token); err != nil {
		return err
	}

	tfxHostname := normalizeTFXHostname(tfeCLIURL)
	repoDefs := []struct {
		Name      string
		Theme     string
		DadJoke   string
		Workspace string
	}{
		{Name: "hal-lucinated", Theme: "mushrooms in the forest", DadJoke: "These runs are fun-guys.", Workspace: "hal-lucinated"},
		{Name: "hal-ogen", Theme: "periodic table lighting", DadJoke: "This workspace has brilliant chemistry.", Workspace: "hal-ogen"},
		{Name: "hal-lelujah", Theme: "choir practice and cloud harmony", DadJoke: "Even the plans sing in four-part harmony.", Workspace: "hal-lelujah"},
		{Name: "hal-oween", Theme: "pumpkins, ghosts, and spooky drift", DadJoke: "This stack is haunted by state spirits.", Workspace: "hal-oween"},
		{Name: "hal-ibut", Theme: "deep-sea fish operations", DadJoke: "Something smells fishy, but the plan is clean.", Workspace: "hal-ibut"},
	}

	workspaceList := []string{}
	for _, def := range repoDefs {
		workspaceList = append(workspaceList, def.Workspace)
	}

	managedListScript := strings.Join([]string{
		"set -e",
		fmt.Sprintf("cat > %s <<'EOF_WS'", tfeCLIManagedWSFile),
		strings.Join(workspaceList, "\n"),
		"EOF_WS",
	}, "\n")

	if out, err := exec.Command(engine, "exec", tfeCLIContainerName, "sh", "-lc", managedListScript).CombinedOutput(); err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}

	for _, def := range repoDefs {
		script := fmt.Sprintf(`set -e
mkdir -p /workspaces/%s
if [ ! -f /workspaces/%s/main.tf ]; then
cat > /workspaces/%s/main.tf <<'EOF_TF'
terraform {
  required_version = ">= 1.6.0"

  cloud {
    hostname     = "%s"
    organization = "%s"

    workspaces {
      name = "%s"
    }
  }
}

locals {
  repo_name = "%s"
  theme     = "%s"
  dad_joke  = "%s"
}

output "repo" {
  value = local.repo_name
}

output "theme" {
  value = local.theme
}

output "dad_joke" {
  value = local.dad_joke
}
EOF_TF
fi
`, def.Name, def.Name, def.Name, tfxHostname, tfeCLIDefaultOrg, def.Workspace, def.Name, def.Theme, def.DadJoke)

		if out, err := exec.Command(engine, "exec", tfeCLIContainerName, "sh", "-lc", script).CombinedOutput(); err != nil {
			return fmt.Errorf("%s", strings.TrimSpace(string(out)))
		}
	}

	return nil
}

func triggerBackgroundScenarioRunSeeding(engine string) error {
	launchScript := fmt.Sprintf(`set -e
if [ -f %s ]; then
  exit 0
fi
if [ -f %s ]; then
  exit 0
fi

cat > /tmp/hal-tfe-cli-seed-runs.sh <<'EOF_HAL_SEED'
#!/usr/bin/env sh
set -eu

touch %s
trap 'rm -f %s' EXIT

failures=0
SEED_STATUS_DIR="/tmp/hal-tfe-cli-seed-status"
rm -rf "${SEED_STATUS_DIR}"
mkdir -p "${SEED_STATUS_DIR}"

run_step() {
	step_name="$1"
	shift
	if "$@"; then return 0; fi
	return 1
}

run_plan() {
  repo="$1"
	(
		set -eu
		cd "/workspaces/${repo}"
		terraform init -input=false >/tmp/${repo}_seed_init.log 2>&1
		terraform plan -input=false >/tmp/${repo}_seed_plan.log 2>&1
	)
}

run_apply() {
  repo="$1"
	(
		set -eu
		cd "/workspaces/${repo}"
		terraform init -input=false >/tmp/${repo}_seed_init.log 2>&1
		terraform apply -auto-approve -input=false >/tmp/${repo}_seed_apply.log 2>&1
	)
}

run_plan_apply() {
	repo="$1"
	run_plan "${repo}"
	run_apply "${repo}"
}

run_task() {
	task_name="$1"
	shift
	(
		if run_step "${task_name}" "$@"; then
			echo ok > "${SEED_STATUS_DIR}/${task_name}.status"
		else
			echo failed > "${SEED_STATUS_DIR}/${task_name}.status"
		fi
	) &
}

# Parallel tasks:
# - 2 workspaces with plan-only
# - 2 workspaces with plan+apply
run_task "hal_ogen_plan" run_plan hal-ogen
run_task "hal_oween_plan" run_plan hal-oween
run_task "hal_lucinated_plan_apply" run_plan_apply hal-lucinated
run_task "hal_lelujah_plan_apply" run_plan_apply hal-lelujah

wait

for status_file in "${SEED_STATUS_DIR}"/*.status; do
	task_name="$(basename "${status_file}" .status)"
	if [ "$(cat "${status_file}")" = "ok" ]; then
		echo "[seed] ok: ${task_name}" >> /tmp/hal-tfe-cli-seed-runs.log
	else
		failures=$((failures + 1))
		echo "[seed] failed: ${task_name}" >> /tmp/hal-tfe-cli-seed-runs.log
	fi
done

# hal-ibut intentionally untouched

if [ "$failures" -eq 0 ]; then
	echo seeded > %s
else
	echo "[seed] completed with ${failures} failure(s)" >> /tmp/hal-tfe-cli-seed-runs.log
fi
EOF_HAL_SEED

chmod +x /tmp/hal-tfe-cli-seed-runs.sh
nohup /tmp/hal-tfe-cli-seed-runs.sh >/tmp/hal-tfe-cli-seed-runs.log 2>&1 &
`, tfeCLIRunsSeedFile, tfeCLIRunsSeedLock, tfeCLIRunsSeedLock, tfeCLIRunsSeedLock, tfeCLIRunsSeedFile)

	out, err := exec.Command(engine, "exec", tfeCLIContainerName, "sh", "-lc", launchScript).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}

	return nil
}

func ensureDefaultScenarioWorkspaces(token string) error {
	baseURL := strings.TrimSuffix(strings.TrimSpace(tfeCLIURL), "/")
	if baseURL == "" {
		return fmt.Errorf("tfe url cannot be empty")
	}

	org := strings.ToLower(strings.TrimSpace(tfeCLIDefaultOrg))
	if org == "" {
		return fmt.Errorf("tfe org cannot be empty")
	}

	if strings.TrimSpace(tfeCLIProjectSeed) == "" {
		// Backward compatibility cleanup: older helper flows created a HAL-CLI project
		// by default. Remove it when not explicitly requested.
		_ = removeLegacyTFEProject(baseURL, org, token, "HAL-CLI")
	}

	daveProjectID, err := ensureTFEOrgAndProject(baseURL, token, org, "Dave")
	if err != nil {
		return fmt.Errorf("failed to ensure TFE project Dave: %w", err)
	}

	frankProjectID, err := ensureTFEOrgAndProject(baseURL, token, org, "Frank")
	if err != nil {
		return fmt.Errorf("failed to ensure TFE project Frank: %w", err)
	}

	scenarioWorkspaces := []struct {
		Name      string
		ProjectID string
	}{
		{Name: "hal-lucinated", ProjectID: daveProjectID},
		{Name: "hal-ogen", ProjectID: frankProjectID},
		{Name: "hal-lelujah", ProjectID: daveProjectID},
		{Name: "hal-oween", ProjectID: frankProjectID},
		{Name: "hal-ibut", ProjectID: daveProjectID},
	}

	for _, ws := range scenarioWorkspaces {
		if err := ensureTFESCenarioWorkspace(baseURL, org, token, ws.Name, ws.ProjectID); err != nil {
			return err
		}
	}

	return nil
}

func removeLegacyTFEProject(baseURL, org, token, projectName string) error {
	listURL := fmt.Sprintf("%s/api/v2/organizations/%s/projects", baseURL, org)
	body, _, err := integrations.TFERequest("GET", listURL, token, nil)
	if err != nil {
		return err
	}

	var listResp map[string]interface{}
	_ = json.Unmarshal(body, &listResp)
	data, _ := listResp["data"].([]interface{})

	for _, item := range data {
		project, _ := item.(map[string]interface{})
		attrs, _ := project["attributes"].(map[string]interface{})
		if fmt.Sprintf("%v", attrs["name"]) != projectName {
			continue
		}

		projectID := strings.TrimSpace(fmt.Sprintf("%v", project["id"]))
		if projectID == "" || projectID == "<nil>" {
			continue
		}

		delURL := fmt.Sprintf("%s/api/v2/projects/%s", baseURL, projectID)
		_, _, delErr := integrations.TFERequest("DELETE", delURL, token, nil)
		return delErr
	}

	return nil
}

func ensureTFESCenarioWorkspace(baseURL, org, token, workspaceName, projectID string) error {
	getURL := fmt.Sprintf("%s/api/v2/organizations/%s/workspaces/%s", baseURL, org, workspaceName)
	getBody, getStatus, getErr := integrations.TFERequest("GET", getURL, token, nil)

	attributes := map[string]interface{}{
		"name":           workspaceName,
		"auto-apply":     true,
		"execution-mode": "remote",
	}

	if getErr == nil {
		workspaceID := extractTFEDataID(getBody)
		if workspaceID == "" {
			return fmt.Errorf("workspace lookup for %s did not return an id", workspaceName)
		}

		patchPayload := map[string]interface{}{
			"data": map[string]interface{}{
				"type":       "workspaces",
				"id":         workspaceID,
				"attributes": attributes,
				"relationships": map[string]interface{}{
					"project": map[string]interface{}{
						"data": map[string]interface{}{
							"id":   projectID,
							"type": "projects",
						},
					},
				},
			},
		}

		patchURL := fmt.Sprintf("%s/api/v2/workspaces/%s", baseURL, workspaceID)
		patchBody, _, patchErr := integrations.TFERequest("PATCH", patchURL, token, patchPayload)
		if patchErr != nil {
			return fmt.Errorf("failed to patch workspace %s: %s", workspaceName, strings.TrimSpace(string(patchBody)))
		}

		return nil
	}

	if getStatus != 404 {
		return fmt.Errorf("workspace lookup failed for %s: %s", workspaceName, strings.TrimSpace(string(getBody)))
	}

	createPayload := map[string]interface{}{
		"data": map[string]interface{}{
			"type":       "workspaces",
			"attributes": attributes,
			"relationships": map[string]interface{}{
				"project": map[string]interface{}{
					"data": map[string]interface{}{
						"id":   projectID,
						"type": "projects",
					},
				},
			},
		},
	}

	createURL := fmt.Sprintf("%s/api/v2/organizations/%s/workspaces", baseURL, org)
	createBody, _, createErr := integrations.TFERequest("POST", createURL, token, createPayload)
	if createErr != nil {
		return fmt.Errorf("failed to create workspace %s: %s", workspaceName, strings.TrimSpace(string(createBody)))
	}

	return nil
}

func openTFECLIConsole(engine string) error {
	args := []string{"exec", "-i"}
	if isInteractiveTTY() {
		args = append(args, "-t")
	}
	args = append(args,
		"-e", "TERM=xterm-256color",
		"-e", "COLORTERM=truecolor",
		"-e", "CLICOLOR=1",
		"-e", "CLICOLOR_FORCE=1",
		tfeCLIContainerName,
		"sh", "-lc",
		"cd /workspaces 2>/dev/null || cd /workspace 2>/dev/null || true; exec ${SHELL:-sh}",
	)

	cmd := exec.Command(engine, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func printTFECLIBanner() {
	fmt.Println("")
	fmt.Println(" _   _    _    _     ")
	fmt.Println("| | | |  / \\  | |    ")
	fmt.Println("| |_| | / _ \\ | |    ")
	fmt.Println("|  _  |/ ___ \\| |___ ")
	fmt.Println("|_| |_/_/   \\_\\_____|")
	fmt.Println("")
	fmt.Println("You are now entering the ephemeral HAL Terraform helper environment.")
	fmt.Println("Available tools: terraform, tfx, git, curl")
	fmt.Printf("Target TFE URL: %s\n", tfeCLIURL)
	if strings.TrimSpace(tfeCLILocalDirectory) != "" {
		fmt.Printf("Host workspace mount: %s -> /workspaces\n", tfeCLILocalDirectory)
	}
	fmt.Println("Auth is already bootstrapped: avoid 'terraform login' unless you want to rotate tokens.")
	fmt.Println("Leaving with CTRL+D or 'exit' only closes this shell. Re-enter later with 'hal tf cli -c'.")
	fmt.Println("Exit this console with CTRL+D or by typing 'exit'.")
	fmt.Println("")
}

func tfeCLICertPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	certPath := filepath.Join(homeDir, ".hal", "tfe-certs", "cert.pem")
	if _, err := os.Stat(certPath); err != nil {
		return "", fmt.Errorf("required TFE certificate was not found at %s", certPath)
	}

	return certPath, nil
}

func containerExists(engine, name string) bool {
	// Use explicit container inspect so image names do not produce false positives.
	return exec.Command(engine, "container", "inspect", name).Run() == nil
}

func imageExists(engine, name string) bool {
	return exec.Command(engine, "image", "inspect", name).Run() == nil
}

func isInteractiveTTY() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func canRenderProgressAnimation() bool {
	term := strings.ToLower(strings.TrimSpace(os.Getenv("TERM")))
	if term == "dumb" {
		return false
	}

	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func init() {
	cliCmd.Flags().BoolVarP(&tfeCLIEnable, "enable", "e", false, "Build or refresh the Terraform/TFX helper image")
	cliCmd.Flags().BoolVarP(&tfeCLIConsole, "console", "c", false, "Start helper container and open an interactive shell")
	cliCmd.Flags().BoolVarP(&tfeCLIDisable, "disable", "d", false, "Remove the helper container and delete HAL-managed scenario workspaces")
	cliCmd.Flags().BoolVarP(&tfeCLIForce, "force", "f", false, "Rebuild image and recreate helper container")
	cliCmd.Flags().BoolVar(&tfeCLIShowBannerOnly, "banner", false, "Print helper welcome banner without opening a shell")
	cliCmd.Flags().StringVar(&tfeCLILocalDirectory, "local-directory", "", "Optional host directory to mount into the helper at /workspaces")
	cliCmd.Flags().StringVar(&tfeCLIBaseImage, "base-image", defaultTFECLIBase, "Base image used to build the helper image")
	cliCmd.Flags().StringVar(&tfeCLIURL, "tfe-url", "https://tfe.localhost:8443", "Terraform Enterprise URL used for helper auth bootstrap")
	cliCmd.Flags().StringVar(&tfeCLIDefaultOrg, "tfe-org", "hal", "Default Terraform Enterprise organization written to ~/.tfx.hcl")
	cliCmd.Flags().StringVar(&tfeCLIAdminUsername, "tfe-admin-username", "haladmin", "Terraform Enterprise admin username used for helper token bootstrap")
	cliCmd.Flags().StringVar(&tfeCLIAdminEmail, "tfe-admin-email", "haladmin@localhost", "Terraform Enterprise admin email used for helper token bootstrap")
	cliCmd.Flags().StringVar(&tfeCLIAdminPassword, "tfe-admin-password", "hal9000FTW", "Terraform Enterprise admin password used for helper token bootstrap")
	cliCmd.Flags().StringVar(&tfeCLIProjectSeed, "tfe-project", "", "Optional Terraform Enterprise project to ensure during helper token bootstrap")
	cliCmd.Flags().BoolVar(&tfeCLIVerbose, "verbose", false, "Show raw Docker build logs instead of HAL build animation")

	Cmd.AddCommand(cliCmd)
}
