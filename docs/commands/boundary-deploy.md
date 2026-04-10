# HAL Boundary Deploy Command Spec

## Command
- `hal boundary deploy`

## Purpose
Deploy the local Boundary control plane and required services.

## Related
- Parent namespace: [boundary.md](boundary.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal boundary deploy --help`:
```text
--configure-obs       Refresh Prometheus target and Grafana dashboard artifacts without redeploying Boundary
-f, --force               Force redeploy
-h, --help                help for deploy
-c, --join-consul         Tether Boundary to the global HAL Consul instance
--pg-version string   PostgreSQL version for Boundary backend (default "16")
-v, --version string      Boundary version to deploy (default "0.15.2")
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal boundary deploy
```
