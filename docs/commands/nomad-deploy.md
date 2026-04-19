# HAL Nomad Deploy Command Spec

## Command
- `hal nomad create`

## Purpose
Deploy local Nomad cluster resources via Multipass.

## Related
- Parent namespace: [nomad.md](nomad.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Deprecated: older HAL docs may reference `hal nomad create --force`. That flag has been removed from the CLI. Use `hal nomad update` or `hal nomad create --update`.
- Command flags from `hal nomad create --help`:
```text
--configure-obs         Refresh Prometheus target and Grafana dashboard artifacts without redeploying Nomad
--cpus string           Number of CPUs for the VM (default "2")
-u, --update                Reconcile an existing Nomad deployment in place
-h, --help                  help for deploy
-c, --join-consul           Tether Nomad to the global HAL Consul instance
--mem string            Amount of RAM for the VM (default "2G")
--ubuntu-image string   Multipass image/channel used for the Nomad VM (default "22.04")
-v, --version string        Nomad version to install (default "1.11.3")
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal nomad create
```
