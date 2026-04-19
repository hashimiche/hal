---
name: ldap
description: Deploy, verify, and troubleshoot the Vault LDAP auth plus LDAP secrets engine lab in hal. Use when the user asks for LDAP login, directory-backed auth, dynamic/static/library LDAP secrets, or LDAP lab reset.
---

# Hal Vault LDAP Configurator

This skill covers the OpenLDAP + phpLDAPadmin + Vault LDAP auth/secrets workflow implemented by `hal vault ldap`.

## Lab Assumptions

- Vault runs locally at `http://127.0.0.1:8200`
- Root token defaults to `root`
- OpenLDAP container is `hal-openldap`
- phpLDAPadmin UI is `https://phpldapadmin.localhost:8082`
- Prefer `hal` for lifecycle and `vault read/write` for day-2 checks

## What The Command Actually Sets Up

- OpenLDAP: seeded users, groups, and service accounts
- phpLDAPadmin UI for directory inspection
- Vault auth mount: `ldap/`
- Vault KV mount: `kv-ldap/`
- Vault LDAP secrets engine mount: `ldap/`
- Policies: `ldap-admin`, `ldap-reader`
- LDAP secrets examples:
  - dynamic role: `ldap/role/dynamic-reader`
  - static role: `ldap/static-role/static-app`
  - library: `ldap/library/dev-pool`
- Root bind account rotation via `ldap/rotate-root`

## Workflow

### Step 1: Choose the lifecycle action

Use smart status mode if needed:

	hal vault ldap

Then run one of these:

	hal vault ldap enable
	hal vault ldap --force
	hal vault ldap disable

### Step 2: Verify Vault and directory state

If Vault MCP is available, inspect:

1. `auth/ldap/config`
2. `auth/ldap/groups/admin`
3. `auth/ldap/groups/reader`
4. `ldap/config`
5. `ldap/role/dynamic-reader`
6. `ldap/static-role/static-app`
7. `ldap/library/dev-pool`

If MCP is unavailable, use:

	export VAULT_ADDR='http://127.0.0.1:8200'
	export VAULT_TOKEN='root'

	vault read auth/ldap/config
	vault read ldap/config
	vault read ldap/role/dynamic-reader
	vault read ldap/static-role/static-app
	vault read ldap/library/dev-pool

### Step 3: Present structured results

**Tier 1 - Success Summary**
Confirm LDAP auth and LDAP secrets are both configured.

**Tier 2 - Configuration Details Table**

| Component | Value | Description |
|-----------|-------|-------------|
| Auth Mount | `auth/ldap/` | Human login against OpenLDAP |
| Secrets Mount | `ldap/` | LDAP secrets engine |
| Dynamic Role | `dynamic-reader` | Short-lived generated user |
| Static Role | `static-app` | Rotated static service account |
| Library | `dev-pool` | Check-out/check-in pooled credentials |

**Tier 3 - Actionable Testing Commands**

	vault login -method=ldap username=bob password=password
	vault read ldap/creds/dynamic-reader
	vault read ldap/static-cred/static-app
	vault write -f ldap/library/dev-pool/check-out

## Edge Cases

- If Vault is offline, instruct the user to deploy Vault first.
- If LDAP teardown is blocked, use `hal vault ldap --force` so Vault lease and mount cleanup runs before container removal.
- If user expects phpLDAPadmin admin password to remain static, explain root credentials are intentionally rotated once the LDAP secrets engine is configured.
