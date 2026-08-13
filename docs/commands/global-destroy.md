# HAL Global Destroy Command Spec

## Command
- `hal delete`

## Purpose
Destroy all HAL-managed infrastructure globally.

## Safety
- Interactive confirmation by default
- `--auto-approve` bypasses prompt
- Supports global `--dry-run`

## Deprecated
- Older HAL docs may reference `--force` or `--yes` for non-interactive teardown. The current flag is `--auto-approve`.

## What gets removed
1. HAL KinD clusters (`kind`, `hal-*`) plus leftover `kind-control-plane` node containers (those are not named `hal-*`, so they are swept by cluster label even if the `kind` CLI is missing or `kind get clusters` fails on Podman 6's Labels-as-slice change)
2. All `hal-*` Docker/Podman containers (including TFE agents)
3. The `hal-tfe-cli:latest` helper image (best-effort)
4. HAL Multipass VMs (purged after deletion)
5. Local observability state (`~/.hal/obs/`)
6. HAL MCP config, PID file, and managed binary
7. The `hal-net` Docker/Podman network (removed after all containers are gone)

## Output statuses
Each cleanup step reports one of three statuses:
- `cleaned` — resource existed and was removed
- `not deployed` — resource was never created, nothing to do
- `clean failed` — resource could not be removed (see warnings)

## hal-net removal
`hal-net` is removed last, after all containers are gone. If non-HAL containers are still attached, the command prints their names and exits with code 1. Fix: stop or remove those containers and re-run `hal delete`.

## Related
- Parent: [global.md](global.md)

## Prerequisites
- HAL CLI is available in your local environment.

## Flags
```text
    --auto-approve   Skip confirmation prompt
-h, --help           help for delete
```
Global flags: `--debug`, `--dry-run`, `--verbose`, `--network-subnet`

## Example
```bash
hal delete
hal delete --auto-approve
```
