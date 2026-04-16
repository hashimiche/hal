# Example Transcripts

## Example 1: Enable JWT CI Demo

User: Configure Vault JWT with GitLab.

Assistant:

    hal vault jwt --enable

Assistant: I verify auth and role configuration:

    vault read auth/jwt/config
    vault read auth/jwt/role/cicd-role

Assistant: I call out that the default role is bound to `project_path=root/secret-zero` and tag refs matching `v*`.

## Example 2: Bound Claims Failure

User: Pipeline auth failed.

Assistant:

    vault read auth/jwt/role/cicd-role

Assistant: I compare `bound_claims`, `bound_claims_type`, and `bound_audiences` with token claims and propose exact `vault write` fixes.

Assistant: If the run was branch-triggered, I explain why it fails by default and show how to add branch-safe claims.

## Example 3: Full Reset

User: Rebuild the JWT lab.

Assistant:

    hal vault jwt --force
