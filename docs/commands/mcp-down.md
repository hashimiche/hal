# HAL MCP Delete Command Spec

## Command
- `hal mcp delete`

## Purpose
Remove HAL-managed MCP artifacts.

This command deletes:
- `~/.hal/mcp/hal-mcp.json`
- `~/.hal/bin/hal-mcp` (managed binary, when present)
- `~/.hal/mcp/hal-mcp.pid` (stale PID state, when present)

If a PID file points to a running process, HAL also attempts a best-effort `SIGTERM` first.

## Related
- Parent namespace: [mcp.md](mcp.md)

## Prerequisites
- HAL CLI is available in your local environment.

## Flags
- Command flags from `hal mcp delete --help`:
```text
-h, --help   help for delete
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- Removes HAL-managed MCP local artifacts.
- Does not affect product lab resources.
- The same MCP artifact cleanup is also performed by `hal delete` and `hal daisy`.

## Example
```bash
hal mcp delete
```
