# HAL MCP Delete Command Spec

## Command
- `hal mcp delete`

## Purpose
Stop background HAL MCP process if PID file is present.

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
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal mcp delete
```
