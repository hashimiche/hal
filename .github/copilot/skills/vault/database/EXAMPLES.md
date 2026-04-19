# Example Transcripts

## Example 1: Enable Dynamic DB Credentials

User: Configure database secrets with Vault.

Assistant:

    hal vault database enable --backend mariadb

Assistant: I verify database connection and role:

    vault read database/config/hal-vault-mariadb
    vault read database/roles/dba-role

## Example 2: Credential Flow Validation

User: Show me JIT credentials in action.

Assistant:

    vault read database/creds/dba-role

Assistant: I explain lease TTL and revocation behavior.

## Example 3: Rebuild DB Secrets Lab

User: Reset the Vault database secrets lab.

Assistant:

    hal vault database --update
