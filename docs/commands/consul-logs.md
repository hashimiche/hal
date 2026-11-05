# HAL Consul Logs Command Spec

## Command
- `hal consul logs [--tail N]`

## Purpose
Print the most recent log lines of the `hal-consul` container (the whole product).

Read-only: the command never follows the stream, never starts, stops or changes anything, and is annotated `hal.mcp/readonly: "true"` (ADR 0003, decisions 3 and 8) so the MCP `hal_read` tool may run it. It replaces running `podman logs` by hand.

## Behavior
- Detects the container engine with the same probe as every other command (`docker info`, then `podman info`), so Docker and Podman both work.
- Reads the component's state with `<engine> inspect`, then runs `<engine> logs --tail N <container>`.
- Missing container: prints `<container> is not running: no container named <container> on <engine>. Check 'hal consul status', or start it with 'hal consul create'.` on stderr and exits `1`.
- Stopped container (exited, created, paused): its last logs are still printed, which is what you want after a crash. A note on stderr says it is not running; exit `0`.
- An engine failure that is not "no such container" (daemon unreachable, permission denied) is reported as that failure, never as "not running".
- Bounded output: `--tail` defaults to 100 lines and is capped at 1000 (a larger value is clamped, with a note on stderr; `0` or a negative value is an error). The printed text is also capped at 256 KiB, keeping the most recent part and starting on a line boundary.
- Log lines go to stdout and nothing else does. Every note and error goes to stderr, so a pipe sees only log lines. Container applications usually log to stderr; both streams are merged into stdout.
- Exit code: `0` when logs were printed (or the component has no output yet), `1` for every failure.

## Related
- Parent namespace: [consul.md](consul.md)
- Status: `hal consul status`

## Prerequisites
- HAL CLI is available in your local environment.
- Docker or Podman is running, and the component has been created at least once.

## Flags
- Command flags from `hal consul logs --help`:
```text
-h, --help       help for logs
    --tail int   Number of most recent log lines to print (max 1000) (default 100)
```
- Global flags: `--debug`, `--dry-run`, `--verbose`

## Side Effects
- None. The command is read-only. It runs `inspect` and `logs` on the engine and writes nothing.

## Example
```bash
hal consul logs
hal consul logs --tail 20
```
