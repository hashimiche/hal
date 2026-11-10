summary: Vault signs people in against an OpenLDAP directory and also manages accounts in it.
read: hal vault ldap | hal vault status | hal creds status
actions: hal vault ldap enable | hal vault ldap update | hal vault ldap disable
docs:
- Vault LDAP auth: https://developer.hashicorp.com/vault/docs/auth/ldap#group-membership-resolution
- Vault LDAP secrets engine, static roles: https://developer.hashicorp.com/vault/docs/secrets/ldap#static-roles
- Vault LDAP secrets engine, service account libraries: https://developer.hashicorp.com/vault/docs/secrets/ldap#service-account-libraries

# Vault with LDAP: login and managed accounts

## The mechanism

Two different Vault features share the name LDAP, and this lab turns both on against
the same directory. They are easy to confuse.

The **LDAP auth method** is a login. A person gives Vault a user name and a
password. Vault does not store them. It uses a service account of its own to look the
user up in the directory, then tries to bind as that user with the password given. If
the bind works, Vault asks the directory which groups the user belongs to and maps
each group name to policies. The login therefore carries the policies of the user's
groups. Nothing about the user lives in Vault except what this login creates.

The **LDAP secrets engine** goes the other way. Vault holds a privileged account in
the directory and uses it to manage other accounts there. It does this in three ways:

- **Dynamic role.** Each request creates a new short-lived entry from a template, and
  the entry is deleted when the lease ends.
- **Static role.** One existing account is handed to Vault, which rotates its password
  on a schedule and returns the current one on request.
- **Service account library.** A set of existing accounts that clients check out and
  check in, one at a time, so a shared account is never used by two clients at once.

Finally, **root rotation** lets Vault change the password of its own privileged
account. After that, nobody but Vault knows it.

## How this lab stages it

- **Two containers, started only by this scenario.** `hal vault ldap enable` starts
  an OpenLDAP server (`hal-openldap`, reachable from the host at
  `ldap.localhost:1389`) and phpLDAPadmin (`hal-phpldapadmin`, a browser UI at
  `https://phpldapadmin.localhost:8082`). Nothing here is shared with another lab.
  Both images are pinned to the `linux/amd64` platform, and their tags can be
  overridden with flags (`hal vault ldap --help` lists them).
- **A seeded directory** under `dc=hal,dc=local`, with an `ou=users` and an
  `ou=groups`. It holds the demo users `alice` and `bob`, three service accounts
  (`app-service`, `lib-service-1`, `lib-service-2`) and two groups. The group `admin`
  has `alice` as its member. The group `reader` has `bob`.
- **Auth method at `auth/ldap`.** It reaches the directory by the name `hal-openldap`
  over plain `ldap://` on the lab network, without TLS. It looks users up under
  `ou=users` by their `cn`, and groups under `ou=groups`. The service account it
  binds with is the directory's own read-only user, a different entry from the
  directory administrator. Two group mappings are written: `admin` to the policy
  `ldap-admin`, and `reader` to the policy `ldap-reader`. The lab sets no group
  filter, so Vault uses its default one, which matches the usual member attributes.
- **Policies.** `ldap-admin` has full rights on the KV v2 mount `kv-ldap` and on the
  whole `ldap` secrets engine. `ldap-reader` can read one demo value in `kv-ldap`,
  and can only use three things in the engine: the dynamic role `dynamic-reader`,
  the static role `static-app`, and the check-out and check-in of the library
  `dev-pool`.
- **Secrets engine at `ldap`.** It binds as the directory administrator. It carries
  the dynamic role `dynamic-reader` (a template that creates an entry under
  `ou=users`, one hour default lease), the static role `static-app` (account
  `app-service`, rotated every 24 hours) and the library `dev-pool` (accounts
  `lib-service-1` and `lib-service-2`, one hour lease, 24 hours at most).
- **Root rotation is the last step of `enable`.** Right after configuring the engine,
  HAL asks Vault to rotate the administrator's password. The seed password is a boot
  value and stops being valid at once. That is why phpLDAPadmin cannot sign in as the
  directory administrator afterwards: only Vault knows the new password.

The two bind accounts are separate on purpose. Rotating the administrator's
password affects the secrets engine only. The auth method keeps working because it
binds with the read-only user, which is not rotated.

## What to expect

- Signing in as `alice` gives the `ldap-admin` policy, and as `bob` the `ldap-reader`
  policy. A user who exists in the directory but belongs to neither group can bind
  but gets no group policy.
- The group names must match. The mapping in Vault is by the group's `cn`, and the
  group is called `reader`, not `readers`. A mapping written for another name would
  quietly grant nothing.
- The user passwords of the two demo users stay known to the lab. The service
  accounts do not: Vault takes them over, so their current passwords come from the
  secrets engine, not from the seed.
- `enable` waits for OpenLDAP to accept the seed, trying about ten times at three
  second intervals. If the directory never answers, the command exits with an
  error and stops there: the Vault side is not configured, and the two containers
  it started are left running. `hal vault ldap update` resets that.
- Because rotation changes a password inside the directory, a directory that is
  rebuilt (`update`) starts from the seed again and Vault rotates it again.

## Commands

- `hal vault ldap` with no action is read-only. It reports whether OpenLDAP and
  phpLDAPadmin are running, whether `auth/ldap` and the secrets engine `ldap` exist,
  and the next step. `hal vault status` lists `ldap` among the active auth methods
  and `ldap (ldap)` among the secrets engines.
- `enable` needs a running, healthy Vault and stops with an error otherwise.
- `update` is a full reset, not a patch. It removes what `disable` removes, then runs
  `enable` again, so the directory starts from the seed and every dynamic account
  and check-out is gone. Use it when the status says the environment is partially
  degraded.
- `disable` goes in a deliberate order: Vault is cleaned up while the directory is
  still there, and the containers are removed last. It revokes the leases of the
  `ldap` secrets engine, disables `auth/ldap`, unmounts `ldap` and `kv-ldap`,
  deletes the two policies, and only then removes the two containers. It also removes the Vault CLI's local
  token file in the home directory, so a CLI login made earlier is forgotten.

The logins for the two demo users are lab secrets, so they are not written in this
file. `hal creds status` shows them.
