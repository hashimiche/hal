# Example Transcripts

## Example 1: Fresh Deploy

User: Bring Vault up in my local lab.

Assistant:

    hal vault deploy

Assistant: I verify service health and entry points.

    hal vault status

## Example 2: Redeploy After Bad State

User: Vault became unstable after local restarts.

Assistant:

    hal vault deploy
    hal vault status

Assistant: If instability persists, I recommend a full destroy then deploy cycle.

## Example 3: Deploy Then Feature Enable

User: Start Vault, then set up OIDC.

Assistant:

    hal vault deploy
    hal vault oidc --enable
