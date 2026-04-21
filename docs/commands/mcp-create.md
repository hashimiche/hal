# HAL MCP Create Command Spec

## Command
- `hal mcp create`

## Purpose
Create or replace the HAL MCP client scaffold configuration and, when enabled, the managed HAL MCP binary.

## Related
- Parent namespace: [mcp.md](mcp.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal mcp create --help`:
```text
--binary-path string   Path to write the managed HAL binary used by MCP clients (default ~/.hal/bin/hal-mcp)
--command string       HAL command name/path to use in generated MCP client config (default "hal")
-h, --help                 help for create
--json                 Only generate/replace MCP config JSON (skip managed binary provisioning)
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- Rewrites `~/.hal/mcp/hal-mcp.json`.
- Replaces the managed HAL MCP binary when binary provisioning is enabled.
- Generated MCP config launches `hal mcp serve`.

## Example
```bash
hal mcp create
```

## Notes
- Running `hal mcp create` again refreshes the same HAL-managed MCP artifacts in place.
- It does not create multiple MCP configs or multiple managed binaries.
