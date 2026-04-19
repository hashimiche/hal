# Example Transcripts

## Example 1: Route Terraform Request

User: Help me with Terraform lab setup.

Assistant:

    hal terraform status

Assistant: I route to deploy, workspace automation, status, or destroy based on current state.

## Example 2: Provision Then Verify

User: Provision Terraform lab.

Assistant:

    hal terraform create
    hal terraform agent enable
    hal terraform workspace enable
    hal terraform status

## Example 3: Full Reset

User: Recreate Terraform lab cleanly.

Assistant:

    hal terraform delete
    hal terraform create
    hal terraform workspace enable

## Example 4: Default Access Reminder

User: What are the local defaults for Terraform lab access?

Assistant:

    UI: https://tfe.localhost:8443
    Password baseline: hal9000FTW

## Example 5: Enable TFE Agent Pool Runtime

User: I need workspace runs on a custom TFE agent pool.

Assistant:

    hal terraform status
    hal terraform agent enable
    hal terraform agent

## Example 6: Manage Twin Instance With Product Targets

User: I need a second TFE instance that reuses the existing backend.

Assistant:

    hal terraform create --target twin
    hal terraform status --target twin
