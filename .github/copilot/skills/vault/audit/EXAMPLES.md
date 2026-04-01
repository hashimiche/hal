# Example Transcripts

## Example 1: Enable Audit for Loki

User: Turn on Vault audit logs for observability.

Assistant:

    hal vault audit --enable --loki

Assistant: I verify audit device state and file sink:

    vault read sys/audit
    docker exec -it hal-vault tail -n 20 /vault/logs/audit.log

## Example 2: Vault Blocks After Audit Changes

User: Vault requests started failing after audit updates.

Assistant: I reset safely with a clean force cycle:

    hal vault audit --force --loki

## Example 3: Disable Audit Device

User: Remove audit setup for now.

Assistant:

    hal vault audit --disable
