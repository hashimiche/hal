# Example Transcripts

## Example 1: Initial Terraform Lab Provisioning

User: Deploy Terraform lab resources and confirm readiness.

Assistant:

    hal terraform deploy
    hal terraform status

Assistant: I summarize provisioning success and any post-deploy steps required by this lab.

## Example 2: Recover From Stale or Drifted State

User: Deploy failed because the environment looks out of sync.

Assistant:

    hal terraform status
    hal terraform deploy
    hal terraform status

Assistant: I use the second status check to confirm whether reconciliation fixed drift.

## Example 3: Provision Then Configure Token Flow

User: Stand up Terraform lab and make sure token flow is configured.

Assistant:

    hal terraform deploy
    hal terraform token
    hal terraform status

Assistant: I verify both infrastructure availability and token-related readiness in one sequence.
