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
## Flags
- Deprecated: older HAL docs may reference `hal vault create --force` or `hal vault create --edition ent --force`. Those forms have been removed from the CLI. Use `hal vault update` or `hal vault create --update`.
- Command flags from `hal vault create --help`:
```text
-e, --edition string        Vault edition to deploy: 'ce' (Community) or 'ent' (Enterprise) (default "ce")
-u, --update                Reconcile an existing Vault deployment in place
-h, --help                  help for deploy
--helper-image string   Helper image used for one-shot setup tasks during Vault deploy (default "alpine:3.22")
-c, --join-consul           Tether Vault to the global HAL Consul instance
-v, --version string        Vault version to deploy (default "2.0")
```
- Global flags: `--debug`, `--dry-run`

Observability artifacts are now managed explicitly with `hal vault obs <create|update|delete|status>`.

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal vault create
```
