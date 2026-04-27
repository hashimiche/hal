# Example Transcripts

## Example 1: Wire Vault Into Observability

User: I want to see Vault metrics in Grafana.

A:

    hal obs status
    hal vault obs create
    hal vault obs status

## Example 2: Vault Was Deployed Before Obs — Backfill

User: I deployed Vault first, then obs. How do I backfill the dashboard?

A:

    hal obs status
    hal vault obs create

A: After this, the Vault dashboard appears in Grafana under the HAL folder.

## Example 3: Refresh After Vault Config Change

User: I updated Vault. Do I need to redo the obs wiring?

A:

    hal vault obs update

## Example 4: Remove Vault Monitoring Artifacts

User: I want to remove Vault from Grafana but keep the obs stack running.

A:

    hal vault obs delete
    hal vault obs status
