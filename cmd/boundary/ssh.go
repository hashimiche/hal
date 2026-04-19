package boundary

import (
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

var (
	sshEnable      bool
	sshDisable     bool
	sshUpdate      bool
	sshUbuntuImage string
	sshVMCPUs      string
	sshVMMem       string
)

var sshCmd = &cobra.Command{
	Use:   "ssh [status|enable|disable|update]",
	Short: "Deploy a tiny Multipass Ubuntu VM as a Boundary SSH Target",
	Run: func(cmd *cobra.Command, args []string) {
		if err := parseLifecycleAction(args, &sshEnable, &sshDisable, &sshUpdate); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		if err := exec.Command("multipass", "version").Run(); err != nil {
			fmt.Println("❌ Error: Multipass is not installed or not running.")
			return
		}

		// ==========================================
		// 1. SMART STATUS MODE
		// ==========================================
		if !sshEnable && !sshDisable && !sshUpdate {
			fmt.Println("🔍 Checking Boundary SSH Target Status...")
			fmt.Println()

			out, err := exec.Command("multipass", "info", "hal-boundary-ssh", "--format", "csv").Output()
			if err != nil {
				fmt.Println("  ❌ SSH Target : Not deployed")
				fmt.Println("\n💡 Next Step:")
				fmt.Println("   hal boundary ssh enable")
				return
			}

			if strings.Contains(string(out), "Running") {
				ip := extractMultipassIP(string(out))
				fmt.Printf("  ✅ SSH Target : Active (VM IP: %s)\n", ip)
				fmt.Println("\n💡 Next Step:")
				fmt.Println("   hal boundary ssh disable")
			} else {
				fmt.Println("  ⚠️  SSH Target : OFFLINE/SUSPENDED")
				fmt.Println("\n💡 Next Step:")
				fmt.Println("   hal boundary ssh update")
			}
			return
		}

		// ==========================================
		// 2. TEARDOWN PATH
		// ==========================================
		if sshDisable || sshUpdate {
			if sshDisable {
				fmt.Println("🛑 Tearing down SSH Target VM and Boundary resources...")
			} else {
				fmt.Println("♻️  Update requested. Resetting SSH Target VM and Boundary resources...")
			}

			if global.DryRun {
				fmt.Println("[DRY RUN] Would clean Boundary SSH lab resources via API")
				fmt.Println("[DRY RUN] Would delete/purge Multipass VM hal-boundary-ssh")
			} else {
				if err := cleanupBoundarySSH(); err != nil {
					fmt.Printf("⚠️  Boundary cleanup warning: %v\n", err)
				}

				_ = exec.Command("multipass", "delete", "hal-boundary-ssh").Run()
				_ = exec.Command("multipass", "purge").Run()
			}

			if sshDisable {
				fmt.Println("✅ SSH Target removed successfully.")
				return
			}
		}

		// ==========================================
		// 3. DEPLOY PATH
		// ==========================================
		if sshEnable || sshUpdate {
			fmt.Println("🚀 Deploying Ubuntu VM SSH Target via Multipass (this takes a few seconds)...")
			if global.DryRun {
				fmt.Printf("[DRY RUN] Would launch Multipass VM hal-boundary-ssh (%s, %s CPU, %s RAM)\n", sshUbuntuImage, sshVMCPUs, sshVMMem)
				fmt.Println("[DRY RUN] Would configure Boundary API resources for SSH target")
				return
			}
			_ = exec.Command("multipass", "delete", "hal-boundary-ssh").Run()
			_ = exec.Command("multipass", "purge").Run()

			vmArgs := []string{"launch", sshUbuntuImage, "--name", "hal-boundary-ssh", "--cpus", sshVMCPUs, "--mem", sshVMMem}
			_, vmErr := exec.Command("multipass", vmArgs...).CombinedOutput()

			if vmErr == nil {
				ipOut, _ := exec.Command("multipass", "info", "hal-boundary-ssh", "--format", "csv").Output()
				ip := extractMultipassIP(string(ipOut))
				fmt.Println("✅ SSH Target ready!")
				fmt.Printf("   Host: %s (Port 22)\n", ip)
				fmt.Println("   Auth: Default ubuntu multipass key")

				err := bootstrapBoundarySSH(ip)
				if err != nil {
					fmt.Printf("❌ Failed to configure Boundary API: %v\n", err)
					return
				}
			} else {
				fmt.Println("❌ Failed to start SSH VM. Ensure multipass has resources.")
			}
		}
	},
}

func init() {
	sshCmd.Flags().BoolVarP(&sshEnable, "enable", "e", false, "Deploy the SSH Target")
	sshCmd.Flags().BoolVarP(&sshDisable, "disable", "d", false, "Remove the SSH Target")
	sshCmd.Flags().BoolVarP(&sshUpdate, "update", "u", false, "Reconcile SSH target VM and Boundary target wiring")
	_ = sshCmd.Flags().MarkHidden("enable")
	_ = sshCmd.Flags().MarkHidden("disable")
	_ = sshCmd.Flags().MarkHidden("update")
	sshCmd.Flags().StringVar(&sshUbuntuImage, "ubuntu-image", "22.04", "Multipass image/channel used for the SSH target VM")
	sshCmd.Flags().StringVar(&sshVMCPUs, "cpus", "1", "Number of CPUs for the SSH target VM")
	sshCmd.Flags().StringVar(&sshVMMem, "mem", "512M", "Amount of RAM for the SSH target VM")

	Cmd.AddCommand(sshCmd)
}

func newBoundaryAdminClient() (*BoundaryClient, error) {
	client := &BoundaryClient{
		Address: "http://127.0.0.1:9200",
		Client:  &http.Client{},
	}

	authID, err := client.GetDevAuthMethodID()
	if err != nil {
		return nil, fmt.Errorf("failed to get auth method: %v", err)
	}

	if err := client.Authenticate(authID, "admin", "password"); err != nil {
		return nil, err
	}

	return client, nil
}

func isBoundaryNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") || strings.Contains(msg, "doesn't exist") || strings.Contains(msg, "resource not found")
}

func isBoundaryAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already") || strings.Contains(msg, "exists") || strings.Contains(msg, "duplicate")
}

func cleanupBoundarySSH() error {
	client, err := newBoundaryAdminClient()
	if err != nil {
		return err
	}

	fmt.Println("⚙️  Cleaning Boundary SSH lab resources...")

	orgID, err := client.FindResourceIDByField("scopes", "name", "hal-academy-ssh", map[string]string{"scope_id": "global"})
	if err != nil || orgID == "" {
		return err
	}

	projID, _ := client.FindResourceIDByField("scopes", "name", "ssh-infrastructure", map[string]string{"scope_id": orgID})
	catalogID := ""
	if projID != "" {
		catalogID, _ = client.FindResourceIDByField("host-catalogs", "name", "ssh-catalog", map[string]string{"scope_id": projID})
	}

	targetID := ""
	if projID != "" {
		targetID, _ = client.FindResourceIDByField("targets", "name", "ubuntu-ssh-secure-access", map[string]string{"scope_id": projID})
	}

	authMethodID := ""
	if orgID != "" {
		authMethodID, _ = client.FindResourceIDByField("auth-methods", "name", "ssh-lab-auth", map[string]string{"scope_id": orgID})
	}

	deleteIfFound := func(endpoint, id string) {
		if id == "" {
			return
		}
		if delErr := client.DeleteResource(endpoint, id); delErr != nil && !isBoundaryNotFound(delErr) {
			fmt.Printf("⚠️  Failed to delete %s/%s: %v\n", endpoint, id, delErr)
		}
	}

	if projID != "" {
		opsRoleID, _ := client.FindResourceIDByField("roles", "name", "ssh-operator-role", map[string]string{"scope_id": projID})
		auditRoleID, _ := client.FindResourceIDByField("roles", "name", "ssh-auditor-role", map[string]string{"scope_id": projID})
		deleteIfFound("roles", opsRoleID)
		deleteIfFound("roles", auditRoleID)
	}

	if orgID != "" {
		opsUserID, _ := client.FindResourceIDByField("users", "name", "SSH Operator", map[string]string{"scope_id": orgID})
		auditUserID, _ := client.FindResourceIDByField("users", "name", "SSH Auditor", map[string]string{"scope_id": orgID})
		deleteIfFound("users", opsUserID)
		deleteIfFound("users", auditUserID)
	}

	if authMethodID != "" {
		opsAcctID, _ := client.FindResourceIDByField("accounts", "login_name", "ssh-operator", map[string]string{"auth_method_id": authMethodID})
		auditAcctID, _ := client.FindResourceIDByField("accounts", "login_name", "ssh-auditor", map[string]string{"auth_method_id": authMethodID})
		deleteIfFound("accounts", opsAcctID)
		deleteIfFound("accounts", auditAcctID)
	}

	deleteIfFound("auth-methods", authMethodID)
	deleteIfFound("targets", targetID)

	if catalogID != "" {
		hostSetID, _ := client.FindResourceIDByField("host-sets", "name", "ssh-host-group", map[string]string{"host_catalog_id": catalogID})
		hostID, _ := client.FindResourceIDByField("hosts", "name", "ubuntu-ssh-target", map[string]string{"host_catalog_id": catalogID})
		deleteIfFound("host-sets", hostSetID)
		deleteIfFound("hosts", hostID)
	}

	deleteIfFound("host-catalogs", catalogID)
	deleteIfFound("scopes", projID)
	deleteIfFound("scopes", orgID)

	return nil
}

func bootstrapBoundarySSH(targetIP string) error {
	fmt.Println("⚙️  Configuring Boundary SSH target via API...")

	client, err := newBoundaryAdminClient()
	if err != nil {
		return err
	}

	orgID, err := client.CreateOrGetResource("scopes", map[string]interface{}{
		"name": "hal-academy-ssh", "scope_id": "global",
	}, "name", map[string]string{"scope_id": "global"})
	if err != nil {
		return fmt.Errorf("failed to create org scope: %v", err)
	}

	projID, err := client.CreateOrGetResource("scopes", map[string]interface{}{
		"name": "ssh-infrastructure", "scope_id": orgID,
	}, "name", map[string]string{"scope_id": orgID})
	if err != nil {
		return fmt.Errorf("failed to create project scope: %v", err)
	}

	catalogID, err := client.CreateOrGetResource("host-catalogs", map[string]interface{}{
		"name": "ssh-catalog", "type": "static", "scope_id": projID,
	}, "name", map[string]string{"scope_id": projID})
	if err != nil {
		return fmt.Errorf("failed to create host catalog: %v", err)
	}

	hostID, err := client.CreateOrGetResource("hosts", map[string]interface{}{
		"name": "ubuntu-ssh-target", "type": "static",
		"address":         targetIP,
		"host_catalog_id": catalogID,
	}, "name", map[string]string{"host_catalog_id": catalogID})
	if err != nil {
		return fmt.Errorf("failed to create host: %v", err)
	}

	setID, err := client.CreateOrGetResource("host-sets", map[string]interface{}{
		"name": "ssh-host-group", "type": "static", "host_catalog_id": catalogID,
	}, "name", map[string]string{"host_catalog_id": catalogID})
	if err != nil {
		return fmt.Errorf("failed to create host set: %v", err)
	}
	if err := client.AddResourceAction("host-sets", setID, "add-hosts", map[string]interface{}{"host_ids": []string{hostID}}); err != nil && !isBoundaryAlreadyExists(err) {
		return fmt.Errorf("failed to add host to host set: %v", err)
	}

	targetID, err := client.CreateOrGetResource("targets", map[string]interface{}{
		"name": "ubuntu-ssh-secure-access", "type": "tcp", "default_port": 22, "scope_id": projID,
	}, "name", map[string]string{"scope_id": projID})
	if err != nil {
		return fmt.Errorf("failed to create target: %v", err)
	}
	if err := client.AddResourceAction("targets", targetID, "add-host-sources", map[string]interface{}{"host_source_ids": []string{setID}}); err != nil && !isBoundaryAlreadyExists(err) {
		return fmt.Errorf("failed to link host set to target: %v", err)
	}

	labAuthID, err := client.CreateOrGetResource("auth-methods", map[string]interface{}{
		"name": "ssh-lab-auth", "type": "password", "scope_id": orgID,
	}, "name", map[string]string{"scope_id": orgID})
	if err != nil {
		return fmt.Errorf("failed to create auth method: %v", err)
	}

	opsAcctID, err := client.CreateOrGetResource("accounts", map[string]interface{}{
		"login_name": "ssh-operator", "password": "password", "type": "password", "auth_method_id": labAuthID,
	}, "login_name", map[string]string{"auth_method_id": labAuthID})
	if err != nil {
		return fmt.Errorf("failed to create operator account: %v", err)
	}
	auditAcctID, err := client.CreateOrGetResource("accounts", map[string]interface{}{
		"login_name": "ssh-auditor", "password": "password", "type": "password", "auth_method_id": labAuthID,
	}, "login_name", map[string]string{"auth_method_id": labAuthID})
	if err != nil {
		return fmt.Errorf("failed to create auditor account: %v", err)
	}

	opsUserID, err := client.CreateOrGetResource("users", map[string]interface{}{
		"name": "SSH Operator", "scope_id": orgID,
	}, "name", map[string]string{"scope_id": orgID})
	if err != nil {
		return fmt.Errorf("failed to create operator user: %v", err)
	}
	auditUserID, err := client.CreateOrGetResource("users", map[string]interface{}{
		"name": "SSH Auditor", "scope_id": orgID,
	}, "name", map[string]string{"scope_id": orgID})
	if err != nil {
		return fmt.Errorf("failed to create auditor user: %v", err)
	}

	if err := client.AddResourceAction("users", opsUserID, "set-accounts", map[string]interface{}{"account_ids": []string{opsAcctID}}); err != nil && !isBoundaryAlreadyExists(err) {
		return fmt.Errorf("failed to attach operator account: %v", err)
	}
	if err := client.AddResourceAction("users", auditUserID, "set-accounts", map[string]interface{}{"account_ids": []string{auditAcctID}}); err != nil && !isBoundaryAlreadyExists(err) {
		return fmt.Errorf("failed to attach auditor account: %v", err)
	}

	opsRoleID, err := client.CreateOrGetResource("roles", map[string]interface{}{
		"name": "ssh-operator-role", "scope_id": projID,
	}, "name", map[string]string{"scope_id": projID})
	if err != nil {
		return fmt.Errorf("failed to create operator role: %v", err)
	}
	if err := client.AddResourceAction("roles", opsRoleID, "add-principals", map[string]interface{}{"principal_ids": []string{opsUserID}}); err != nil && !isBoundaryAlreadyExists(err) {
		return fmt.Errorf("failed to add operator principal: %v", err)
	}
	if err := client.AddResourceAction("roles", opsRoleID, "add-grants", map[string]interface{}{"grant_strings": []string{"ids=*;type=*;actions=*"}}); err != nil && !isBoundaryAlreadyExists(err) {
		return fmt.Errorf("failed to grant operator role permissions: %v", err)
	}

	auditRoleID, err := client.CreateOrGetResource("roles", map[string]interface{}{
		"name": "ssh-auditor-role", "scope_id": projID,
	}, "name", map[string]string{"scope_id": projID})
	if err != nil {
		return fmt.Errorf("failed to create auditor role: %v", err)
	}
	if err := client.AddResourceAction("roles", auditRoleID, "add-principals", map[string]interface{}{"principal_ids": []string{auditUserID}}); err != nil && !isBoundaryAlreadyExists(err) {
		return fmt.Errorf("failed to add auditor principal: %v", err)
	}
	if err := client.AddResourceAction("roles", auditRoleID, "add-grants", map[string]interface{}{"grant_strings": []string{fmt.Sprintf("ids=%s;type=target;actions=authorize-session,read", targetID)}}); err != nil && !isBoundaryAlreadyExists(err) {
		return fmt.Errorf("failed to grant auditor role permissions: %v", err)
	}

	fmt.Println("✅ Boundary SSH target successfully bootstrapped!")
	fmt.Println("\n💡 Test your access:")
	fmt.Println("   1. Log in to UI (http://boundary.localhost:9200)")
	fmt.Println("   2. Change Scope to 'hal-academy-ssh'")
	fmt.Println("   3. Auth Method: ssh-lab-auth")
	fmt.Println("   4. Login as 'ssh-operator' or 'ssh-auditor' (Password: password)")
	fmt.Println("\n🖥️  Or test via Boundary CLI:")
	fmt.Printf("   boundary connect ssh -target-id %s -auth-method-id %s -login-name ssh-operator\n", targetID, labAuthID)

	return nil
}
