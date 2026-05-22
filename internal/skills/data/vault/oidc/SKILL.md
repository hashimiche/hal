---
name: oidc
description: Deploy, verify, and troubleshoot the Vault OIDC lab in hal. Use this skill when the user asks to enable OIDC, set up human SSO, debug browser login redirects, inspect Authentik-backed roles, reset the OIDC demo, or configure SCIM provisioning. Triggers include "enable oidc", "Vault SSO", "Authentik", "OIDC callback", "configure oidc", "hal vault oidc", "enable scim", and "vault scim".
---

# Hal Vault OIDC Configurator

This skill covers the Authentik-backed OIDC demo implemented by `hal vault oidc`.

## Lab Assumptions

- Vault runs locally at `http://vault.localhost:8200`
- Root token defaults to `root`
- Authentik is exposed locally on port `9100` at `http://authentik.localhost:9100`
- Prefer `hal` for lifecycle actions and `vault read/write` for post-deploy tuning

## What The Command Actually Sets Up

**Authentik containers** (all on `hal-net`):
- `hal-authentik-pg` — PostgreSQL database
- `hal-authentik-server` — Authentik API + UI (HTTP `9100`, HTTPS `9143`)
- `hal-authentik-worker` — Celery background tasks

**Authentik objects provisioned via REST API**:
- Groups: `admin`, `user-ro`
- Users: `alice / password` (admin), `bob / password` (user-ro)
- Scope mapping: `hal: OIDC groups scope` (created automatically if absent in 2024.x+)
- OAuth2 provider: `vault-oidc-provider` (confidential, implicit-consent flow, `sub_mode: user_username`)
- Application slug: `hashicorp-vault` (tile launch URL: `http://vault.localhost:8200/ui/vault/auth/oidc`)

**Vault objects (OIDC only)**:
- OIDC auth mount: `oidc/`
- KV-V2 mount: `kv-oidc/`
- Policies: `admin` (all paths), `user-ro` (kv-oidc/team1 read)
- External identity groups `admin` and `user-ro` with aliases bound to OIDC accessor
- OIDC issuer: `http://authentik.localhost:9100/application/o/hashicorp-vault/`

**Additional Vault objects with `--scim` (Enterprise)**:
- SCIM activation flag (one-way)
- Policy `scim-client` (identity/scim/v2/* — create/read/update/patch/delete/list)
- Entity `scim-client-authentik` with alias on token mount
- Token role `authentik-scim` (32-day renewable, orphan)
- SCIM client `authentik-scim` with `alias_mount_accessor = oidc/`
- Authentik outbound SCIM provider `vault-scim-provider` (AWS compatibility mode)
- Authentik: SCIM provider assigned as backchannel on `hashicorp-vault` app
- External groups (`admin`, `user-ro`) owned by Authentik SCIM — **not** pre-created in Vault

## Workflow

### Step 1: Choose the lifecycle action

Use smart status mode if needed:

    hal vault oidc

Then use the correct lifecycle command:

    hal vault oidc enable
    hal vault oidc update
    hal vault oidc disable

Flags:
- `--authentik-image` — override image (default: `ghcr.io/goauthentik/server`)
- `--authentik-tag` — override tag (default: `2026.2.3`)
- `--scim` — [Vault Enterprise] configure Authentik outbound SCIM provisioning to Vault

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
| Client ID | dynamic (printed at enable time) | OAuth2 client registered in Authentik |
| Default Role | `default` | The configured default Vault OIDC role |
| Discovery URL | `http://authentik.localhost:9100/application/o/hashicorp-vault/` | OIDC discovery endpoint |
| External Groups | `admin`, `user-ro` | Authentik groups mapped to Vault identity groups and policies |
| Authentik Admin UI | `http://authentik.localhost:9100/if/admin/` | Manage users, groups, applications |

**Tier 3 — Actionable Testing Commands**

    export VAULT_ADDR='http://vault.localhost:8200'
    export VAULT_TOKEN='root'

    vault login -method=oidc
    vault read auth/oidc/config
    vault read auth/oidc/role/default

## Handling Edge Cases

1. **Callback URL mismatch:** Authentik must have `http://localhost:8250/oidc/callback`, `http://127.0.0.1:8250/oidc/callback`, and `http://vault.localhost:8200/ui/vault/auth/oidc/oidc/callback` as allowed redirect URIs on the `vault-oidc-provider`.
2. **Vault is offline:** Instruct the user to run `hal vault create` first.
3. **Group claim missing:** The `hal: OIDC groups scope` mapping must be assigned to the `vault-oidc-provider`. It uses `ak_groups` to return group names. Verify with `vault read auth/oidc/role/default` — `groups_claim` should be `groups`.
4. **403 on first enable after fresh stack:** Bootstrap token race — the Authentik worker creates the token asynchronously. `hal vault oidc update` retries safely.
5. **User asks about Authentik admin password:** It is printed once at first enable and stored in `~/.hal/authentik/env` (field `AUTHENTIK_BOOTSTRAP_PASSWORD`). Username is `akadmin`.
6. **"Expired or missing OAuth state" on second OIDC attempt:** Caused by the explicit-consent authorization flow orphaning the state. Fixed by using the implicit-consent flow. The `GetDefaultAuthorizationFlowPK` function now prefers slugs containing `implicit`.
7. **SCIM group membership empty after adding a user:** This was a known gap in older Authentik (ManyToMany signals not firing). **Fixed in Authentik 2026.2.3+** — membership changes now auto-propagate. If you see stale membership with an older Authentik version, use `hal vault oidc update --scim --sync` to force a full re-sync.
8. **SCIM returns 400/invalidValue for group PATCH:** Authentik without `compatibility_mode: aws` includes `"schemas"` inside PatchOp Operations array items, which Vault SCIM rejects. This is already set by `hal`. If syncing manually, ensure provider has compatibility mode enabled.
9. **SCIM returns permission denied on group PATCH:** The `scim-client` Vault policy must include the `patch` capability. Vault 1.14+ treats HTTP PATCH as a separate capability from `update`.