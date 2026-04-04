# Example Transcripts

## Example 1: User Asks For Removed Command

User: Set up token handling for Terraform in this lab.

Assistant:

    hal terraform deploy
    hal terraform workspace -e
    hal terraform status

Assistant: `hal terraform token` is deprecated; automation now handles token and workspace wiring.

## Example 2: Investigate Workspace Automation Regressions

User: Terraform auth started failing after environment changes.

Assistant:

    hal terraform status
    hal terraform workspace -e
    hal terraform status

Assistant: I compare status before and after workspace rewire to decide whether issue is integration-only or full environment drift.

## Example 3: End-to-End Bootstrap With Automation

User: Start from a clean state and include workspace automation.

Assistant:

    hal terraform deploy
    hal terraform workspace -e
    hal terraform status

Assistant: I verify the full chain is complete: resources provisioned, workspace wired, and status healthy.
