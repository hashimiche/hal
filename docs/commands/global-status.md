# HAL Global Status Command Spec

## Command
- `hal status`
- `hal status --json`

## Purpose
Show a global status summary across all HAL product deployments, for people (default) or for programs (`--json`).

Read-only: annotated `hal.mcp/readonly: "true"` (ADR 0003, decisions 3 and 8) so the MCP `hal_read` tool may run it, with or without `--json`.

## Behavior
- Default: prints the human table (engine, per-product state, endpoint, version, feature rows, capacity summary) with colors and emoji.
- `--json`: prints the live lab snapshot built by `global.BuildStatusSnapshot` as a single line of JSON followed by a newline, and nothing else.
  - stdout carries only the JSON document: no banner, no ANSI escape, no progress text, no tip line.
  - Shape (unchanged, read by HAL Plus `runtime-status.mjs`): `{"timestamp", "engine", "products": [{"product", "state", "health", "reason", "endpoint", "containers", "features": [{"feature", "state", "health", "reason"}]}]}`.
  - Products in the snapshot: `consul`, `vault`, `nomad`, `boundary`, `terraform`, `obs`. This is a different list from the human table (which also shows AAP and the TFE twin).
  - Errors (no container engine, snapshot failure) go to stderr and the exit code is `1`; stdout stays empty.
  - It replaces the frozen `hal-health` sidecar snapshot: every call inspects the live lab.
- Without `--json`, a missing container engine still prints its error on stdout and exits `0` (existing behavior, unchanged).

## Related
- Parent: [global.md](global.md)

## Prerequisites
- HAL CLI is available in your local environment.
- Docker or Podman is running.

## Flags
- Command flags from `hal status --help`:
```text
-h, --help   help for status
    --json   Print the lab status snapshot as JSON (stdout carries only the JSON)
```
- Global flags: `--debug`, `--dry-run`, `--verbose`

## Side Effects
- None. The command is read-only. It runs `ps`, `inspect` and a few read-only probes (`vault audit list` in `hal-vault`, `kubectl get deployments`, `multipass info`) and writes nothing.

## Example
```bash
hal status
hal status --json
hal status --json | jq '.products[] | {product, state}'
```
