# HAL MCP Down Command Spec

## Command
- `hal mcp down`

## Purpose
Stop background HAL MCP process if PID file is present.

## Related
- Parent namespace: [mcp.md](mcp.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal mcp down --help`:
```text
-h, --help   help for down
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal mcp down
```
