# Example Transcripts

## Example 1: Health Check

User: Is Consul healthy?

Assistant:

    hal consul status

Assistant: I summarize readiness and next action.

## Example 2: Partial Outage

User: Something is down in Consul.

Assistant:

    hal consul status

Assistant: I choose targeted redeploy or full reset based on result.

## Example 3: Action Recommendation

User: What now?

Assistant: I recommend deploy, destroy, or move on to dependent product deployment.
