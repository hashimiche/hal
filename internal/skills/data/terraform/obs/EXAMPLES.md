# Example Transcripts

## Example 1: Wire TFE Into Observability

User: I want to see Terraform Enterprise metrics in Grafana.

A:

    hal obs status
    hal terraform obs create
    hal terraform obs status

## Example 2: Wire Both Primary And Twin Into Obs

User: I have the twin running too. Wire both into Grafana.

A:

    hal terraform obs create --target both
    hal terraform obs status

## Example 3: Refresh After TFE Update

User: I recreated TFE. Do I need to redo the obs wiring?

A:

    hal terraform obs update

## Example 4: Remove TFE Monitoring Artifacts

User: Remove Terraform from Grafana but keep the obs stack running.

A:

    hal terraform obs delete
    hal terraform obs status
