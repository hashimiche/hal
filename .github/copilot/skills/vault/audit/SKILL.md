---
name: audit
description: Enable, disable, verify, and explain Vault audit devices in the local hal lab. Use this skill when the user asks to enable audit logging, stream Vault logs to Loki/Grafana, find where audit logs are stored, reset audit devices, or troubleshoot why Vault is blocking on audit writes. Triggers include "enable audit", "vault logging", "stream to loki", "where are the audit logs", "hal vault audit", and "reset audit device".
---

# Hal Vault Audit Configurator

Use this skill to manage Vault audit devices in the local HAL lab.

## Lab Assumptions

- Vault runs locally at `http://127.0.0.1:8200`
- The default root token is `root`
- Prefer `hal` commands for lifecycle actions
- For day-2 inspection after deployment, provide exact `vault read` or `docker exec` commands rather than telling the user to edit Go code

## What This Skill Covers

- Enabling or disabling Vault audit logging
- Explaining the Loki/Promtail shared-volume pattern used by the lab
- Verifying mounted audit devices and their target paths
- Showing the user how to inspect or tail the logs safely
- Troubleshooting blocked Vault behavior caused by audit device write failures
- Confirming the observability URLs when the obs stack is present

## Workflow

### Step 1: Choose the correct lifecycle action

Use the smart status mode first if the user is unsure what is already configured:

    hal vault audit

Then run the appropriate lifecycle command:

    hal vault audit enable
    hal vault audit enable --loki
    hal vault audit --force --loki
    hal vault audit disable

If the user specifically asks for Loki/Grafana integration, prefer `--loki` because the code mounts `/vault/logs/audit.log` inside the Vault container and lets Promtail read the file passively.

### Step 2: Verify the resulting state

If Vault MCP is available, inspect:

1. `sys/audit`

If MCP is not available, use CLI verification commands:

    export VAULT_ADDR='http://127.0.0.1:8200'
    export VAULT_TOKEN='root'

    vault read sys/audit
    docker exec -it hal-vault ls -l /vault/logs
    docker exec -it hal-vault tail -n 20 /vault/logs/audit.log

If observability is enabled, also verify:

    hal obs status

### Step 3: Explain the architecture clearly

State that the HAL lab favors the file audit device pointed at `/vault/logs/audit.log`.

Why this matters:

- Vault writes locally to disk inside the container
- Promtail reads the shared Docker volume passively
- If Loki or Promtail is unavailable, Vault does not block on a remote network hop
- If local disk is full and Vault cannot write the file, Vault can block operations until the device is disabled or storage is cleaned up

### Step 4: Present structured results

**Tier 1 — Success Summary**
Provide a brief confirmation of the enabled audit device and whether Loki integration is in use.

**Tier 2 — Configuration Details Table**
Include the real mounted path and file target when available:

| Device Path | Type | Target | Purpose |
|-------------|------|--------|---------|
| `file/` | `file` | `/vault/logs/audit.log` | Primary local log / Loki source |

**Tier 3 — Actionable Insights & Testing**
Provide at least one of these commands:

    export VAULT_ADDR='http://127.0.0.1:8200'
    export VAULT_TOKEN='root'

    vault read sys/audit
    docker exec -it hal-vault tail -f /vault/logs/audit.log

If the user also has observability deployed, mention these surfaces explicitly:

- Grafana: `http://grafana.localhost:3000`
- Loki: `http://loki.localhost:3100/ready`
- Prometheus: `http://prometheus.localhost:9090`

## Expected Lab State

- Audit path defaults to `file/`
- File target defaults to `/vault/logs/audit.log`
- `--force` disables the device first, then re-enables it
- Disabling the file device also purges the old log file inside the container

## Handling Edge Cases

1. **Vault is offline:** Instruct the user to run `hal vault create` first.
2. **Blocked Vault due to audit write failure:** Explain that Vault can block operations if it cannot write to the audit sink. Check disk space and the log file target.
3. **Stale audit configuration:** Use `hal vault audit --force --loki` to reset the device cleanly.
4. **User wants raw log analysis rather than configuration:** Switch to the `audit-analysis` skill.