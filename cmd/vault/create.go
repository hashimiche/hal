package vault

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hal/internal/global"
	"hal/internal/ui"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// promptForVaultLicense interactively asks for a Vault Enterprise license when
// none was found in the environment. It returns "" on a non-interactive stdin
// (piped input, MCP, CI) or when the user submits nothing, so callers fall back
// to the standard non-interactive error. The input may be the license string
// itself or a path to a .hclic file (a leading ~ is expanded); if it resolves to
// a readable file, its contents are used.
//
// The read uses full raw mode (term.MakeRaw + a manual byte loop) rather than a
// line reader or term.ReadPassword. This is deliberate and non-obvious: Vault
// Enterprise license strings routinely exceed the terminal's canonical-mode line
// limit (MAX_CANON, ~1024 bytes on macOS), which makes a cooked-mode reader
// silently stop accepting input — including Enter — mid-paste. term.ReadPassword
// does NOT help here because on macOS it keeps ICANON enabled (it only disables
// echo), so it hits the same cap. Full raw mode disables ICANON, removing the
// cap so long pastes submit correctly.
func promptForVaultLicense() string {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "" // not a TTY — preserve non-interactive behavior
	}

	fmt.Println("🔑 Vault Enterprise requires a license, but none was found in the environment.")
	fmt.Println("   Tip: the most reliable path is a file — export VAULT_LICENSE_PATH=/path/to/vault.hclic")
	fmt.Print("   Or paste the license OR a path to a .hclic file, then press Enter (empty Enter aborts):\n   > ")

	raw, err := readLineRaw(fd)
	fmt.Println() // raw mode does not echo the newline
	if err != nil {
		return ""
	}
	input := strings.TrimSpace(raw)
	if input == "" {
		return ""
	}
	// Reassure the user the (unechoed) paste landed.
	fmt.Printf("   ✓ received %d characters\n", len(input))

	// Expand a leading ~ so "~/vault.hclic" resolves.
	candidate := input
	if strings.HasPrefix(candidate, "~/") {
		if home, herr := os.UserHomeDir(); herr == nil {
			candidate = filepath.Join(home, candidate[2:])
		}
	}
	if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
		if b, readErr := os.ReadFile(candidate); readErr == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return input
}

// readLineRaw reads a single line from the terminal in FULL raw mode, so pasted
// input is not subject to the canonical-mode line-length cap (MAX_CANON) that
// makes long licenses impossible to submit. It terminates on CR or LF, supports
// Ctrl-C/Ctrl-D as abort and backspace for edits, and strips bracketed-paste
// escape markers a terminal may wrap the paste in.
func readLineRaw(fd int) (string, error) {
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	defer term.Restore(fd, oldState)

	var buf []byte
	b := make([]byte, 1)
	for {
		n, readErr := os.Stdin.Read(b)
		if n > 0 {
			switch b[0] {
			case '\r', '\n':
				return stripBracketedPaste(buf), nil
			case 3, 4: // Ctrl-C / Ctrl-D → abort
				return "", fmt.Errorf("input aborted")
			case 127, 8: // DEL / backspace
				if len(buf) > 0 {
					buf = buf[:len(buf)-1]
				}
			default:
				buf = append(buf, b[0])
			}
		}
		if readErr != nil {
			if len(buf) > 0 {
				return stripBracketedPaste(buf), nil
			}
			return "", readErr
		}
	}
}

// stripBracketedPaste removes the ESC[200~ / ESC[201~ markers some terminals
// wrap pasted content in when bracketed-paste mode is active.
func stripBracketedPaste(b []byte) string {
	s := string(b)
	s = strings.ReplaceAll(s, "\x1b[200~", "")
	s = strings.ReplaceAll(s, "\x1b[201~", "")
	return s
}

var (
	vaultVersion     string
	vaultEdition     string // ce, ent, enterprise, or ent-hsm
	vaultImage       string // optional override for the computed per-edition image name
	vaultHelperImage string
	vaultHelperTag   string
	vaultUpdate      bool
	vaultJoinConsul  bool

	// Production-mode (--mode prod) knobs. See prod.go.
	vaultMode         string // dev or prod
	vaultKeyShares    int
	vaultKeyThreshold int
	vaultProdNodeID   string
)

var deployCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a local Vault instance",
	Run: func(cmd *cobra.Command, args []string) {

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		// ==========================================
		// PRE-FLIGHT CHECKS
		// ==========================================
		if vaultJoinConsul && !global.IsConsulRunning(engine) {
			fmt.Println("❌ Error: --join-consul was requested, but the global Consul brain is not running.")
			fmt.Println("   💡 Run 'hal consul create' first to bring the Control Plane online.")
			return
		}

		// ==========================================
		// MODE RESOLUTION (dev vs prod)
		// ==========================================
		// Reject unknown editions before license prompting or image resolution.
		if vaultEdition != "ce" && !isEnterpriseEdition(vaultEdition) {
			fmt.Printf("❌ Error: unknown --edition %q (expected 'ce', 'ent', or 'ent-hsm').\n", vaultEdition)
			return
		}

		isHSMEdition := vaultEdition == "ent-hsm"
		if isHSMEdition && vaultMode != "prod" {
			fmt.Println("❌ Error: --edition ent-hsm requires --mode prod.")
			fmt.Println("   💡 Deploy with: hal vault create --edition ent-hsm --mode prod")
			return
		}

		// Production mode is Enterprise-only. It implies --edition ent unless the
		// caller selected the HSM-capable Enterprise runtime explicitly.
		if vaultMode == "prod" {
			if cmd.Flags().Changed("edition") && !isEnterpriseEdition(vaultEdition) {
				fmt.Println("❌ Error: --mode prod requires Vault Enterprise; '--edition ce' is incompatible.")
				fmt.Println("   💡 Drop --edition (prod implies Enterprise) or pass --edition ent.")
				return
			}
			if !isHSMEdition {
				vaultEdition = "ent"
			}
		} else if vaultMode != "" && vaultMode != "dev" {
			fmt.Printf("❌ Error: unknown --mode %q (expected 'dev' or 'prod').\n", vaultMode)
			return
		}

		// Determine the source image and version before the license gate so invalid
		// HSM tags fail without prompting for a license.
		imageRepo := defaultVaultImageCE
		actualVersion := vaultVersion
		if isEnterpriseEdition(vaultEdition) {
			imageRepo = defaultVaultImageEnt
			if isHSMEdition {
				if !cmd.Flags().Changed("vault-tag") {
					actualVersion = defaultVaultEntHSMTag
				} else if !isHSMTag(actualVersion) {
					fmt.Printf("❌ Error: --edition ent-hsm requires an HSM-enabled Vault tag; %q does not end in .hsm.\n", actualVersion)
					return
				}
			} else if !cmd.Flags().Changed("vault-tag") {
				actualVersion = defaultVaultEntTag
			}
		}
		// --vault-image is the source repository. For ent-hsm, the Vault binary is
		// extracted from this image into the local SoftHSM runtime image.
		if vaultImage != "" {
			imageRepo = vaultImage
		}
		if vaultMode == "prod" && !isHSMEdition && isEnterpriseEdition(vaultEdition) && isHSMTag(actualVersion) {
			fmt.Println("ℹ️  HSM-enabled Vault tag detected; use --edition ent-hsm to include the SoftHSM2 runtime.")
		}

		// THE NEW LICENSE CHECK
		if isEnterpriseEdition(vaultEdition) {
			license := os.Getenv("VAULT_LICENSE")
			licensePath := os.Getenv("VAULT_LICENSE_PATH")

			// If VAULT_LICENSE_PATH is set, read the license from file
			if license == "" && licensePath != "" {
				licenseBytes, err := os.ReadFile(licensePath)
				if err != nil {
					fmt.Printf("❌ Error: Failed to read license from VAULT_LICENSE_PATH: %v\n", err)
					return
				}
				license = string(licenseBytes)
			}

			// Nothing in the environment. On an interactive terminal, offer to
			// take the license (or a path to a .hclic) inline instead of failing.
			if license == "" {
				license = promptForVaultLicense()
			}

			if strings.TrimSpace(license) == "" {
				fmt.Println("❌ Error: Vault Enterprise requested but no license found.")
				fmt.Println("   💡 Provide it one of these ways:")
				fmt.Println("      export VAULT_LICENSE='your_license_string'")
				fmt.Println("      export VAULT_LICENSE_PATH='/path/to/vault.hclic'")
				fmt.Println("      or re-run interactively and paste it when prompted.")
				return
			}
			license = strings.TrimSpace(license)

			// Store in environment for container injection
			os.Setenv("VAULT_LICENSE", license)
		}

		if vaultUpdate {
			if global.Debug {
				fmt.Println("[DEBUG] --update detected. Reconciling Vault by replacing runtime artifacts...")
			}
			_ = exec.Command(engine, "rm", "-f", vaultContainer).Run()
			_ = exec.Command(engine, "volume", "rm", "-f", vaultLogsVolume).Run()
			_ = exec.Command(engine, "volume", "rm", "-f", vaultPluginsVolume).Run()
			_ = exec.Command(engine, "volume", "rm", "-f", vaultDataVolume).Run()
		}

		// Production mode diverges entirely: real server -config, Raft storage,
		// TLS, and auto init+unseal. See prod.go.
		if vaultMode == "prod" {
			imageRef := fmt.Sprintf("%s:%s", imageRepo, actualVersion)
			if isHSMEdition {
				runtimeRef := vaultSoftHSMRuntimeImage + ":" + vaultSoftHSMRuntimeTag
				imagePresent := exec.Command(engine, "image", "inspect", runtimeRef).Run() == nil
				shouldBuild := vaultUpdate || !imagePresent ||
					cmd.Flags().Changed("vault-tag") || cmd.Flags().Changed("vault-image") ||
					cmd.Flags().Changed("softhsm-base-image") || cmd.Flags().Changed("softhsm-base-tag")
				if shouldBuild {
					baseRef := softHSMBaseImage + ":" + softHSMBaseTag
					if global.DryRun {
						fmt.Printf("[DRY RUN] Would build SoftHSM runtime image %s (source: %s, base: %s)\n", runtimeRef, imageRef, baseRef)
					} else {
						fmt.Printf("🔨 Building SoftHSM runtime image %s (source: %s)...\n", runtimeRef, imageRef)
						if err := buildSoftHSMImage(engine, imageRef, baseRef); err != nil {
							fmt.Printf("❌ Failed to build SoftHSM image: %v\n", err)
							return
						}
						fmt.Printf("  ✅ Runtime image %s built.\n", runtimeRef)
					}
				}
				imageRef = runtimeRef
			}
			runVaultProd(engine, imageRef, actualVersion, isHSMEdition)
			return
		}

		ui.LogoStart("vault", 4)
		defer ui.LogoStop()
		ui.LogoStep("Deploying Vault %s (%s) via %s", actualVersion, strings.ToUpper(vaultEdition), engine)

		// 1. Ensure the global HAL network exists
		global.EnsureNetwork(engine)

		// Correction des permissions du volume d'audit pour l'utilisateur Vault (UID 100)
		ui.LogoStep("Preparing shared volume permissions")
		helperRef := vaultHelperImage + ":" + vaultHelperTag
		_ = exec.Command(engine, "run", "--rm", "-v", vaultLogsVolume+":/vault/logs", helperRef, "chown", "-R", "100:1000", "/vault/logs").Run()
		_ = exec.Command(engine, "run", "--rm", "-v", vaultPluginsVolume+":/vault/plugins", helperRef, "sh", "-c", "mkdir -p /vault/plugins && chown -R 100:1000 /vault/plugins").Run()
		_ = exec.Command(engine, "run", "--rm", "-v", vaultDataVolume+":/vault/file", helperRef, "chown", "-R", "100:1000", "/vault/file").Run()

		// 2. Build the Docker run arguments
		vaultArgs := []string{
			"run", "-d",
			"--name", vaultContainer,
			"--network", global.HalNetName,
			"--cap-add", "IPC_LOCK",
			"-p", fmt.Sprintf("%d:%d", vaultHTTPPort, vaultHTTPPort),
			"-v", vaultLogsVolume + ":/vault/logs",
			"-v", vaultPluginsVolume + ":/vault/plugins",
			"-v", vaultDataVolume + ":/vault/file",
		}

		// Vault 2.x tries to set SETFCAP capability which fails on Docker Desktop.
		// Setting SKIP_SETCAP=true tells Vault to skip this step (safe for dev mode).
		if strings.HasPrefix(actualVersion, "2.") {
			vaultArgs = append(vaultArgs, "-e", "SKIP_SETCAP=true")
		}

		// Set plugin directory for external plugins (like OS secret engine)
		vaultArgs = append(vaultArgs, "-e", "VAULT_PLUGIN_DIR=/vault/plugins")

		// Inject the Enterprise License (we already know it exists thanks to the pre-flight check)
		if isEnterpriseEdition(vaultEdition) {
			ui.LogoStep("Injecting VAULT_LICENSE into container")
			vaultArgs = append(vaultArgs, "-e", fmt.Sprintf("VAULT_LICENSE=%s", os.Getenv("VAULT_LICENSE")))
		}

		// Inject the Consul Tether
		if vaultJoinConsul {
			ui.LogoStep("Tethering Vault to the global HAL Consul")
			vaultArgs = append(vaultArgs, "-e", "CONSUL_HTTP_ADDR=http://hal-consul:8500")
		}

		// Append the image and the Vault Dev mode commands
		vaultArgs = append(vaultArgs,
			fmt.Sprintf("%s:%s", imageRepo, actualVersion),
			"server", "-dev", fmt.Sprintf("-dev-listen-address=0.0.0.0:%d", vaultHTTPPort), "-dev-root-token-id="+vaultRootToken, "-dev-plugin-dir=/vault/plugins",
		)

		if global.DryRun {
			ui.LogoStop()
			fmt.Printf("[DRY RUN] Would execute: %s %s\n", engine, strings.Join(vaultArgs, " "))
			return
		}

		ui.LogoStep("Starting Vault container")
		out, err := exec.Command(engine, vaultArgs...).CombinedOutput()
		if err != nil {
			ui.LogoStop()
			if strings.Contains(string(out), "AlreadyExists") || strings.Contains(string(out), "already in use") {
				fmt.Println("⚠️  Vault already exists. Use '--update' to reconcile it.")
				return
			}
			fmt.Printf("❌ Failed to start Vault: %s\n", string(out))
			return
		}

		// 3. THE HEALTH CHECK PHASE
		ui.LogoStep("Waiting for Vault to initialize")

		if err := waitForService("Vault", vaultHealthURL, 30); err != nil {
			ui.LogoStop()
			handleDockerFailure(vaultContainer, engine)
			return
		}

		ui.LogoStop()
		global.RefreshHalHealth(engine)
		ui.Success("Vault is up and running in Dev mode!")
		ui.Section("Connection")
		ui.Field("Edition", strings.ToUpper(vaultEdition))
		ui.Field("UI", vaultPublicURL)
		ui.Field("Token", vaultRootToken)

		if vaultJoinConsul {
			ui.Item("🟢 Tethered to the global Consul Control Plane")
		}

		ui.Section("Use your local CLI")
		ui.Item("export VAULT_ADDR='%s'", vaultPublicURL)
		ui.Item("export VAULT_TOKEN='%s'", vaultRootToken)
	},
}

// waitForService pings the URL every 2 seconds until it gets an HTTP 200 or hits the timeout limit
func waitForService(name string, url string, maxRetries int) error {
	client := http.Client{Timeout: 2 * time.Second}

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

// handleDockerFailure pulls the container logs directly to diagnose the crash
func handleDockerFailure(container string, engine string) {
	fmt.Printf("❌ %s failed to start or become healthy.\n", container)
	fmt.Println("📜 Fetching recent container logs...")

	out, _ := exec.Command(engine, "logs", "--tail", "20", container).CombinedOutput()
	logStr := strings.TrimSpace(string(out))

	if logStr != "" {
		fmt.Println("----------------- CONTAINER LOGS -----------------")
		fmt.Println(logStr)
		fmt.Println("--------------------------------------------------")
	} else {
		fmt.Println("(No logs found)")
	}
	fmt.Println("⚠️  Deployment halted. Run 'hal vault delete' to clean up the broken resources.")
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Reconcile an existing Vault instance",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		vaultUpdate = true
		deployCmd.Run(cmd, args)
	},
}

func bindLifecycleFlags(cmd *cobra.Command, includeUpdate bool) {
	cmd.Flags().StringVarP(&vaultVersion, "vault-tag", "v", defaultVaultTag, "Vault container image tag")
	cmd.Flags().StringVar(&vaultImage, "vault-image", "", "Vault source image name (overrides per-edition default; ent-hsm extracts the Vault binary from it)")
	cmd.Flags().StringVarP(&vaultEdition, "edition", "e", defaultVaultEdition, "Vault edition to deploy: 'ce', 'ent', or 'ent-hsm' (Enterprise HSM build + SoftHSM2 runtime; requires --mode prod)")
	cmd.Flags().StringVar(&vaultMode, "mode", defaultVaultMode, "Deployment mode: 'dev' (in-memory, auto-unsealed, HTTP) or 'prod' (persistent single-node Raft, TLS, initialized+unsealed; implies --edition ent)")
	cmd.Flags().IntVar(&vaultKeyShares, "key-shares", defaultVaultKeyShares, "[prod] Number of unseal key shares to generate at operator init")
	cmd.Flags().IntVar(&vaultKeyThreshold, "key-threshold", defaultVaultKeyThreshold, "[prod] Number of unseal key shares required to unseal")
	cmd.Flags().StringVar(&vaultProdNodeID, "node-id", defaultVaultProdNodeID, "[prod] Raft node identifier for the single-node cluster")
	cmd.Flags().StringVar(&vaultHelperImage, "vault-helper-image", defaultVaultHelperImage, "Helper container image name for one-shot setup tasks during Vault deploy")
	cmd.Flags().StringVar(&vaultHelperTag, "vault-helper-tag", defaultVaultHelperTag, "Helper container image tag for one-shot setup tasks during Vault deploy")
	cmd.Flags().StringVar(&softHSMBaseImage, "softhsm-base-image", defaultSoftHSMBaseImage, "Base image for the SoftHSM runtime build (must be glibc-based)")
	cmd.Flags().StringVar(&softHSMBaseTag, "softhsm-base-tag", defaultSoftHSMBaseTag, "Base image tag for the SoftHSM runtime build")
	if includeUpdate {
		cmd.Flags().BoolVarP(&vaultUpdate, "update", "u", false, "Reconcile an existing Vault deployment in place")
	}
	cmd.Flags().BoolVarP(&vaultJoinConsul, "join-consul", "c", false, "Tether Vault to the global HAL Consul instance")
}

func isEnterpriseEdition(edition string) bool {
	return edition == "ent" || edition == "enterprise" || edition == "ent-hsm"
}

func init() {
	bindLifecycleFlags(deployCmd, true)
	bindLifecycleFlags(updateCmd, false)
	Cmd.AddCommand(deployCmd)
	Cmd.AddCommand(updateCmd)
}
