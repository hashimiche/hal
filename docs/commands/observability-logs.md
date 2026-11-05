# HAL Observability Logs Command Spec

## Command
- `hal obs logs [--tail N] [--component NAME]`

## Purpose
Print the most recent log lines of one of the containers `hal obs delete` owns.

Read-only: the command never follows the stream, never starts, stops or changes anything, and is annotated `hal.mcp/readonly: "true"` (ADR 0003, decisions 3 and 8) so the MCP `hal_read` tool may run it. It replaces running `podman logs` by hand.

## Behavior
- Detects the container engine with the same probe as every other command (`docker info`, then `podman info`), so Docker and Podman both work.
- Reads the component's state with `<engine> inspect`, then runs `<engine> logs --tail N <container>`.
- Missing container: prints `<container> is not running: no container named <container> on <engine>. Check 'hal obs status', or start it with 'hal obs create'.` on stderr and exits `1`.
- Stopped container (exited, created, paused): its last logs are still printed, which is what you want after a crash. A note on stderr says it is not running; exit `0`.
- An engine failure that is not "no such container" (daemon unreachable, permission denied) is reported as that failure, never as "not running".
- Without `--component` the default container (`hal-grafana`) is read. `--component` takes the container name without its `hal-` prefix; the full container name is accepted too.
- Bounded output: `--tail` defaults to 100 lines and is capped at 1000 (a larger value is clamped, with a note on stderr; `0` or a negative value is an error). The printed text is also capped at 256 KiB, keeping the most recent part and starting on a line boundary.
- Log lines go to stdout and nothing else does. Every note and error goes to stderr, so a pipe sees only log lines. Container applications usually log to stderr; both streams are merged into stdout.
- Exit code: `0` when logs were printed (or the component has no output yet), `1` for every failure.

## Related
- Parent namespace: [observability.md](observability.md)
- Status: `hal obs status`

## Prerequisites
- HAL CLI is available in your local environment.
- Docker or Podman is running, and the component has been created at least once.

## Components
| `--component` | Container | What it is |
|---|---|---|
| `grafana` | `hal-grafana` | dashboards (default) |
| `loki` | `hal-loki` | log store |
| `prometheus` | `hal-prometheus` | metrics store |
| `promtail` | `hal-promtail` | log shipper |

## Flags
- Command flags from `hal obs logs --help`:
```text
    --component string   Which container to read (default "grafana")
-h, --help               help for logs
    --tail int           Number of most recent log lines to print (max 1000) (default 100)
```
- Global flags: `--debug`, `--dry-run`, `--verbose`

## Side Effects
- None. The command is read-only. It runs `inspect` and `logs` on the engine and writes nothing.

## Example
```bash
hal obs logs
hal obs logs --tail 20
hal obs logs --component loki --tail 50
```
