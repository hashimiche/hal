---
name: oidc
description: Deploy, verify, and troubleshoot the Vault OIDC lab in hal. Use this skill when the user asks to enable OIDC, set up human SSO, debug browser login redirects, inspect Keycloak-backed roles, or reset the OIDC demo. Triggers include "enable oidc", "Vault SSO", "Keycloak", "OIDC callback", "configure oidc", and "hal vault oidc".
---

# Hal Vault OIDC Configurator

This skill covers the Keycloak-backed OIDC demo implemented by `hal vault oidc`.

## Lab Assumptions

- Vault runs locally at `http://127.0.0.1:8200`
- Root token defaults to `root`
- Keycloak is exposed locally on port `8081`
- Prefer `hal` for lifecycle actions and `vault read/write` for post-deploy tuning

## What The Command Actually Sets Up

- Keycloak container: `hal-keycloak`
- OIDC auth mount: `oidc/`
- KV mount: `kv-oidc/`
- Policies: `admin`, `user-ro`
- External identity groups mapped from Keycloak groups `admin` and `user-ro`
- Demo users: `alice` and `bob`
- Keycloak realm: `hal`

## Workflow

### Step 1: Choose the lifecycle action

Use smart status mode if needed:

    hal vault oidc

Then use the correct lifecycle command:

    hal vault oidc --enable
    hal vault oidc --force
    hal vault oidc --disable

### Step 2: Enrich with Vault MCP Context

Once the `hal` command completes successfully, verify the configuration using the official HashiCorp Vault MCP server.

Inspect:

1. `auth/oidc/config`
2. `auth/oidc/role/default`
3. `sys/auth`
4. `kv-oidc/data/team1`

### Step 3: Present structured results

**Tier 1 — Success Summary**
Provide a brief confirmation that the OIDC auth method is enabled.

**Tier 2 — Configuration Details Table**

| Component | Value | Description |
|-----------|-------|-------------|
| Auth Path | `auth/oidc/` | The mount point for the OIDC auth method |
| Client ID | `vault` | The OIDC client registered in Keycloak |
| Default Role | `default` | The configured default Vault OIDC role |
| Discovery URL | `http://keycloak.localhost:8081/realms/hal` | The OIDC discovery endpoint |
| External Groups | `admin`, `user-ro` | Keycloak groups mapped to Vault identity groups and policies |

**Tier 3 — Actionable Testing Commands**

    export VAULT_ADDR='http://127.0.0.1:8200'
    export VAULT_TOKEN='root'

    vault login -method=oidc
    vault read auth/oidc/config
    vault read auth/oidc/role/default

## Handling Edge Cases

1. **Callback URL mismatch:** Ensure the allowed callback URL matches `http://localhost:8250/oidc/callback`.
2. **Vault is offline:** Instruct the user to run `hal vault deploy` first.
3. **Group claim mismatch:** Verify Keycloak group membership and `groups_claim` on `auth/oidc/role/default`.
4. **User asks about groups or policies after deployment:** Provide exact `vault read/write` commands for `identity/group`, `identity/group-alias`, or `auth/oidc/role/default`.