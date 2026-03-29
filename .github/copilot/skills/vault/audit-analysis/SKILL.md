---
name: audit-analysis
description: Query and analyze Vault audit logs using the vault-audit MCP server. Use this skill whenever the user asks "who accessed X", "what happened to my secret", "check the audit logs", or "investigate Vault activity".
---

# Vault Audit Log Investigator

This skill leverages the `vault-audit` MCP server (which reads from the local Loki instance) to investigate security events, trace API requests, and summarize Vault activity.

## Workflow

### Step 1: Identify the intent
Determine what the user is looking for:
- If they want a summary of recent activity, use the `audit.search_events` tool.
- If they want to know how many times a specific action happened, use `audit.aggregate`.
- If they provide a specific request ID (e.g., `req_12345`), use `audit.trace`.

### Step 2: Query the MCP
Execute the appropriate tool. 
*Note: The local lab uses content-based filtering, so rely on the `namespace`, `operation`, `mount_type`, or `entity_id` parameters in the `audit.search_events` tool.*

### Step 3: Present structured results
Do not dump raw JSON logs. Synthesize the MCP output into a clean, markdown-formatted incident report.

**Tier 1 — Executive Summary**
A one-sentence overview of what was found (e.g., "I found 3 read operations on the database engine in the last 15 minutes.").

**Tier 2 — Activity Table**
Present the key events in a markdown table.

| Time | Operation | Path | Identity (Entity ID) | Status |
|------|-----------|------|----------------------|--------|
| ...  | ...       | ...  | ...                  | ...    |

**Tier 4 — Actionable Insights**
If the MCP tool returns `critical_events` or `high_risk_events` (like failed logins or root token usage), highlight them prominently with recommendations.