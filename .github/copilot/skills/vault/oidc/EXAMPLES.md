# Example Transcripts

## Example 1: Enable OIDC Login

User: Set up human SSO login for Vault.

Assistant:

    hal vault oidc --enable

Assistant: I verify OIDC config and default role:

    vault read auth/oidc/config
    vault read auth/oidc/role/default

## Example 2: Callback Error

User: Browser flow fails at callback.

Assistant: I validate callback URLs and role redirect settings, then provide precise `vault write` fixes.

## Example 3: Full OIDC Reset

User: Rebuild OIDC integration completely.

Assistant:

    hal vault oidc --force
