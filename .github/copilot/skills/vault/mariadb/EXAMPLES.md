# Example Transcripts

## Example 1: Enable Dynamic DB Credentials

User: Configure MariaDB secrets with Vault.

Assistant:

    hal vault mariadb --enable

Assistant: I verify database connection and role:

    vault read database/config/hal-mariadb
    vault read database/roles/readonly-user

## Example 2: Credential Flow Validation

User: Show me JIT credentials in action.

Assistant:

    vault read database/creds/readonly-user

Assistant: I explain lease TTL and revocation behavior.

## Example 3: Rebuild DB Secrets Lab

User: Reset MariaDB and Vault DB integration.

Assistant:

    hal vault mariadb --force
