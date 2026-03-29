---
name: oidc
description: Deploy and configure the Vault OIDC authentication method. Use this skill whenever the user asks to test human SSO, configure web-based login, or enable the OIDC auth backend. Triggers on phrases like "enable oidc", "setup sso", "configure oidc", or "deploy hal oidc".
---

# Hal Vault OIDC Configurator

This skill uses the `hal` CLI to enable the OIDC auth method in a local Vault instance, simulating a Single Sign-On (SSO) integration for human users.

## Workflow

### Step 1: Execute the Hal deployment

Run the `hal` CLI tool directly to configure the OIDC auth method.

    hal vault oidc

### Step 2: Enrich with Vault MCP Context

Once the `hal` command completes successfully, verify the configuration using the official HashiCorp Vault MCP server. 

Use the Vault MCP tools to query the following endpoints against `http://127.0.0.1:8200`:
1. **Read the OIDC config:** `auth/oidc/config`
2. **Read the generated roles:** `auth/oidc/role`

### Step 3: Present structured results

Synthesize the output into a clean, markdown-formatted response.

**Tier 1 — Success Summary**
Provide a brief confirmation that the OIDC auth method is enabled.

**Tier 2 — Configuration Details Table**
Extract the data you found via the MCP query and present it in a table:

| Component | Value | Description |
|-----------|-------|-------------|
| Auth Path | `auth/oidc/` | The mount point for the OIDC auth method |
| Client ID | [Extract from MCP] | The OIDC application client ID |
| Default Role | [Extract from MCP] | The role assigned upon successful login |

**Tier 3 — Actionable Testing Commands**
Provide the user with the exact commands they need to test the SSO login flow:

    export VAULT_ADDR='http://127.0.0.1:8200'
    export VAULT_TOKEN='root'

    # Initiate an OIDC login via the CLI:
    vault login -method=oidc role=[role-name]

### Handling Edge Cases

1. **Callback URL Mismatch:** If the OIDC flow fails in the browser, advise the user to ensure the Identity Provider's allowed callback URL matches `http://localhost:8250/oidc/callback`.