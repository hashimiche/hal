# Example Transcripts

## Example 1: Enable OIDC Login

User: Set up human SSO login for Vault.

Assistant:

    hal vault oidc enable

Assistant: I verify OIDC config and default role:

    vault read auth/oidc/config
    vault read auth/oidc/role/default

Assistant: I confirm Authentik users `alice` (group `admin`) and `bob` (group `user-ro`) map to Vault external groups with matching policies.

## Example 2: Callback Error

User: Browser flow fails at callback.

Assistant: I validate callback URLs and role redirect settings, then provide precise `vault write` fixes.

## Example 3: Full OIDC Reset

User: Rebuild OIDC integration completely.

Assistant:

    hal vault oidc update

A: This cleans Vault OIDC mounts and policies, removes the Authentik application and OAuth2 provider, then re-provisions everything with fresh clientID/clientSecret — without restarting the Authentik stack.

## Example 4: Authentik admin credentials

User: What is the Authentik admin password?

A: The admin username is `akadmin`. Reveal the password with `hal vault oidc status` or `hal creds status`; it is also stored in `~/.hal/authentik/env` under `AUTHENTIK_BOOTSTRAP_PASSWORD`. Access the UI at `http://authentik.localhost:9100/if/admin/`. Reset it with `docker exec hal-authentik-server ak changepassword akadmin`.
