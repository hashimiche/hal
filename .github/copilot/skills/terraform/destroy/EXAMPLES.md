# Example Transcripts

## Example 1: Planned Terraform Teardown

User: Tear down Terraform resources for a clean lab reset.

Assistant:

    hal terraform destroy
    hal terraform status

Assistant: I confirm that managed resources were removed and the environment is clean.

## Example 2: Cleanup After Failed Provisioning

User: Apply failed midway, clean up partial resources.

Assistant:

    hal terraform destroy
    hal terraform status

Assistant: If leftovers still appear, I suggest one more destroy pass before any redeploy.

## Example 3: Destroy and Rebuild Sequence

User: Remove everything and rebuild from scratch.

Assistant:

    hal terraform destroy
    hal terraform deploy
    hal terraform status

Assistant: I validate the rebuilt state so the user can continue safely.
