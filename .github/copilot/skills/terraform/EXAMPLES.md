# Example Transcripts

## Example 1: Route Terraform Request

User: Help me with Terraform lab setup.

Assistant:

    hal terraform status

Assistant: I route to deploy, token, status, or destroy based on current state.

## Example 2: Provision Then Verify

User: Provision Terraform lab.

Assistant:

    hal terraform deploy
    hal terraform status

## Example 3: Full Reset

User: Recreate Terraform lab cleanly.

Assistant:

    hal terraform destroy
    hal terraform deploy
