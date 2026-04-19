# HAL MCP Command Spec

## Base Command
- Command: `hal mcp`
- Purpose: manage HAL MCP server lifecycle
- Default behavior: runs `hal mcp status`

## Subcommands
- `hal mcp create`
  - Create MCP client config scaffold (and managed binary when applicable)
  - Spec: [mcp-create.md](mcp-create.md)

- `hal mcp update` (alias: `hal mcp up`)
  - Run or reconcile HAL MCP server
  - Current MVP transport: stdio
  - Spec: [mcp-up.md](mcp-up.md)

- `hal mcp status`
  - Show MCP readiness/config/binary state
  - Spec: [mcp-status.md](mcp-status.md)

- `hal mcp delete` (alias: `hal mcp down`)
  - Stop background HAL MCP process if PID file exists
  - Spec: [mcp-down.md](mcp-down.md)

## Notes
- MCP intent aliases and advanced metadata are implemented in `cmd/mcp/advanced.go`.

## Sources
- Namespace and lifecycle commands: `cmd/mcp/mcp.go`
- Advanced aliases/intent metadata: `cmd/mcp/advanced.go`
