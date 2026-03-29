---
name: k8s
description: Deploy and configure the Vault Kubernetes authentication method. Use this skill whenever the user asks to test pod authentication, configure k8s auth, or enable the Kubernetes auth backend. Triggers on phrases like "enable k8s auth", "setup kubernetes", "pod identity", or "deploy hal k8s".
---

# Hal Vault Kubernetes Configurator

This skill uses the `hal` CLI to enable the Kubernetes auth method, allowing simulated local Kubernetes pods to authenticate to Vault using their Service Account Tokens.

## Workflow

### Step 1: Execute the Hal deployment

Run the `hal` CLI tool directly to configure the auth method.

    hal vault k8s

### Step 2: Enrich with Vault MCP Context

Verify the configuration using the official HashiCorp Vault MCP server. 

Use the Vault MCP tools to query the following endpoints against `http://127.0.0.1:8200`:
1. **Read the K8s config:** `auth/kubernetes/config`
2. **Read the generated roles:** `auth/kubernetes/role`

### Step 3: Present structured results

**Tier 1 — Success Summary**
Provide a brief confirmation that the Kubernetes auth method is enabled and pointing to the local cluster endpoint.

**Tier 2 — Configuration Details Table**
Extract the data you found via the MCP query:

| Component | Value | Description |
|-----------|-------|-------------|
| Auth Path | `auth/kubernetes/` | The mount point |
| K8s Host | [Extract from MCP] | The Kubernetes API server address |
| Vault Role | [Extract from MCP] | The role mapping K8s Service Accounts to Vault policies |

**Tier 3 — Actionable Testing Commands**
Provide the user with the commands to inspect the setup:

    export VAULT_ADDR='http://127.0.0.1:8200'
    export VAULT_TOKEN='root'

    # Inspect the Kubernetes API connection config:
    vault read auth/kubernetes/config

### Handling Edge Cases

1. **Token Reviewer Errors:** If testing a login fails, advise the user that the Vault container must have network access to the local Kubernetes API (e.g., `kubernetes.default.svc`) to verify the Service Account JWTs.