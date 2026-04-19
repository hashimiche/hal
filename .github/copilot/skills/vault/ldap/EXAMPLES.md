# Example Transcripts

## Example 1: Enable LDAP Lab

User: Configure LDAP auth and secrets for Vault.

Assistant:

    hal vault ldap enable

Assistant: I verify LDAP auth and secrets mounts, then provide login and secrets tests.

    vault login -method=ldap username=bob password=password
    vault read ldap/creds/dynamic-reader
    vault read ldap/static-cred/static-app

## Example 2: LDAP Login Failure

User: `vault login -method=ldap` fails for bob.

Assistant:

    hal vault ldap
    vault read auth/ldap/config

Assistant: I compare bind settings, userdn/groupdn, and group-policy mapping.

## Example 3: Full LDAP Reset

User: Rebuild LDAP from scratch.

Assistant:

    hal vault ldap --force
