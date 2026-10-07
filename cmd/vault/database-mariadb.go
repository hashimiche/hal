package vault

// database-mariadb.go — the shared hal-vault-mariadb container (ADR 0004)
//
// hal-vault-mariadb is a shared service counted per consumer, like Authentik and
// GitLab. `hal vault database` (mariadb backend) and the Agentic IAM lab both use
// it, each with its own Vault mount and its own broker user, so disabling one
// never breaks the other. Consumer keys live in internal/global
// (global.VaultMariaDBConsumer*).
//
// Rules for a consumer:
//
//  1. Call ensureVaultMariaDB on enable and releaseVaultMariaDB on disable. Never
//     run or remove the container directly.
//  2. Administer it as root through runVaultMariaDBSQL. Root keeps the fixed
//     vaultMariaDBRootPassword for the container's whole life: give Vault a
//     broker user of your own, so rotate-root only ever rotates that broker.
//  3. Before releasing, drop your Vault mount and what you created inside
//     MariaDB (broker user, schema). Release never touches other consumers' state.
//  4. Handle global.DryRun before calling; these helpers always act.

import (
	"fmt"
	"os/exec"
	"strings"

	"hal/internal/global"
)

// ensureVaultMariaDB makes sure hal-vault-mariadb accepts connections, then
// registers consumer. A running container is reused as is and a stopped one is
// restarted, so the other consumers' users and schemas survive; image is then
// ignored. Only a missing (or unstartable) container is started fresh from
// image, and its registry is then reset before consumer is added: the consumers
// it listed lost their state with the old container. Returns true when an
// existing container was reused.
func ensureVaultMariaDB(engine, image, consumer string) (bool, error) {
	reused := false
	state, err := exec.Command(engine, "inspect", "-f", "{{.State.Running}}", vaultMariaDBContainer).Output()
	switch {
	case err != nil:
		// Missing: started fresh below.
	case strings.TrimSpace(string(state)) == "true":
		reused = true
	default:
		fmt.Printf("♻️  Restarting stopped %s...\n", vaultMariaDBContainer)
		if out, startErr := exec.Command(engine, "start", vaultMariaDBContainer).CombinedOutput(); startErr == nil {
			reused = true
		} else {
			fmt.Printf("⚠️  Could not restart %s, recreating it: %s\n", vaultMariaDBContainer, strings.TrimSpace(string(out)))
			_ = exec.Command(engine, "rm", "-f", vaultMariaDBContainer).Run()
		}
	}

	if reused {
		fmt.Printf("🔗 Reusing shared %s...\n", vaultMariaDBContainer)
	} else {
		fmt.Printf("🚀 Booting shared %s...\n", vaultMariaDBContainer)
		global.EnsureNetwork(engine)
		args := []string{
			"run", "-d", "--name", vaultMariaDBContainer,
			"--network", global.HalNetName,
			"--network-alias", vaultMariaDBHostAlias,
			"-p", fmt.Sprintf("%d:%d", vaultMariaDBPort, vaultMariaDBPort),
			"-e", "MARIADB_ROOT_PASSWORD=" + vaultMariaDBRootPassword,
			image,
		}
		if out, runErr := exec.Command(engine, args...).CombinedOutput(); runErr != nil {
			return false, fmt.Errorf("failed to start %s: %v\n%s", vaultMariaDBContainer, runErr, strings.TrimSpace(string(out)))
		}
		if err := global.ClearSharedService(global.SharedVaultMariaDBServiceKey); err != nil {
			fmt.Printf("⚠️  Could not reset shared service registry: %v\n", err)
		}
	}

	fmt.Printf("⏳ Waiting for %s to accept connections...\n", vaultMariaDBContainer)
	if err := waitForMariaDB(engine, vaultMariaDBContainer, 30); err != nil {
		return reused, fmt.Errorf("%s did not accept connections in time (check: %s logs %s)", vaultMariaDBContainer, engine, vaultMariaDBContainer)
	}

	if err := global.AddSharedServiceConsumer(global.SharedVaultMariaDBServiceKey, consumer); err != nil {
		fmt.Printf("⚠️  Could not register shared service consumer: %v\n", err)
	}
	return reused, nil
}

// releaseVaultMariaDB unregisters consumer and removes hal-vault-mariadb once no
// other consumer remains. Returns the consumers still registered; the container
// is kept while that list is not empty.
func releaseVaultMariaDB(engine, consumer string) []string {
	remaining, err := global.RemoveSharedServiceConsumer(global.SharedVaultMariaDBServiceKey, consumer)
	if err != nil {
		fmt.Printf("⚠️  Could not update shared service registry: %v\n", err)
	}

	if len(remaining) > 0 {
		fmt.Printf("  ℹ️  %s still in use by: %s — container kept\n", vaultMariaDBContainer, strings.Join(remaining, ", "))
		return remaining
	}

	fmt.Printf("⚙️  Removing %s container (no other consumers)...\n", vaultMariaDBContainer)
	_ = exec.Command(engine, "rm", "-f", vaultMariaDBContainer).Run()
	return remaining
}

// runVaultMariaDBSQL runs sql as root inside hal-vault-mariadb. It works on a
// reused container because root's password is the fixed vaultMariaDBRootPassword
// whoever started it.
func runVaultMariaDBSQL(engine, sql string) error {
	out, err := exec.Command(engine, "exec", vaultMariaDBContainer,
		"mariadb", "-u", "root", "-p"+vaultMariaDBRootPassword, "-e", sql).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
