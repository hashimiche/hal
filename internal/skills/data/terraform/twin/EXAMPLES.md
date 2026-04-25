# Terraform Twin Skill Examples

## Example 1: Enable The Twin Instance

User: Spin up a second TFE instance alongside the primary.

A:

    hal terraform status
    hal terraform twin enable
    hal terraform twin

## Example 2: Check Twin Status

User: Is the twin TFE running?

A:

    hal terraform twin
    hal terraform status --target twin

## Example 3: Wire VCS Automation On Twin

User: Set up GitLab workspace wiring on the twin.

A:

    hal terraform twin
    hal terraform vcs-workflow enable --target twin
    hal terraform vcs-workflow --target twin

## Example 4: Open API Helper For Twin

User: I need a Terraform shell pointing at the twin TFE.

A:

    hal terraform api-workflow enable --target twin

## Example 5: Tear Down The Twin Only

User: Remove the twin TFE but keep the primary running.

A:

    hal terraform twin disable --auto-approve
    hal terraform status

## Example 6: Recreate A Stale Twin

User: The twin feels stale. Recreate it cleanly.

A:

    hal terraform twin update
    hal terraform twin
