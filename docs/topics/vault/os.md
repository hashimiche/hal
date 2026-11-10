summary: Vault Enterprise rotates the passwords of Linux accounts on a Multipass VM, over SSH, through a parent account.
read: hal vault os | hal vault status | hal creds status
actions: hal vault os enable | hal vault os update | hal vault os disable
docs:
- Vault OS secrets engine, parent-managed rotation: https://developer.hashicorp.com/vault/docs/secrets/os#parent-managed-rotation
- OS secrets engine, SSH host key verification: https://developer.hashicorp.com/vault/docs/secrets/os#ssh-host-key-verification
- OS secrets engine, password rotation: https://developer.hashicorp.com/vault/docs/secrets/os#password-rotation

# Vault OS secrets engine

## The mechanism

A Linux server has local accounts, and their passwords tend to be set once and
forgotten. The OS secrets engine gives Vault the job of changing them. It is a
plugin, and it is Vault Enterprise only.

The engine keeps two kinds of objects. A host is a remote machine Vault can reach
over SSH, with an address and a port. An account is a local user on that host.
Vault stores the account's current password and creates a new version each time it
rotates it.

Rotation needs someone with the right to change the password. In parent-managed
rotation, a privileged account does it: Vault opens an SSH session as the parent
account and runs `sudo chpasswd` to set the new password of the target account. The
parent is stored in Vault like any other account. The target accounts point to it.
The parent needs passwordless sudo, and the Vault docs advise limiting that sudo
right to the single command `chpasswd`.

Vault also has to know it is talking to the right machine. It can be given the
host's SSH public key in advance, which is the strict way. Or it can trust the first
key the host presents and then require that same key from then on (trust on first
use, TOFU). If the key ever changes, the engine aborts the connection.

## How this lab stages it

- **The host is a Multipass virtual machine**, not a container. It is named
  `hal-vault-os`, runs Ubuntu 24.04 by default and gets one CPU and 1 GB of memory.
  `--ubuntu-image`, `--vm-cpus` and `--vm-mem` change these. Vault, in its
  container, reaches the VM over the Multipass network on port 22.
- **Three local accounts are created on the VM**: `mgmt-user`, `demouser` and
  `appadmin`. HAL gives `mgmt-user` passwordless sudo for `/usr/sbin/chpasswd`
  only, through a file under `/etc/sudoers.d`. HAL also turns on SSH password
  authentication, which Ubuntu cloud images ship switched off, by rewriting the
  image's own SSH settings file.
- **This is parent-managed rotation.** `mgmt-user` is the parent. `demouser` and
  `appadmin` are registered in Vault with `mgmt-user` as their parent, so Vault logs
  in as `mgmt-user` and runs `chpasswd` for them.
- **The engine is mounted at `os/`.** HAL registers the plugin
  `vault-plugin-secrets-os` at the pinned version `0.1.1+ent`, with Vault
  downloading it. It then registers the host `demo-vm`, whose address is the VM's
  IP, and the three accounts.
- **Host keys are trust on first use.** HAL sets the engine's config option for
  TOFU. It registers every account with connection verification off, so Vault's
  first SSH connection to the VM does not happen at registration. It happens at the
  first rotation, and that is when the key is stored.
- **The last step of `enable` is a real rotation** of `demouser`, which proves the
  chain works from Vault to the VM. The command output then shows the new password
  of that account. The lab secrets are kept out of this file.

## What to expect

- **Vault Enterprise is required.** On Community Edition `enable` stops with an
  error. The fix is a Vault created with the Enterprise edition and a license, which
  the Vault topic covers.
- **Multipass must be installed and running**, even to read the status. The
  command stops first if it is not.
- **The VM must be reachable from the Vault container.** After the VM is
  configured, HAL checks port 22 from inside the Vault container and stops with a
  hint if it cannot connect. On macOS this is usually the container engine not
  routing to the Multipass network.
- **The plugin download needs internet access**, since Vault fetches it from
  HashiCorp's releases for the pinned version. If registration fails, HAL points at
  the releases page.
- **The registration step is written for the dev-mode server.** It runs inside the
  container against plain HTTP on 127.0.0.1:8200, with the fixed dev-mode token, and
  the dev server's plugin directory is on the `hal-vault-plugins` volume. Prod mode
  has none of these, so expect `enable` to fail at plugin registration there. This
  comes from reading the code. It was not run in prod mode.
- **Disable and update rebuild the VM.** Both revoke the engine's leases, unmount
  `os/`, and then delete the VM with `multipass delete --purge hal-vault-os`, which
  purges that one VM and nothing else. HAL refuses to delete a VM whose name does not
  start with `hal-`. It does not deregister the plugin. `update` then mounts the
  engine afresh, so the TOFU step starts over against the new VM.
- **HAL only stages the parent-managed flow.** The engine can also run self-managed
  rotation, where an account changes its own password, pre-shared host keys, and
  scheduled or periodic rotation. The lab covers none of them.

## Commands

- `hal vault os` with no action is read-only. It reports whether the VM exists and
  its IP, whether the engine is mounted at `os/`, and names the next step. `hal vault
  status` lists `os` among the secrets engines.
- `enable` needs a healthy Enterprise Vault and creates everything above. It stops
  early and prints why when a prerequisite is missing.
- `update` removes what `disable` removes, then runs `enable` again. Use it when the
  status says the environment is partially degraded. If the VM `hal-vault-os`
  already exists, `enable` stops and points at `update`.
- `disable` cleans the Vault side first, so the order is right, then deletes the VM.
  It still deletes the VM when Vault is offline and skips the Vault cleanup.

The passwords of the VM accounts are lab secrets that Vault rotates. They are not
written in this file. For the Vault login, `hal creds status` shows the credentials
of whatever is running.
