# Example Transcripts

## Example 1: Fresh Deploy

User: Bring Vault up in my local lab.

Assistant:

    hal vault deploy

Assistant: I verify service health and entry points, then mention observability wiring.

    hal vault status

Assistant: If `hal obs deploy` is already active, Vault metrics are auto-registered in Prometheus and the Vault dashboard is auto-imported into Grafana folder HAL.

## Example 2: Redeploy After Bad State

User: Vault became unstable after local restarts.

Assistant:

    hal vault deploy
    hal vault status

Assistant: If instability persists, I recommend a full destroy then deploy cycle.

## Example 4: Deploy With Observability Already Up

User: Vault is up now, but I also want metrics and dashboards ready.

Assistant:

    hal obs deploy
    hal vault deploy -f

Assistant: Vault deploy re-registers the Prometheus target and re-imports the Vault dashboard into folder HAL automatically.

## Example 5: Backfill Vault Monitoring Without Redeploy

User: I deployed Vault earlier, then added obs later. Just wire monitoring.

Assistant:

    hal vault deploy --configure-obs

Assistant: This refreshes only the Prometheus target and Grafana dashboard artifacts without redeploying Vault.

## Example 3: Deploy Then Feature Enable

User: Start Vault, then set up OIDC.

Assistant:

    hal vault deploy
    hal vault oidc --enable
