# Example Transcripts

## Example 1: Wire Nomad Into Observability

User: Add Nomad metrics to the Grafana dashboard.

A:

    hal obs status
    hal nomad obs create
    hal nomad obs status

## Example 2: Remove Nomad Monitoring

User: Remove the Nomad dashboard from Grafana.

A:

    hal nomad obs delete
