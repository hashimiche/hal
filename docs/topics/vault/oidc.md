summary: A person signs in to Vault from a browser through Authentik, and the groups claim sets their policy.
read: hal vault oidc | hal vault status | hal creds status
actions: hal vault oidc enable | hal vault oidc update | hal vault oidc disable
docs:
- Vault OIDC auth: https://developer.hashicorp.com/vault/docs/auth/jwt#oidc-authentication
- Vault identity groups: https://developer.hashicorp.com/vault/docs/concepts/identity#external-vs-internal-groups
- Authentik and Vault: https://integrations.goauthentik.io/security/hashicorp-vault/#external-groups

# Vault OIDC auth with Authentik

## The mechanism

This is browser single sign-on for a person. Vault never sees the person's password.
It sends the browser to an identity provider (IdP), the person signs in there, and
the IdP sends the browser back to Vault with a one-time code. Vault exchanges the
code for an ID token over a direct call to the IdP, checks the signature and the
issuer against the IdP's discovery document, and reads the claims.

Two claims matter. One names the user (here `preferred_username`). The other lists
the user's groups. The role on the Vault side says which claim holds the groups.
Vault does not trust group names by themselves, so something has to turn a name in
the claim into policies. That is the job of an external group with a group alias:
the external group carries the policies, and the alias is the name the IdP uses for
that group, bound to this auth mount. When the token's groups claim contains the
alias name, the login inherits the group's policies.

This is not the machine login of the JWT topic (`vault/jwt`), where a CI job
presents a token signed by its platform and no browser is involved.

## How this lab stages it

- **Authentik is the IdP.** `hal vault oidc enable` starts the Authentik stack
  (three containers: a PostgreSQL database, the server and the worker), or reuses
  it if another lab already started it. Authentik answers on the alias
  `authentik.localhost`, port 9100, both from the host browser and from containers
  on the lab network. That keeps the issuer in every token identical, so Vault's
  check against it holds.
- **Authentik is shared.** The Terraform Enterprise SAML scenarios (`hal tf saml`,
  primary and twin) and the Agentic IAM lab (`vault/agentic-iam`) use the same
  stack. Each scenario registers as a consumer, and the stack stops only when none
  is left.
- **Two demo users and two groups.** HAL creates the users `alice` and `bob` in
  Authentik, in the groups `admin` and `user-ro`. Their passwords are lab secrets
  and are not written here.
- **An OAuth2 provider and an application** named `vault-oidc-provider` and
  `HashiCorp Vault` in Authentik, with strict redirect URIs: the CLI listener on
  port 8250 (both `localhost` and `127.0.0.1`) and the Vault UI callback at
  `vault.localhost:8200`.
- **Vault side.** The auth method is mounted at `auth/oidc` with the Authentik
  issuer as discovery URL. One role, `default`, is the default role. It reads the
  user from `preferred_username` and the groups from the `groups` claim, and it
  asks for the scopes `openid`, `profile`, `email` and `groups`.
- **Groups to policies.** Vault gets two external groups, `admin` and `user-ro`,
  each with a group alias of the same name on `auth/oidc`. The policy `admin` is
  allowed everything. The policy `user-ro` can list and read one path, `team1`, in
  a KV v2 mount named `kv-oidc`. The value stored there is a demo value.

## What to expect

- Sign in from the Vault UI at `vault.localhost:8200`: choose the OIDC method and
  leave the role blank, since the role is the default one. Authentik asks for the
  user, then returns to Vault. The command line uses the same flow and the same
  default role.
- `alice` lands with the full-access policy, `bob` with the read-only one. A user
  who belongs to neither Authentik group signs in but gets no group policy, so the
  login works and the access is empty.
- The group names are the link. Renaming a group in Authentik without changing the
  alias in Vault breaks the mapping silently: the login still succeeds, the policy
  is gone.
- Before it configures the auth method, `enable` checks that the Vault container
  can fetch Authentik's discovery document, and retries for about a minute. If the
  write of the auth method still fails on that discovery check, it retries a few
  more times before it gives up.
- Authentik has its own admin account, used to manage the IdP itself. It is not a
  Vault login.

## Optional: SCIM (Enterprise, beta)

`hal vault oidc enable --scim` is for Vault Enterprise only and refuses to run on
Community Edition. With it, Authentik pushes users and groups into Vault over SCIM
instead of the lab creating the external groups itself: HAL activates Vault's SCIM
feature, creates a SCIM client with its own narrow policy and a bearer token for it,
and configures an outbound SCIM provider in Authentik. If the lab had already
created the `admin` and `user-ro` groups by the plain path, `--scim` removes them
first so SCIM can own them. The code marks SCIM as beta, and group membership
updates can lag, so `hal vault oidc update --scim --sync` re-pushes everything.
The plain path above is the reference; use SCIM only to see provisioning.

## Commands

- `hal vault oidc` with no action is read-only. It shows the Authentik stack, whether
  `auth/oidc` and `kv-oidc` exist in Vault, and the next step. `hal vault status`
  lists `oidc` among the active auth methods.
- `enable` needs a running, healthy Vault and stops with an error otherwise. It
  starts or reuses Authentik, registers the consumer, provisions users, groups,
  provider and application, checks that Vault can reach the discovery URL, then
  configures Vault. The Authentik image and tag can be overridden with flags;
  `hal vault oidc --help` lists them.
- `update` removes the Vault-side configuration and the Authentik application and
  provider, then provisions again with fresh client credentials. Authentik itself
  keeps running.
- `disable` disables `auth/oidc`, unmounts `kv-oidc`, deletes the policies `admin`
  and `user-ro` and the two identity groups, and releases this scenario's claim on
  Authentik. The Authentik stack and its volume are removed only when no other
  scenario still uses it. When one does, the stack stays, and only the SCIM provider
  that pointed at Vault is removed.

When a shared Authentik stays up, `disable` leaves the demo users, groups and the
OAuth2 application in it. They are removed only with the stack.

The logins for the demo users and for the Authentik admin are lab secrets, so they
are not written in this file. `hal creds status` shows them. `hal vault oidc` also
shows how to reach the Authentik admin.
