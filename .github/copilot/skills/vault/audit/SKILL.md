---
name: audit
description: Enable and configure Vault audit devices, including Loki integration. Use this skill whenever the user asks to track Vault usage, see who accessed a secret, enable logging, stream logs to Loki/Grafana, or configure audit devices. Triggers on phrases like "enable audit", "setup logging", "stream to loki", "where are the logs", or "deploy hal audit".
---

# Hal Vault Audit Configurator

This skill uses the `hal` CLI to enable audit logging on the local Vault container. It uses a robust, passive Docker Volume architecture to seamlessly ship logs to the PLG (Prometheus, Loki, Grafana) observability stack without risking Vault network timeouts.

## Workflow

### Step 1: Execute the Hal deployment

Determine if the user just wants basic logging or if they are integrating with the `hal obs` stack, then run the appropriate `hal` CLI command.

    # For standard, local file-based audit logging:
    hal vault audit enable

    # For observability integration (mounts the shared volume for Promtail):
    hal vault audit enable --loki

*(Note: If the user wants to turn off auditing, use `hal vault audit disable`).*

### Step 2: Enrich with Vault MCP Context

Verify the configuration using the official HashiCorp Vault MCP server. 

Use the Vault MCP tools to query the following endpoint against `http://127.0.0.1:8200`:
1. **List Audit Devices:** `sys/audit`

### Step 3: Present structured results

**Tier 1 — Success Summary**
Provide a brief confirmation of the enabled audit device. If the `--loki` flag was used, explicitly mention the shared Docker volume architecture.

**Tier 2 — Configuration Details Table**
Extract the data you found via the MCP query:

| Device Path | Type | Target | Purpose |
|-------------|------|--------|---------|
| `file/` | `file` | `/vault/logs/audit.log` | Primary local log / Loki source |

**Tier 3 — Actionable Insights & Testing**
Explain the architecture and provide commands to view the logs.

> **Architecture Note:**
> Vault blocks all operations if it cannot write to its audit devices. By using a shared Docker volume (`hal-vault-logs`), `hal` ensures that Vault natively writes to a file, and Promtail passively reads it. If Loki or Promtail crashes, Vault remains completely unaffected and will continue serving requests.

    # To tail the live Vault audit logs directly from the container:
    docker exec -it hal-vault tail -f /vault/logs/audit.log

### Handling Edge Cases

1. **Blocked Vault (Audit Failure):** If Vault completely locks up and refuses API requests, it means the local Docker disk is full and `/vault/logs/audit.log` cannot be written to. Instruct the user to run `hal vault deploy --destroy` to clear out the volume and start fresh.