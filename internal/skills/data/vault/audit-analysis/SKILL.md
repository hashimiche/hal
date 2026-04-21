---
name: audit-analysis
description: Investigate and summarize Vault audit activity in the local hal lab. Use this skill when the user asks who accessed a secret, what happened to a request, whether root was used, why a login failed, or to analyze Vault audit logs from Loki or the local audit stream. Triggers include "who accessed", "check audit logs", "trace request", "failed login", "Vault activity", and "investigate Vault".
---

# Vault Audit Log Investigator

Use this skill for forensic or troubleshooting questions after Vault audit logging has already been enabled.

## Lab Assumptions

- Vault audit logging should already be enabled, ideally with `hal vault audit enable --loki`
- Observability may already be running via `hal obs create`
- Prefer the `vault-audit` MCP server when available
- Do not dump raw audit JSON unless the user explicitly asks for it

## What This Skill Covers

- Activity summaries over a recent time window
- Tracing a specific request or request ID
- Identifying failed auth attempts, root token usage, secret reads, and destructive operations
- Explaining likely causes and next operator actions

## Workflow

### Step 1: Identify the intent

Determine what the user is looking for:

- If they want a summary of recent activity, use the `audit.search_events` tool.
- If they want to know how many times a specific action happened, use `audit.aggregate`.
- If they provide a specific request ID, use `audit.trace`.
- If they ask who accessed a specific path, filter by path plus operation and summarize identities.
- If they ask whether root was used, look specifically for privileged tokens, dangerous paths, and failed logins.

### Step 2: Query the MCP

Prefer filters such as:

- `path`
- `operation`
- `mount_type`
- `entity_id`
- `namespace`
- time window

If the MCP server is unavailable, tell the user that deep audit analysis is limited and fall back to high-signal commands such as:

	docker exec -it hal-vault tail -n 100 /vault/logs/audit.log

### Step 3: Present structured results

Do not dump raw JSON logs. Synthesize the MCP output into a clean, markdown-formatted incident report.

**Tier 1 — Executive Summary**
A one-sentence overview of what was found.

**Tier 2 — Activity Table**

| Time | Operation | Path | Identity (Entity ID) | Status |
|------|-----------|------|----------------------|--------|
| ...  | ...       | ...  | ...                  | ...    |

**Tier 3 — Risk Highlights**
Call out any of these prominently:

- root token usage
- repeated failed logins
- destructive writes or deletes
- access outside the expected mount or namespace
- anomalous burst activity

**Tier 4 — Actionable Insights**
If the MCP tool returns `critical_events` or `high_risk_events`, highlight them prominently with recommendations.

## Recommended Response Style

- Start with what happened
- Then show the minimal evidence table
- Then give the likely explanation
- End with next commands the user can run

## Common Edge Cases

1. **No logs found:** Ask whether `hal vault audit enable --loki` was run and whether observability is up.
2. **User asks to enable logging first:** Switch to the `audit` skill.
3. **Too much raw data:** Summarize by operation, actor, path, and time window instead of pasting events.