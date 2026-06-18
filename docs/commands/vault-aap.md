# HAL Vault AAP Command Spec

## Command
- `hal vault aap`

## Purpose
Manage Vault integration flows for Ansible Automation Platform installation.

## Related
- Parent namespace: [vault.md](vault.md)

## Lifecycle
- `hal vault aap` (defaults to `hal vault aap oidc status`)
- `hal vault aap oidc`
- `hal vault aap oidc enable`
- `hal vault aap oidc update`
- `hal vault aap oidc disable`

## Flags
- Command flags from `hal vault aap oidc --help`:
```text
      --aap-ca-cert-file string       Path to the AAP CA certificate inside hal-aap container (default "/home/aap/aap/tls/ca.cert")
      --aap-password string           AAP controller password (default "admin")
      --aap-username string           AAP controller username (default "admin")
      --aap-vault-lookup-type-id int  AAP credential type ID for HashiCorp Vault Secret Lookup (OIDC) (default 29)
      --aap-vault-url string          Vault URL used by AAP external secret lookup credentials (default "http://hal-vault:8200")
      --bound-audience string         JWT audience expected by Vault AAP roles (default "http://hal-vault:8200")
      --development-org string        AAP organization mapped to development Vault role (default "Development")
      --oidc-discovery-url string     AAP OIDC discovery URL used by Vault JWT auth config (default "https://hal-aap/o")
      --production-org string         AAP organization mapped to production Vault role (default "Production")
  -h, --help                          help for aap
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
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
- Reconciles AAP resources for each environment (`Development` and `Production` by default):
  - Organization
  - External credential: `Vault OIDC Base Credential - <Environment>`
  - Custom credential type: `Vault KV Lookup <Environment> Credential`
  - Credential using that type: `Vault KV Lookup <Environment> Credential Values`
  - Input source linking `env_name` to Vault KV lookup metadata
  - Project: `<Environment> Project` using `https://github.com/chrisdola/hashicorp-ansible-playbooks`
  - Job template: `<Environment> KV Demo` using playbook `print_kv.yml`, inventory `Demo Inventory`, and extra vars `{"var_names":["env_name"]}`
  - Project source-control update is triggered and HAL waits for `print_kv.yml` to appear before creating the job template
- Disable removes those objects and disables `auth/jwt-aap`.

## Workaround Note (AAP 500 on `credential_types` API)
When the `credential_types` API endpoint is broken in a given AAP build, this command uses `awx-manage shell_plus` inside `automation-controller-web` for credential type reads/writes/deletes while keeping all other operations on the REST API.

## Example
```bash
hal aap create
hal vault create
hal vault aap oidc enable
hal vault aap oidc update
hal vault aap oidc disable
```
- Disable removes those objects and disables `auth/jwt-aap`.
