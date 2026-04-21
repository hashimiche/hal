# HAL MCP Command Spec

## Base Command
- Command: `hal mcp`
- Purpose: manage HAL MCP server lifecycle
- Default behavior: runs `hal mcp status`

## Subcommands
- `hal mcp create`
  - Create or replace MCP client config scaffold (and managed binary when applicable)
  - Spec: [mcp-create.md](mcp-create.md)

- `hal mcp serve`
  - Run the HAL MCP stdio server for an MCP client such as `hal-plus`
  - Current MVP transport: stdio
  - Spec: [mcp-serve.md](mcp-serve.md)

- `hal mcp status`
  - Show MCP readiness, config state, and managed binary state
  - Spec: [mcp-status.md](mcp-status.md)

- `hal mcp delete`
  - Remove HAL-managed MCP artifacts (config, managed binary, stale PID state)
  - Spec: [mcp-down.md](mcp-down.md)

## Notes
- MCP intent aliases and advanced metadata are implemented in `cmd/mcp/advanced.go`.
- `hal delete` and `hal daisy` also remove HAL-managed MCP artifacts as part of global teardown.

## Sources
- Namespace and lifecycle commands: `cmd/mcp/mcp.go`
- Advanced aliases/intent metadata: `cmd/mcp/advanced.go`
