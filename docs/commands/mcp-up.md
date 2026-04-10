# HAL MCP Up Command Spec

## Command
- `hal mcp up`

## Purpose
Run HAL MCP server.

## Behavior
- Current supported transport is stdio.

## Related
- Parent namespace: [mcp.md](mcp.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal mcp up --help`:
```text
-h, --help               help for up
--transport string   MCP transport to use (stdio for MVP) (default "stdio")
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal mcp up
```
