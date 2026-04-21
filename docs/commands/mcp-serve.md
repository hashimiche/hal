# HAL MCP Serve Command Spec

## Command
- `hal mcp serve`

## Purpose
Run the HAL MCP stdio server for an MCP client such as `hal-plus`.

## Behavior
- Current supported transport is stdio.
- This command is meant to be launched by an MCP client, not typed interactively for config updates.
- When run manually in a terminal, HAL prints guidance and exits instead of waiting for malformed input.
- MCP clients spawn this command as a child process, then exchange framed JSON-RPC messages over stdin/stdout.

## Related
- Parent namespace: [mcp.md](mcp.md)

## Prerequisites
- HAL CLI is available in your local environment.

## Flags
- Command flags from `hal mcp serve --help`:
```text
-h, --help               help for serve
--transport string       MCP transport to use (stdio for MVP) (default "stdio")
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command serves framed JSON-RPC messages over stdio. It does not create or update the MCP config file.

## Example
```bash
hal mcp serve
```