# HAL Vault Deploy Command Spec

## Command
- `hal vault create`

## Purpose
Deploy a local Vault instance and baseline configuration for HAL labs.

## Related
- Parent namespace: [vault.md](vault.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Deployment modes
`hal vault create` supports two modes via `--mode` (default `dev`):

- **`dev`** (default): boots `vault server -dev` — in-memory storage, auto-unsealed,
  plaintext HTTP at `http://vault.localhost:8200`, well-known `root` token.
  `--edition ent` uses the Enterprise image but still runs in dev mode.
- **`prod`**: boots a real `vault server -config` — **single-node integrated Raft**
  on a persistent volume, **TLS** at `https://vault.localhost:8200` (self-signed
  cert forged under `~/.hal/vault-prod/certs/`), and automatic `operator init` +
  `unseal`. Implies `--edition ent` and requires `VAULT_LICENSE` /
  `VAULT_LICENSE_PATH`. The generated unseal key + root token are cached at
  `~/.hal/vault-prod/init.json` (mode `0600`) and surfaced by `hal vault status`
  and `hal creds status`. `--edition ce --mode prod` is rejected.

## Flags
- Deprecated: older HAL docs may reference `hal vault create --force` or `hal vault create --edition ent --force`. Those forms have been removed from the CLI. Use `hal vault update` or `hal vault create --update`.
- Command flags from `hal vault create --help`:
```text
-e, --edition string              Vault edition to deploy: 'ce' (Community) or 'ent' (Enterprise) (default "ce")
    --mode string                 Deployment mode: 'dev' (in-memory, auto-unsealed, HTTP) or 'prod' (persistent single-node Raft, TLS, initialized+unsealed; implies --edition ent) (default "dev")
    --key-shares int              [prod] Number of unseal key shares to generate at operator init (default 1)
    --key-threshold int           [prod] Number of unseal key shares required to unseal (default 1)
    --node-id string              [prod] Raft node identifier for the single-node cluster (default "hal-vault-node-1")
-u, --update                      Reconcile an existing Vault deployment in place
-h, --help                        help for create
    --vault-helper-image string   Helper container image name for one-shot setup tasks during Vault deploy (default "alpine")
    --vault-helper-tag string     Helper container image tag for one-shot setup tasks during Vault deploy (default "3.24")
    --vault-image string          Vault container image name (overrides per-edition default: hashicorp/vault or hashicorp/vault-enterprise)
-v, --vault-tag string            Vault container image tag (default "2.0.4")
-c, --join-consul                 Tether Vault to the global HAL Consul instance
```
- Global flags: `--debug`, `--dry-run`, `--verbose`

Observability artifacts are now managed explicitly with `hal vault obs <create|update|delete|status>`.

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.
- In `prod` mode it also writes `~/.hal/vault-prod/` (config, TLS certs, and the
  `init.json` holding the unseal key + root token). `hal vault delete` removes it.

## Example
```bash
# dev (default)
hal vault create

# single-node production Vault Enterprise (TLS + persistent Raft)
export VAULT_LICENSE='...'
hal vault create --mode prod
```
