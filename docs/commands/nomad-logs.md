# HAL Nomad Logs Command Spec

## Command
- `hal nomad logs [--tail N]`

## Purpose
Print the most recent log lines of the `nomad` systemd unit inside the `hal-nomad` Multipass VM (Nomad is not a container). Runs `multipass exec hal-nomad -- journalctl -u nomad -n N --no-pager`; it does not need Docker or Podman.

Read-only: the command never follows the stream, never starts, stops or changes anything, and is annotated `hal.mcp/readonly: "true"` (ADR 0003, decisions 3 and 8) so the MCP `hal_read` tool may run it. It replaces running `podman logs` by hand.

## Behavior
- Checks that the `hal-nomad` VM exists and is `Running` (`multipass info`), then reads the unit journal.
- Missing VM: prints `hal-nomad is not running: no Multipass VM with that name. Check 'hal nomad status', or start it with 'hal nomad create'.` on stderr and exits `1`.
- Stopped VM: same message style on stderr, exit `1` (a stopped VM cannot be queried).
- Bounded output: `--tail` defaults to 100 lines and is capped at 1000 (a larger value is clamped, with a note on stderr; `0` or a negative value is an error). The printed text is also capped at 256 KiB, keeping the most recent part and starting on a line boundary.
- Log lines go to stdout and nothing else does. Every note and error goes to stderr, so a pipe sees only log lines. Container applications usually log to stderr; both streams are merged into stdout.
- Exit code: `0` when logs were printed (or the component has no output yet), `1` for every failure.

## Related
- Parent namespace: [nomad.md](nomad.md)
- Status: `hal nomad status`

## Prerequisites
- HAL CLI is available in your local environment.
- Multipass is installed and the `hal-nomad` VM is running (`hal nomad create`).

## Flags
- Command flags from `hal nomad logs --help`:
```text
-h, --help       help for logs
    --tail int   Number of most recent log lines to print (max 1000) (default 100)
```
- Global flags: `--debug`, `--dry-run`, `--verbose`

## Side Effects
- None. The command is read-only. It runs `inspect` and `logs` on the engine (or `multipass info` / `multipass exec ... journalctl`) and writes nothing.

## Example
```bash
hal nomad logs
hal nomad logs --tail 20
```
