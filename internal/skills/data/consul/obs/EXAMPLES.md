# Example Transcripts

## Example 1: Wire Consul Into Observability

User: I want to see Consul metrics in Grafana.

A:

    hal obs status
    hal consul obs create
    hal consul obs status

## Example 2: Remove Consul Monitoring

User: Remove the Consul dashboard from Grafana.

A:

    hal consul obs delete
