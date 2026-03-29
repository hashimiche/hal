package boundary

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var (
	sshEnable  bool
	sshDisable bool
	sshForce   bool
)

var sshCmd = &cobra.Command{
	Use:   "ssh",
	Short: "Deploy a tiny Multipass Ubuntu VM as a Boundary SSH Target",
	Run: func(cmd *cobra.Command, args []string) {

		if err := exec.Command("multipass", "version").Run(); err != nil {
			fmt.Println("❌ Error: Multipass is not installed or not running.")
			return
		}

		// ==========================================
		// 1. SMART STATUS MODE
		// ==========================================
		if !sshEnable && !sshDisable && !sshForce {
			fmt.Println("🔍 Checking Boundary SSH Target Status...")
			fmt.Println()

			out, err := exec.Command("multipass", "info", "hal-boundary-ssh", "--format", "csv").Output()
			if err != nil {
				fmt.Println("  ❌ SSH Target : Not deployed")
				fmt.Println("\n💡 Next Step:")
				fmt.Println("   hal boundary ssh --enable")
				return
			}

			if strings.Contains(string(out), "Running") {
				ip := extractMultipassIP(string(out))
				fmt.Printf("  ✅ SSH Target : Active (VM IP: %s)\n", ip)
				fmt.Println("\n💡 Next Step:")
				fmt.Println("   hal boundary ssh --disable")
			} else {
				fmt.Println("  ⚠️  SSH Target : OFFLINE/SUSPENDED")
				fmt.Println("\n💡 Next Step:")
				fmt.Println("   hal boundary ssh --force")
			}
			return
		}

		// ==========================================
		// 2. TEARDOWN PATH
		// ==========================================
		if sshDisable || sshForce {
			if sshDisable {
				fmt.Println("🛑 Tearing down SSH Target VM...")
			} else {
				fmt.Println("♻️  Force flag detected. Resetting SSH Target VM...")
			}

			_ = exec.Command("multipass", "delete", "hal-boundary-ssh").Run()
			_ = exec.Command("multipass", "purge").Run()

			if sshDisable {
				fmt.Println("✅ SSH Target removed successfully.")
				return
			}
		}

		// ==========================================
		// 3. DEPLOY PATH
		// ==========================================
		if sshEnable || sshForce {
			fmt.Println("🚀 Deploying Ubuntu VM SSH Target via Multipass (this takes a few seconds)...")

			vmArgs := []string{"launch", "22.04", "--name", "hal-boundary-ssh", "--cpus", "1", "--mem", "512M"}
			_, vmErr := exec.Command("multipass", vmArgs...).CombinedOutput()

			if vmErr == nil {
				ipOut, _ := exec.Command("multipass", "info", "hal-boundary-ssh", "--format", "csv").Output()
				ip := extractMultipassIP(string(ipOut))
				fmt.Println("✅ SSH Target ready!")
				fmt.Printf("   Host: %s (Port 22)\n", ip)
				fmt.Println("   Auth: Default ubuntu multipass key")
			} else {
				fmt.Println("❌ Failed to start SSH VM. Ensure multipass has resources.")
			}
		}
	},
}

func init() {
	sshCmd.Flags().BoolVarP(&sshEnable, "enable", "e", false, "Deploy the SSH Target")
	sshCmd.Flags().BoolVarP(&sshDisable, "disable", "d", false, "Remove the SSH Target")
	sshCmd.Flags().BoolVarP(&sshForce, "force", "f", false, "Force a clean redeployment")

	Cmd.AddCommand(sshCmd)
}
