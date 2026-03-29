---
name: mariadb
description: Deploy and configure a local Vault MariaDB database secrets engine. Use this skill whenever the user asks to test database secrets, dynamic credentials for MariaDB/MySQL, or wants to spin up a database connected to Vault. Triggers on phrases like "enable mariadb", "setup a database secret engine", "dynamic db credentials", or "deploy hal mariadb".
---

# Hal Vault MariaDB Configurator

This skill uses the `hal` CLI to spin up a local MariaDB docker container, automatically configure the Vault `database/` secrets engine, rotate the root database credentials (Secret Zero), and generate a test role.

## Workflow

### Step 1: Execute the Hal deployment

Run the `hal` CLI tool directly to build the infrastructure. Do not attempt to write Vault API curl commands to do this; `hal` handles the orchestration.

    hal vault mariadb

*(Note: If the user explicitly asks for a clean slate, append the `-f` flag).*

### Step 2: Enrich with Vault MCP Context

Once the `hal` command completes successfully, do not just stop. You must verify the configuration using the official HashiCorp Vault MCP server. 

Use the Vault MCP tools to query the following endpoints against `http://127.0.0.1:8200`:
1. **Read the connection config:** `database/config/hal-mariadb`
2. **Read the generated role:** `database/roles/readonly-user`

### Step 3: Present structured results

Synthesize the output from the `hal` CLI and the Vault MCP into a clean, markdown-formatted response. Use the following tiers:

**Tier 1 — Success Summary**
Provide a brief confirmation that the database is running and the engine is mounted. 

**Tier 2 — Configuration Details Table**
Extract the data you found via the MCP query and present it in a table:

| Component | Value | Description |
|-----------|-------|-------------|
| DB Container | `hal-mariadb` | Local Docker network hostname |
| Connection Name | `hal-mariadb` | Vault's internal reference to the DB |
| Vault Role | `readonly-user` | The role used to generate dynamic creds |

**Tier 3 — Actionable Testing Commands**
Provide the user with the exact commands they need to test the workflow themselves. Always include the required environment variables:

    export VAULT_ADDR='http://127.0.0.1:8200'
    export VAULT_TOKEN='root'

    # Generate a new dynamic database credential:
    vault read database/creds/readonly-user

### Handling Edge Cases

1. **Vault is not running:** If the `hal vault mariadb` command fails because it cannot reach `127.0.0.1:8200`, instruct the user to run `hal vault deploy` first, then stop.
2. **Port Conflicts:** If Docker fails to bind port `3306`, advise the user to kill any existing local MySQL/MariaDB instances.