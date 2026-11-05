# HAL Boundary Logs Command Spec

## Command
- `hal boundary logs [--tail N] [--component NAME]`

## Purpose
Print the most recent log lines of one of the containers `hal boundary delete` owns.

Read-only: the command never follows the stream, never starts, stops or changes anything, and is annotated `hal.mcp/readonly: "true"` (ADR 0003, decisions 3 and 8) so the MCP `hal_read` tool may run it. It replaces running `podman logs` by hand.

## Behavior
- Detects the container engine with the same probe as every other command (`docker info`, then `podman info`), so Docker and Podman both work.
- Reads the component's state with `<engine> inspect`, then runs `<engine> logs --tail N <container>`.
- Missing container: prints `<container> is not running: no container named <container> on <engine>. Check 'hal boundary status', or start it with 'hal boundary create'.` on stderr and exits `1`.
- Stopped container (exited, created, paused): its last logs are still printed, which is what you want after a crash. A note on stderr says it is not running; exit `0`.
- An engine failure that is not "no such container" (daemon unreachable, permission denied) is reported as that failure, never as "not running".
- Without `--component` the default container (`hal-boundary`) is read. `--component` takes the container name without its `hal-` prefix; the full container name is accepted too.
- Bounded output: `--tail` defaults to 100 lines and is capped at 1000 (a larger value is clamped, with a note on stderr; `0` or a negative value is an error). The printed text is also capped at 256 KiB, keeping the most recent part and starting on a line boundary.
- Log lines go to stdout and nothing else does. Every note and error goes to stderr, so a pipe sees only log lines. Container applications usually log to stderr; both streams are merged into stdout.
- Exit code: `0` when logs were printed (or the component has no output yet), `1` for every failure.

## Related
- Parent namespace: [boundary.md](boundary.md)
- Status: `hal boundary status`

## Prerequisites
- HAL CLI is available in your local environment.
- Docker or Podman is running, and the component has been created at least once.

## Components
| `--component` | Container | What it is |
|---|---|---|
| `boundary` | `hal-boundary` | controller and worker (default) |
| `boundary-backend` | `hal-boundary-backend` | PostgreSQL backend |
| `boundary-target-mariadb` | `hal-boundary-target-mariadb` | MariaDB target |

The `hal-boundary-ssh` Multipass VM is a demo target, not a service, so it is not a log source.

## Flags
- Command flags from `hal boundary logs --help`:
```text
    --component string   Which container to read (default "boundary")
-h, --help               help for logs
    --tail int           Number of most recent log lines to print (max 1000) (default 100)
```
- Global flags: `--debug`, `--dry-run`, `--verbose`

## Side Effects
- None. The command is read-only. It runs `inspect` and `logs` on the engine and writes nothing.

## Example
```bash
hal boundary logs
hal boundary logs --tail 20
hal boundary logs --component boundary-backend --tail 50
```
