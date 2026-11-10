summary: Terraform Enterprise single sign-on through a SAML identity provider, Authentik, with optional SCIM provisioning.
read: hal terraform status | hal creds status
actions: hal terraform saml enable | hal terraform saml update | hal terraform saml disable
docs:
- SAML attributes: https://developer.hashicorp.com/terraform/enterprise/saml/configuration#attributes
- Team membership mapping: https://developer.hashicorp.com/terraform/enterprise/saml/configuration#team-membership-mapping
- SCIM complements SAML: https://developer.hashicorp.com/terraform/enterprise/scim#scim-complements-saml

# Terraform Enterprise SAML single sign-on

This topic is about the local Terraform Enterprise runtime. The lab has no HCP
Terraform mode, and SAML as set up here is a Terraform Enterprise feature.

## The mechanism

With SAML, Terraform Enterprise stops checking passwords and trusts an identity
provider (IdP) instead. A person who clicks the SSO button is sent to the IdP. After
signing in there, the browser carries a signed assertion back to Terraform
Enterprise, which is the service provider (SP). The assertion names the user and can
carry extra attributes.

Three things have to agree on both sides. The IdP must send the assertion to the
SP's assertion consumer (ACS) URL, and the audience in it must equal the SP's entity
ID. Terraform Enterprise must hold the IdP's signing certificate and its sign-in
URL. After that, attributes do the rest: one says which username to use, one lists
the teams the user belongs to, and one can say whether the user is a site admin.

SCIM is a separate protocol that works beside SAML. SAML runs at the moment of
login. SCIM lets the IdP create users and teams ahead of time and keep them in sync.
When SCIM is on, Terraform Enterprise ignores the team information in assertions and
treats SCIM as the source of truth, and users must exist before they can sign in.

## How this lab stages it

- **Authentik is the IdP.** `hal terraform saml enable` starts the Authentik stack,
  or reuses the one already running, and registers this target as a consumer of it.
  Authentik is shared: `hal vault oidc` uses the same stack, so it stops only when
  no consumer is left. Its admin UI is at `http://authentik.localhost:9100`.
- **Two demo users in two groups.** HAL creates the groups `admins` and `devs` and
  the users `alice` (in `admins`) and `bob` (in `devs`) with the lab's demo
  password. The password is not written here. `hal terraform saml --help` and the
  output of `enable` list the demo users, and `hal creds status` shows the Authentik
  admin entry.
- **The SP identifiers are portless.** The ACS URL and the audience are built from
  `https://tfe.localhost` without the port, because Terraform Enterprise derives
  them from its hostname setting, and that setting has no port. The Authentik
  provider is created with exactly those values.
- **A small proxy fixes the port.** Authentik's response would POST the assertion
  to the portless address. HAL starts a SAML proxy container
  (`hal-authentik-saml-proxy`, host port 9102) that rewrites the form action to the
  port the TFE proxy actually publishes. The sign-in URL stored in Terraform
  Enterprise points at that proxy, not at Authentik's own port.
- **The attribute names are set explicitly.** HAL turns SAML on with the username
  attribute `Username`, the team attribute `MemberOf` and a provider type of
  `saml`, and keeps SSO API sessions for 14 days. Authentik is given two matching
  property mappings: the username, and the names of the user's groups.
- **Teams come from one of two places.** Without `--scim`, HAL creates the teams
  `admins` and `devs` in the organization itself, because Terraform Enterprise
  adds a user to a team whose name matches the assertion but never creates the
  team. `admins` gets manage rights on workspaces, projects, modules and providers.
  `devs` gets read rights on workspaces and projects. With `--scim`, HAL skips that
  step and lets Authentik provision the users, the groups and one group-to-team link
  per group.

## What differs from the textbook

- **Site admin through SAML does not happen here.** HAL sets the site-admin
  attribute name to `SiteAdminRole`, where the vendor default is `SiteAdmin`, and
  the site-admin role to `site-admins`. But Authentik emits only the two mappings
  above, and neither demo group is called `site-admins`. So, from the code, neither
  demo user is expected to become a site admin by signing in with SAML.
- **SCIM is gated and destructive for teams.** `--scim` needs Terraform Enterprise
  2.0 or later. The check is the last step of `enable`, so on an older version SAML
  is already configured and only SCIM is skipped. When the check passes, HAL deletes
  any `admins` and `devs` teams that a previous run without SCIM created, so SCIM
  can create them fresh.
- **The SCIM endpoint is the proxy.** Authentik reaches Terraform Enterprise at
  `/scim/v2` on the shared TFE proxy over the lab network, not through the host
  port, and certificate checking is switched off for that provider.
- **The default organization is `hal`, for the twin too.** The `--tfe-org` flag
  defaults to `hal` whichever target you pick, and the command creates the
  organization if it is missing. The twin's own foundation organization is
  `hal-bis`, so `--target twin` without `--tfe-org hal-bis` puts the demo teams in a
  second organization named `hal` on the twin.
- **One target at a time.** `--target both` is refused. Use `primary` or `twin`.

## What to expect

- After `enable`, the Terraform Enterprise login page offers SSO. Signing in as
  `alice` or `bob` lands the user in the matching team.
- `update` without flags removes the Terraform Enterprise SAML settings and the
  Authentik application and providers, then enables again. `update --scim --sync`
  does none of that and only re-pushes the group membership.
- A login that fails at the last step is usually a mismatch between the ACS URL or
  audience and what Authentik was given, or a certificate problem: the
  sign-in URL has to point at the SAML proxy.
- `disable` removes the SAML and SCIM settings from Terraform Enterprise and
  removes this target's consumer record. If nothing else uses Authentik, it stops
  the stack and removes its volumes. Otherwise it stops the SAML proxy and deletes
  only this target's application and providers.

## Commands

- `hal terraform saml` with no action shows the status, but it is not purely
  read-only. To read the SAML settings it gets an API token, and that may mint and
  cache one and create the organization. `hal terraform status` is the safe read.
- `enable` needs the Terraform Enterprise core to be running. It starts Authentik,
  the SAML proxy, provisions Authentik, fetches its metadata, then configures
  Terraform Enterprise.
- `update` and `disable` are described above. Flags such as `--scim`, `--sync`, the
  Authentik image and tag, and the organization are listed by
  `hal terraform saml --help`.

The Authentik admin login is a lab secret, so it is not written in this file. It is
the Authentik entry under Terraform Enterprise in the credentials that
`hal terraform status` prints, and `hal creds status` shows the same tree.
