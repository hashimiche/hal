# Example Transcripts

## Example 1: Configure Token Flow

User: Set up token handling for Terraform in this lab.

Assistant:

    hal terraform token
    hal terraform status

Assistant: I confirm token-related setup completed and that Terraform lab status is still healthy.

## Example 2: Investigate Authentication Regressions

User: Terraform auth started failing after environment changes.

Assistant:

    hal terraform status
    hal terraform token
    hal terraform status

Assistant: I compare status before and after token refresh to decide whether issue is auth-only or full environment drift.

## Example 3: End-to-End Bootstrap With Token Setup

User: Start from a clean state and include token flow.

Assistant:

    hal terraform deploy
    hal terraform token
    hal terraform status

Assistant: I verify the full chain is complete: resources provisioned, token path configured, and status healthy.
