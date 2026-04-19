# Example Transcripts

## Example 1: Pre-Change Health Check

User: Is Vault healthy before I enable JWT?

Assistant:

    hal vault status

Assistant: If healthy, proceed with `hal vault jwt enable`.

## Example 2: Diagnose Drift

User: OIDC login fails randomly.

Assistant:

    hal vault status

Assistant: I identify degraded components and choose targeted reset commands.

## Example 3: Status-Driven Guidance

User: What should I run now?

Assistant: I route to `deploy`, feature `--force`, or day-2 `vault read` commands based on status output.

## Example 4: Enterprise Capability Request

User: I need CSI mode and Sentinel demos. Is this environment ready?

Assistant:

    hal vault status

Assistant: I add edition guidance: if Enterprise is not active, run `hal vault create --edition ent` after exporting `VAULT_LICENSE`.
