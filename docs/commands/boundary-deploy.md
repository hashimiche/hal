# HAL Boundary Deploy Command Spec

## Command
- `hal boundary create`

## Purpose
Deploy the local Boundary control plane and required services.

## Related
- Parent namespace: [boundary.md](boundary.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Deprecated: older HAL docs may reference `hal boundary create --force`. That flag has been removed from the CLI. Use `hal boundary update` or `hal boundary create --update`.
- Command flags from `hal boundary create --help`:
```text
-u, --update              Reconcile an existing Boundary deployment in place
-h, --help                help for deploy
-c, --join-consul         Tether Boundary to the global HAL Consul instance
--pg-version string   PostgreSQL version for Boundary backend (default "16")
-v, --version string      Boundary version to deploy (default "0.15.2")
```
- Global flags: `--debug`, `--dry-run`

Observability artifacts are now managed explicitly with `hal boundary obs <create|update|delete|status>`.

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal boundary create
```
