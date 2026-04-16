---
name: database
description: Deploy, verify, and troubleshoot the Vault database secrets lab in hal. Use this skill when the user asks to enable the database secrets engine, generate dynamic DB credentials, debug database leases, rotate the root connection, or reset the local database demo. Triggers include "enable database", "dynamic db credentials", "Vault database engine", "rotate root", and "hal vault database".
---

# Hal Vault Database Configurator

This skill covers the local Vault database secrets engine lab implemented by `hal vault database`.

## Lab Assumptions

- Vault runs locally at `http://127.0.0.1:8200`
- Root token defaults to `root`
- Backend defaults to MariaDB (`--backend mariadb`); Postgres is planned but not implemented yet.
- Prefer `hal` for deployment and cleanup, then use `vault read/write` for day-2 operations

## What The Command Actually Sets Up

- Vault secrets engine: `database/`
- Connection name: `database/config/hal-vault-mariadb`
- Dynamic role: `database/roles/dba-role`
- Root connection user initially created: `vaultadmin`
- Vault rotates the `vaultadmin` password so it becomes Vault-owned

## Workflow

### Step 1: Choose the lifecycle action

Use smart status mode if needed:

    hal vault database

Then use the correct lifecycle command:

    hal vault database --enable --backend mariadb
    hal vault database --force
    hal vault database --disable

### Step 2: Enrich with Vault MCP Context

Once the `hal` command completes successfully, verify the configuration using Vault MCP when available.

Inspect:

1. `database/config/hal-vault-mariadb`
2. `database/roles/dba-role`
3. `sys/mounts`

### Step 3: Present structured results

**Tier 1 — Success Summary**
Provide a brief confirmation that the database is running and the engine is mounted.

**Tier 2 — Configuration Details Table**

| Component | Value | Description |
|-----------|-------|-------------|
| DB Container | `hal-vault-mariadb` | Local Docker network hostname |
| Connection Name | `hal-vault-mariadb` | Vault's internal reference to the selected DB |
| Vault Role | `dba-role` | The role used to generate dynamic creds |
| Mount Path | `database/` | The database secrets engine |

**Tier 3 — Actionable Testing Commands**

    export VAULT_ADDR='http://127.0.0.1:8200'
    export VAULT_TOKEN='root'

    vault read database/creds/dba-role
    vault read database/config/hal-vault-mariadb
    vault read database/roles/dba-role

## Handling Edge Cases

1. **Vault is not running:** Instruct the user to run `hal vault deploy` first.
2. **Port conflicts:** If Docker fails to bind `3306` or `5432`, advise the user to stop local DB services using those ports.
3. **Dangling leases prevent cleanup:** Explain that the code force-revokes `database/` leases during teardown.
4. **User wants to change SQL statements or TTLs after deployment:** Provide exact `vault write database/roles/dba-role ...` commands rather than suggesting Go edits.