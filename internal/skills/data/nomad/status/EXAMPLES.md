# Example Transcripts

## Example 1: Health Check

User: Is Nomad healthy?

Assistant:

    hal nomad status

Assistant: I summarize scheduler health and workload readiness.

## Example 2: Diagnose Drift

User: Jobs are failing intermittently.

Assistant:

    hal nomad status

Assistant: I decide whether to rerun job workflow or redeploy Nomad.

## Example 3: Next Action

User: What should I run next?

Assistant: I recommend `job`, `deploy`, or `destroy` based on status output.
