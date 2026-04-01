# Example Transcripts

## Example 1: Status Before Changes

User: Check Terraform lab state first.

Assistant:

    hal terraform status

Assistant: I summarize whether deploy or destroy is the safer next action.

## Example 2: Partial Drift Diagnosis

User: Some resources look out of sync.

Assistant:

    hal terraform status

Assistant: I route to token refresh, deploy retry, or full reset.

## Example 3: Action Recommendation

User: What should I run now?

Assistant: I recommend `deploy`, `token`, or `destroy` based on the reported state.
