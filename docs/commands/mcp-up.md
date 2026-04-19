# HAL MCP Update Command Spec

## Command
- `hal mcp update` (alias: `hal mcp up`)

## Purpose
Run or reconcile HAL MCP server.

## Behavior
- Current supported transport is stdio.

## Related
- Parent namespace: [mcp.md](mcp.md)

## Prerequisites
- HAL CLI is available in your local environment.

## Flags
- Command flags from `hal mcp update --help`:
```text
-h, --help               help for update
--transport string   MCP transport to use (stdio for MVP) (default "stdio")
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal mcp update
```
