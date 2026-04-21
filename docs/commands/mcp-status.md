# HAL MCP Status Command Spec

## Command
- `hal mcp status`

## Purpose
Display HAL MCP readiness and scaffold/config state.

## Behavior
- Default when running `hal mcp` with no subcommand.
- Reports whether the MCP config file exists.
- Reports whether the managed HAL MCP binary exists.
- Reminds the user that stdio clients start and stop `hal mcp serve` on demand.

## Related
- Parent namespace: [mcp.md](mcp.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal mcp status --help`:
```text
-h, --help   help for status
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- None. This is a read-only inspection command.

## Example
```bash
hal mcp status
```
