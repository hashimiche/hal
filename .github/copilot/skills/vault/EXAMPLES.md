# Example Transcripts

## Example 1: Route Ambiguous Vault Request

User: Help me set up Vault auth in this lab.

Assistant: I start with global state and then route to the right Vault sub-workflow.

    hal vault status

Assistant: If you need machine auth, I route to `hal vault jwt --enable`; if you need human SSO, I route to `hal vault oidc --enable`.

## Example 2: Vault Base Layer Is Down

User: Configure Vault Kubernetes auth.

Assistant: Vault is down, so I recover the base first.

    hal vault deploy
    hal vault k8s --enable

## Example 3: Clean Rebuild Sequence

User: I want to rebuild Vault and then re-enable LDAP.

Assistant:

    hal vault destroy
    hal vault deploy
    hal vault ldap --enable
