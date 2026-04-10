# HAL Consul Deploy Command Spec

## Command
- `hal consul deploy`

## Purpose
Deploy a standalone local Consul server for labs/testing.

## Related
- Parent namespace: [consul.md](consul.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal consul deploy --help`:
```text
--configure-obs    Refresh Prometheus target and Grafana dashboard artifacts without redeploying Consul
-f, --force            Force redeploy
-h, --help             help for deploy
-v, --version string   Consul version to deploy (default "1.15.0")
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal consul deploy
```
