# Example Transcripts

## Example 1: Fresh Deploy

User: Bring Vault up in my local lab.

Assistant:

    hal vault create

Assistant: I verify service health and entry points, then mention observability wiring.

    hal vault status

Assistant: I remind the user Vault is in dev mode with root token `root`. Observability artifacts are managed explicitly with `hal vault obs create`.

## Example 2: Redeploy After Bad State

User: Vault became unstable after local restarts.

Assistant:

    hal vault create
    hal vault status

Assistant: If instability persists, I recommend a full destroy then deploy cycle.

## Example 4: Deploy With Observability Already Up

User: Vault is up now, but I also want metrics and dashboards ready.

Assistant:

    hal obs create
    hal vault create
    hal vault obs create

Assistant: Vault obs create writes/refreshes the Prometheus target and Vault dashboard artifacts in the HAL obs directories.

## Example 5: Backfill Vault Monitoring Without Redeploy

User: I deployed Vault earlier, then added obs later. Just wire monitoring.

Assistant:

    hal vault obs create

Assistant: This refreshes only the Prometheus target and Grafana dashboard artifacts without redeploying Vault.

## Example 3: Deploy Then Feature Enable

User: Start Vault, then set up OIDC.

Assistant:

    hal vault create
    hal vault oidc enable

## Example 6: Deploy Vault Enterprise

User: I need Enterprise features for this Vault lab.

Assistant:

    export VAULT_LICENSE='your_license_string'
    hal vault create --edition ent --update
    hal vault status
