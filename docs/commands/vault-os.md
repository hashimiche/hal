# HAL Vault OS Command Spec

## Command
- `hal vault os`

## Purpose
Deploy an Ubuntu VM via Multipass and configure Vault's OS secret engine to manage and rotate local Linux user passwords on the VM.

## Related
- Parent namespace: [vault.md](vault.md)

## Prerequisites
- HAL CLI is available in your local environment
- Multipass is installed and running (https://multipass.run/install)
- **Vault Enterprise 2.0+** must be running and healthy (OS secret engine is Enterprise-only)
- Valid Vault Enterprise license (`VAULT_LICENSE` or `VAULT_LICENSE_PATH`)

## Flags
```text
      --ubuntu-image string   Ubuntu image for Multipass VM (default "22.04")
      --vm-cpus string        Number of CPUs for the VM (default "1")
      --vm-mem string         Amount of RAM for the VM (default "1G")
  -h, --help                  help for os
```
- Global flags: `--debug`, `--dry-run`, `--verbose`

## Side Effects
- Creates a Multipass VM named `hal-vault-os`
- Creates three local users on the VM: `mgmt-user`, `demouser`, `appadmin`
- Mounts the OS secret engine plugin at `os/` in Vault
- Registers a host (`demo-vm`) and three accounts in Vault

## Lifecycle Actions

### Status (Default)
```bash
hal vault os
```
Displays:
- VM existence and IP address
- Vault OS secret engine mount status
- Suggested next steps

### Enable
```bash
hal vault os enable
```
1. Launches Ubuntu 22.04 VM via Multipass
2. Creates `mgmt-user` (privileged parent), `demouser`, and `appadmin` on the VM
3. Grants `mgmt-user` passwordless sudo for `/usr/sbin/chpasswd` only
4. Enables SSH password authentication on the VM
5. Verifies network connectivity from the Vault container to the VM
6. Registers and mounts the `vault-plugin-secrets-os` plugin
7. Configures TOFU host key verification
8. Registers the host and all three accounts in Vault
9. Performs a test password rotation for `demouser` to verify end-to-end connectivity

### Disable
```bash
hal vault os disable
```
1. Revokes all OS secret engine leases
2. Unmounts the `os/` secrets engine
3. Deletes the `hal-vault-os` Multipass VM and purges its cache

### Update
```bash
hal vault os update
```
Runs disable followed by enable — full teardown and rebuild.

## Example Workflows

### Basic Setup
```bash
# Deploy Vault Enterprise first
export VAULT_LICENSE_PATH=/path/to/vault.hclic
hal vault create --edition ent

# Deploy OS secret engine integration
hal vault os enable

# Check status
hal vault os
```

### Password Rotation Demo
```bash
# Manually rotate demouser's password
vault write -f os/hosts/demo-vm/accounts/demouser/rotate

# Read the new password
vault read os/hosts/demo-vm/accounts/demouser/creds

# Read appadmin's current password
vault read os/hosts/demo-vm/accounts/appadmin/creds

# Shell into the VM to verify
multipass shell hal-vault-os
```

### Cleanup
```bash
hal vault os disable
```

## How It Works

The OS secret engine uses **parent-managed rotation**:

1. Vault SSHes into the VM as `mgmt-user` using its stored password
2. Vault runs `echo "<user>:<newpass>" | sudo /usr/sbin/chpasswd` on the VM
3. `mgmt-user` has passwordless sudo scoped to only `/usr/sbin/chpasswd`
4. The new password is versioned and stored in Vault

This is more reliable than self-managed rotation (which uses a PTY + `passwd` expect workflow) and is the recommended approach for production-like demos.

## Accounts

| Account    | Role           | Purpose                                      |
|------------|----------------|----------------------------------------------|
| `mgmt-user`  | Parent account | SSHes in and rotates other users' passwords  |
| `demouser`   | Target account | Managed by `mgmt-user`                       |
| `appadmin`   | Target account | Managed by `mgmt-user`                       |

## Technical Details

### VM Configuration
- **Name**: `hal-vault-os`
- **OS**: Ubuntu 22.04 LTS (configurable via `--ubuntu-image`)
- **Resources**: 1 CPU, 1GB RAM (configurable)
- **Network**: Multipass default network

### Vault Configuration
- **Plugin**: `vault-plugin-secrets-os` v`0.1.0+ent`
- **Mount Path**: `os/`
- **Host Key Verification**: Trust-on-first-use (TOFU)
- **Connection**: SSH with password authentication

### sudoers
```
mgmt-user ALL=NOPASSWD:/usr/sbin/chpasswd
```

## Troubleshooting

### Multipass Not Found
```
❌ Error: Multipass is not installed or not running.
```
Install from https://multipass.run/install

### Vault Not Enterprise
```
❌ Error: The OS secret engine requires Vault Enterprise.
```
Deploy Enterprise: `hal vault delete && hal vault create --edition ent`

### Vault Container Can't Reach VM
```
❌ The Vault container cannot reach <IP>:22
```
This is a Docker-to-Multipass networking issue on macOS. Verify Docker Desktop can route to the Multipass subnet:
```bash
docker exec hal-vault nc -z <VM_IP> 22
```

### Rotation Fails After Enable
Check that SSH password auth is active on the VM:
```bash
multipass exec hal-vault-os -- sudo sshd -T | grep passwordauthentication
# Should show: passwordauthentication yes
```
If it shows `no`, run `hal vault os update` to rebuild with the corrected config.

### VM Already Exists
```
⚠️  VM 'hal-vault-os' already exists. Use 'hal vault os update' to reconcile.
```
Run `hal vault os update` to tear down and rebuild cleanly.

## Related Commands
- `hal vault create` — Deploy Vault instance
- `hal vault database` — Database secrets engine integration
- `hal vault ldap` — LDAP secrets engine integration
- `hal nomad create` — Also uses Multipass for VM management
- `hal boundary ssh` — Also uses Multipass for SSH target demo
