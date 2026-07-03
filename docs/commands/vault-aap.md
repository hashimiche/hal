# HAL Vault AAP Command Spec

## Command
- `hal vault aap`

## Purpose
Manage the AAP runtime and Vault OIDC integration. The `enable` action starts the AAP container (if not already running), configures Vault JWT auth for AAP organization-scoped access, enables a Vault SSH secrets engine with a CA and signing role, spins up a `hal-ssh-target` demo container, and provisions per-environment SSH credentials in AAP.

## Related
- Parent namespace: [vault.md](vault.md)

## Lifecycle
- `hal vault aap` (defaults to status)
- `hal vault aap enable`
- `hal vault aap update`
- `hal vault aap disable`

## Flags
- Command flags from `hal vault aap --help`:
```text
      --aap-ca-cert-file string       Path to the AAP CA certificate inside hal-aap container (default "/home/aap/aap/tls/ca.cert")
      --aap-image string              AAP container image name (default "ubi9-aap")
      --aap-password string           AAP controller password (default "admin")
      --aap-tag string                AAP container image tag (default "latest")
      --aap-username string           AAP controller username (default "admin")
      --aap-vault-lookup-type-id int  AAP credential type ID for HashiCorp Vault Secret Lookup (OIDC) (default 29)
      --aap-vault-url string          Vault URL used by AAP external secret lookup credentials (default "http://hal-vault:8200")
      --bound-audience string         JWT audience expected by Vault AAP roles (default "http://hal-vault:8200")
      --development-org string        AAP organization mapped to development Vault role (default "Development")
      --host-port int                 Host HTTPS port to publish AAP container port 443 (default 443)
      --oidc-discovery-url string     AAP OIDC discovery URL used by Vault JWT auth config (default "https://hal-aap/o")
      --production-org string         AAP organization mapped to production Vault role (default "Production")
      --ssh-target-image string       SSH target container image name (default "ubi9")
      --ssh-target-tag string         SSH target container image tag (default "latest")
  -h, --help                          help for aap
```
- Global flags: `--debug`, `--dry-run`

## Side Effects

### Vault JWT Auth
- Enables/configures `auth/jwt-aap` in Vault.
- Writes policies:
  - `aap-development-policy`
  - `aap-production-policy`
- Writes roles:
  - `aap-development-role`
  - `aap-production-role`
- Seeds KV v2 data:
  - `secret/data/development`
  - `secret/data/production`

### Vault SSH Secrets Engine
- Enables the `ssh/` secrets engine (idempotent).
- Generates a CA key pair at `ssh/config/ca` (`generate_signing_key=true`).
- Writes the `ssh-signer` policy covering:
  - `ssh/sign/ssh-signer-role` — read/create/update
  - `ssh/issue/ssh-signer-role` — read/create/update
  - `ssh/public_key` — read
  - `ssh/config/ca` — read
- Writes the `ssh-signer-role` SSH role at `ssh/roles/ssh-signer-role`:
  - `key_type=ca`, `algorithm_signer=rsa-sha2-256`
  - `allow_user_certificates=true`, `allow_host_certificates=true`
  - `allowed_users=rhel`, `default_user=rhel`
  - `ttl=30m`, `max_ttl=1h`
- Writes per-environment JWT roles in `auth/jwt-aap`:
  - `aap-ssh-dev-role` bound to the Development AAP organization
  - `aap-ssh-prod-role` bound to the Production AAP organization
  - Both grant `token_policies=["ssh-signer"]`

### SSH Target Container (`hal-ssh-target`)
- Starts a detached `ubi9:latest` container named `hal-ssh-target` on `hal-net` with hostname `ssh-target.demo.local`.
- Installs `openssh-server` via `dnf` inside the container.
- Creates the `rhel` user.
- Creates `/etc/ssh/trusted_user_ca_keys.pub` and appends `TrustedUserCAKeys` to `sshd_config`.
- Generates host keys and starts `sshd`.
- Injects the Vault SSH CA public key into `/etc/ssh/trusted_user_ca_keys.pub` and sends `SIGHUP` to sshd so Vault-signed certificates for `rhel` are accepted.

### AAP SSH Credentials
For each environment (Development, Production):
- Generates an ephemeral 4096-bit RSA key pair in-process (never written to disk).
- Creates a **Vault Signed SSH (OIDC)** credential (`"Vault Signed SSH Credential - <Label>"`) with:
  - `url`, `auth_path=jwt-aap`, `role_id=aap-ssh-<env>-role`, `unsigned_public_key`, `valid_principals=rhel`
- Creates a **Machine Credential** (`"<Label> SSH Credential"`) with `username=rhel` and the generated private key.
- Creates a credential input source linking the `signed_key` field of the Machine Credential to the Vault Signed SSH credential.
- Attaches the Machine Credential to the `<Label> KV Demo` job template.

### AAP Inventory and Job Templates
- Creates (or reuses) an `"SSH Target Inventory"` inventory with a `hal-ssh-target` host entry pointing to `ssh-target.demo.local`.
- Updates the `Development KV Demo` and `Production KV Demo` job templates to use `"SSH Target Inventory"` (falls back to `"Demo Inventory"` with a warning if not found).
- Both job templates carry two credentials: the existing custom KV credential and the new SSH Machine Credential.

### Reconciles AAP resources for each environment
- Organization
- External credential: `Vault OIDC Base Credential - <Environment>`
- Custom credential type: `Vault KV Lookup <Environment> Credential`
- Credential using that type: `Vault KV Lookup <Environment> Credential Values`
- Input source linking `env_name` to Vault KV lookup metadata
- Project: `<Environment> Project` using `https://github.com/chrisdola/hashicorp-ansible-playbooks`
- Job template: `<Environment> KV Demo` using playbook `print_kv.yml`, inventory `SSH Target Inventory`, and extra vars `{"var_names":["env_name"]}`
- Project source-control update is triggered and HAL waits for `print_kv.yml` to appear before creating the job template

### `disable` also
- Removes SSH credential input sources, Machine Credentials, and Vault Signed SSH credentials from AAP.
- Removes AAP OIDC resources (job templates, projects, custom credential types, external credentials).
- Disables the Vault SSH engine (`ssh/`) and deletes SSH policies and roles.
- Disables `auth/jwt-aap`.
- Removes the `hal-ssh-target` container.
- Removes the `hal-aap` container.

## Workaround Note (AAP 500 on `credential_types` API)
When the `credential_types` API endpoint is broken in a given AAP build, this command uses `awx-manage shell_plus` inside `automation-controller-web` for credential type reads/writes/deletes while keeping all other operations on the REST API.

## Example
```bash
hal vault create
hal vault aap enable
hal vault aap update
hal vault aap disable
```
