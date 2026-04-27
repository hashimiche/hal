---
name: mcp
description: Manage the HAL MCP server lifecycle and AI client configuration. Use when the user asks to start MCP, configure an AI client (Copilot, Claude), check MCP health, or understand the MCP transport options.
---

# HAL MCP Skill

The `hal mcp` namespace manages the HAL MCP server — the runtime bridge between AI clients and the live HAL lab environment.

## Transport Modes

| Mode | Command | Protocol |
|---|---|---|
| stdio (local/dev) | `hal mcp serve` | 2024-11-05 |
| streamable-HTTP (container) | `hal mcp serve --transport streamable-http --http-host 0.0.0.0 --http-port 8080 --http-path /mcp` | 2025-03-26 |

## Primary Commands

```
hal mcp create          # scaffold AI client config (stdio, local dev)
hal mcp create --http   # pull ghcr.io/hashimiche/hal-mcp:latest and run container
hal mcp serve           # start MCP server in stdio mode (used by HAL Plus)
hal mcp status          # check MCP readiness
hal mcp delete          # remove MCP managed artifacts
hal mcp policy          # print runtime answer/tool policy (read-only)
```

## Key Flags

- `--http` — run the MCP server as a streamable-HTTP container instead of stdio
- `--http-tag <tag>` — override the pulled image tag (default `latest`)

## Architecture Notes

- The `hal-mcp` container does **not** mount the host container engine socket and does not run as root.
- Tool calls requiring engine access (`hal_status_baseline`) return engine-unavailable in rootless Podman — this is expected.
- AI clients must treat engine-unavailable as `runtime unknown`, not product down.
- There is no SSH-based MCP transport. Do not suggest SSH tunnelling for MCP.
- `hal mcp create` configures the AI client config scaffold; it does not start a persistent server process unless `--http` is used.

## Lab Assumptions

- MCP runs on `hal-net` alongside HAL Plus.
- HAL Plus spawns `hal mcp serve` (stdio) internally — no manual `hal mcp serve` needed when using HAL Plus.
- For standalone AI client use (Copilot, Claude Desktop), run `hal mcp create` to generate the client config.
