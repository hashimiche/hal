package boundary

import (
	"fmt"
	"os/exec"
	"strings"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var (
	mariadbEnable    bool
	mariadbDisable   bool
	mariadbForce     bool
	targetMariadbVer string
)

var mariadbCmd = &cobra.Command{
	Use:   "mariadb",
	Short: "Deploy a dummy MariaDB Database as a Boundary Target",
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		// ==========================================
		// 1. SMART STATUS MODE
		// ==========================================
		if !mariadbEnable && !mariadbDisable && !mariadbForce {
			fmt.Println("🔍 Checking Boundary MariaDB Target Status...")
			fmt.Println()

			out, err := exec.Command(engine, "inspect", "-f", "{{.State.Status}}", "hal-boundary-target-mariadb").Output()
			status := strings.TrimSpace(string(out))

			if err != nil {
				fmt.Println("  ❌ MariaDB Target : Not deployed")
				fmt.Println("\n💡 Next Step:")
				fmt.Println("   hal boundary mariadb --enable")
			} else if status == "running" {
				fmt.Println("  ✅ MariaDB Target : Active (hal-boundary-target-mariadb:3306)")
				fmt.Println("\n💡 Next Step:")
				fmt.Println("   hal boundary mariadb --disable")
			} else {
				fmt.Printf("  ⚠️  MariaDB Target : %s\n", strings.ToUpper(status))
				fmt.Println("\n💡 Next Step:")
				fmt.Println("   hal boundary mariadb --force")
			}
			return
		}

		// ==========================================
		// 2. TEARDOWN PATH
		// ==========================================
		if mariadbDisable || mariadbForce {
			if mariadbDisable {
				fmt.Println("🛑 Tearing down MariaDB Target...")
			} else {
				fmt.Println("♻️  Force flag detected. Resetting MariaDB Target...")
			}
			_ = exec.Command(engine, "rm", "-f", "hal-boundary-target-mariadb").Run()

			if mariadbDisable {
				fmt.Println("✅ MariaDB Target removed successfully.")
				return
			}
		}

		// ==========================================
		// 3. DEPLOY PATH
		// ==========================================
		if mariadbEnable || mariadbForce {
			fmt.Printf("🚀 Deploying dummy MariaDB Target (mariadb:%s)...\n", targetMariadbVer)

			global.EnsureNetwork(engine)

			dbArgs := []string{
				"run", "-d",
				"--name", "hal-boundary-target-mariadb",
				"--network", "hal-net",
				"-p", "3306:3306",
				"-e", "MARIADB_ROOT_PASSWORD=targetroot",
				"-e", "MARIADB_DATABASE=targetdb",
				"-e", "MARIADB_USER=admin",
				"-e", "MARIADB_PASSWORD=targetpass",
				fmt.Sprintf("mariadb:%s", targetMariadbVer),
			}

			_, dbErr := exec.Command(engine, dbArgs...).CombinedOutput()
			if dbErr == nil {
				fmt.Println("✅ MariaDB Target ready!")
				fmt.Println("   Host: hal-boundary-target-mariadb:3306")
				fmt.Println("   User: admin")
				fmt.Println("   Pass: targetpass")
			} else {
				fmt.Println("❌ Failed to start Target Database container.")
			}
		}
	},
}

func init() {
	mariadbCmd.Flags().BoolVarP(&mariadbEnable, "enable", "e", false, "Deploy the MariaDB Target")
	mariadbCmd.Flags().BoolVarP(&mariadbDisable, "disable", "d", false, "Remove the MariaDB Target")
	mariadbCmd.Flags().BoolVarP(&mariadbForce, "force", "f", false, "Force a clean redeployment")
	mariadbCmd.Flags().StringVar(&targetMariadbVer, "mariadb-version", "11.4", "MariaDB version for the target")

	Cmd.AddCommand(mariadbCmd)
}
