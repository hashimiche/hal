# HAL MCP Create Command Spec

## Command
- `hal mcp create`

## Purpose
Create HAL MCP client scaffold configuration (and managed binary flow when enabled).

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
--json                 Only generate/update MCP config JSON (skip managed binary provisioning)
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal mcp create
```
