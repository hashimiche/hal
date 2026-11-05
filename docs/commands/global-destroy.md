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
1. The KinD cluster HAL created: the cluster named `kind` with at least one node attached to `hal-net`. If the `kind` CLI is missing or fails, that cluster's own node containers are removed through the engine. Any other KinD cluster, including a plain `kind create cluster` of your own, is **not** touched and is listed as "left alone"
2. All `hal-*` Docker/Podman containers (including `hal-tfe-agent`), plus any other container whose name or image contains `tfe-agent` or `tfc-agent` **and** that is attached to `hal-net` (the run containers HAL's TFE starts, since HAL sets `TFE_RUN_PIPELINE_DOCKER_NETWORK=hal-net`). A TFC/TFE agent you run yourself is normally not on `hal-net` and is **not** touched
3. The `hal-tfe-cli:latest` helper image and the Agentic IAM lab's `localhost/hal-agentic-iam:*` images (best-effort)
4. HAL Multipass VMs (`hal-*`), each removed with `multipass delete --purge <vm>`. No global `multipass purge` is run, so VMs you deleted yourself stay recoverable
5. Local observability state (`~/.hal/obs/`)
6. HAL MCP config, PID file, and managed binary
7. The `hal-net` Docker/Podman network (removed after all containers are gone)

## KinD discovery and ownership
HAL's cluster is named `kind`, the same as kind's default, and carries the same `io.x-k8s.kind.cluster` label as any user cluster. HAL tells them apart by `hal-net`: `ensureHALKindCluster` always attaches the node(s) to it, a plain `kind create cluster` never does. Teardown asks the container engine (`ps -a` by label, then `inspect` for the cluster label and networks), so it also works for stopped nodes and when `kind get clusters` fails on Podman 6 with an older kind CLI. That template error is not a teardown warning.

A HAL cluster whose nodes were manually detached from `hal-net` is no longer recognized; remove it with `kind delete cluster`.

## Dry run
`hal delete --dry-run` prints that only KinD clusters attached to `hal-net` are deleted and that each Multipass VM is removed with `multipass delete --purge <vm>`.

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
