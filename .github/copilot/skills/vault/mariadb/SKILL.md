---
name: mariadb
description: Deploy, verify, and troubleshoot the Vault MariaDB database secrets lab in hal. Use this skill when the user asks to enable the database secrets engine, generate dynamic MariaDB credentials, debug database leases, rotate the root connection, or reset the local database demo. Triggers include "enable mariadb", "dynamic db credentials", "Vault database engine", "rotate root", and "hal vault mariadb".
---

# Hal Vault MariaDB Configurator

This skill covers the local MariaDB + Vault database secrets engine lab implemented by `hal vault mariadb`.

## Lab Assumptions

- Vault runs locally at `http://127.0.0.1:8200`
- Root token defaults to `root`
- MariaDB is deployed as container `hal-mariadb`
- Prefer `hal` for deployment and cleanup, then use `vault read/write` for day-2 operations

## What The Command Actually Sets Up

- MariaDB container: `hal-mariadb`
- Vault secrets engine: `database/`
- Connection: `database/config/hal-mariadb`
- Dynamic role: `database/roles/readonly-user`
- Root connection user initially created: `vaultadmin`
- Vault rotates the `vaultadmin` password so it becomes Vault-owned

## Workflow

### Step 1: Choose the lifecycle action

Use smart status mode if needed:

    hal vault mariadb

Then use the correct lifecycle command:

    hal vault mariadb --enable
    hal vault mariadb --force
    hal vault mariadb --disable

### Step 2: Enrich with Vault MCP Context

Once the `hal` command completes successfully, verify the configuration using Vault MCP when available.

Inspect:

1. `database/config/hal-mariadb`
2. `database/roles/readonly-user`
3. `sys/mounts`

### Step 3: Present structured results

**Tier 1 — Success Summary**
Provide a brief confirmation that the database is running and the engine is mounted.

**Tier 2 — Configuration Details Table**

| Component | Value | Description |
|-----------|-------|-------------|
| DB Container | `hal-mariadb` | Local Docker network hostname |
| Connection Name | `hal-mariadb` | Vault's internal reference to the DB |
| Vault Role | `readonly-user` | The role used to generate dynamic creds |
| Mount Path | `database/` | The database secrets engine |

**Tier 3 — Actionable Testing Commands**

    export VAULT_ADDR='http://127.0.0.1:8200'
    export VAULT_TOKEN='root'

    vault read database/creds/readonly-user
    vault read database/config/hal-mariadb
    vault read database/roles/readonly-user

## Handling Edge Cases

1. **Vault is not running:** Instruct the user to run `hal vault deploy` first.
2. **Port conflicts:** If Docker fails to bind port `3306`, advise the user to stop any local MySQL or MariaDB service.
3. **Dangling leases prevent cleanup:** Explain that the code force-revokes `database/` leases during teardown.
4. **User wants to change SQL statements or TTLs after deployment:** Provide exact `vault write database/roles/readonly-user ...` commands rather than suggesting Go edits.