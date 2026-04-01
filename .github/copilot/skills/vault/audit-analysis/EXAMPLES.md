# Example Transcripts

## Example 1: Who Accessed A Path

User: Who read `kv-oidc/data/team1` today?

Assistant: I query audit events by path plus operation and return identities and timestamps.

## Example 2: Request Trace

User: Trace request `req_abc123`.

Assistant: I run a request trace workflow and summarize sequence, status, and actor.

## Example 3: Security Review

User: Any suspicious Vault activity in the last hour?

Assistant: I aggregate failed logins and privileged actions, then return a concise risk summary and next checks.
