# HAL Consul Deploy Command Spec

## Command
- `hal consul create`

## Purpose
Deploy a standalone local Consul server for labs/testing.

## Related
- Parent namespace: [consul.md](consul.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Deprecated: older HAL docs may reference `hal consul create --force`. That flag has been removed from the CLI. Use `hal consul update` or `hal consul create --update`.
- Command flags from `hal consul create --help`:
```text
--configure-obs    Refresh Prometheus target and Grafana dashboard artifacts without redeploying Consul
-u, --update           Reconcile an existing Consul deployment in place
-h, --help             help for deploy
-v, --version string   Consul version to deploy (default "1.15.0")
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal consul create
```
