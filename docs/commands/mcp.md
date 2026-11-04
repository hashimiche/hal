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

The existing `get_vault_pki_status` tool also returns `data.pqc_execution` from
`hal vault pki status --pqc` and `data.agility_execution` from
`hal vault pki status --agility`, alongside conventional `data.execution`. Its
input and response envelope are unchanged. These report deployment readiness,
not cryptographic proof or browser trust. The tool recommends
`hal vault pki enable --agility`, which provisions missing conventional PKI and
PQC lab prerequisites on a compatible, licensed Vault instance. See
[Vault PKI](vault-pki.md).

The PQC and agility payloads are additive: clients should tolerate their
absence with older HAL versions and preserve conventional `data.execution`
handling. All three executions have separate checks; the top-level result
retains conventional PKI execution semantics. No input or envelope version
change is required. HAL Plus consumers should ground PQC answers in
`pqc_execution` when present and never infer mutual authentication from
readiness.
