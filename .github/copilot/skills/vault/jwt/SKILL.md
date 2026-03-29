---
name: jwt
description: Deploy and configure the Vault JWT authentication method. Use this skill whenever the user asks to test JWT authentication, configure GitHub Actions / GitLab CI pipelines with Vault, or enable the JWT auth backend. Triggers on phrases like "enable jwt", "setup github actions auth", "configure jwt", or "deploy hal jwt".
---

# Hal Vault JWT Configurator

This skill uses the `hal` CLI to enable the JWT auth method in a local Vault instance and configure a test role for pipeline authentication.

## Workflow

### Step 1: Execute the Hal deployment

Run the `hal` CLI tool directly to configure the auth method. 

    hal vault jwt

*(Note: If the user explicitly asks for a clean slate, append the `-f` flag).*

### Step 2: Enrich with Vault MCP Context

Once the `hal` command completes successfully, verify the configuration using the official HashiCorp Vault MCP server. 

Use the Vault MCP tools to query the following endpoints against `http://127.0.0.1:8200`:
1. **Read the JWT config:** `auth/jwt/config`
2. **Read the generated roles:** `auth/jwt/role` (and fetch the specific role details if a role exists)

### Step 3: Present structured results

Synthesize the output from the `hal` CLI and the Vault MCP into a clean, markdown-formatted response.

**Tier 1 — Success Summary**
Provide a brief confirmation that the JWT auth method is enabled and configured.

**Tier 2 — Configuration Details Table**
Extract the data you found via the MCP query and present it in a table:

| Component | Value | Description |
|-----------|-------|-------------|
| Auth Path | `auth/jwt/` | The mount point for the JWT auth method |
| Bound Issuer | [Extract from MCP] | The trusted token issuer (e.g., GitHub/GitLab) |
| OIDC Discovery URL | [Extract from MCP] | The URL used to fetch public keys |

**Tier 3 — Actionable Testing Commands**
Provide the user with the exact commands they need to inspect the workflow themselves:

    export VAULT_ADDR='http://127.0.0.1:8200'
    export VAULT_TOKEN='root'

    # Check the JWT backend configuration:
    vault read auth/jwt/config

### Handling Edge Cases

1. **Vault is not running:** If the command fails because it cannot reach `127.0.0.1:8200`, instruct the user to run `hal vault deploy` first.
2. **Missing Claims:** If the user is troubleshooting a failed login, remind them to check the `bound_claims` on the role using `vault read auth/jwt/role/<role-name>`.