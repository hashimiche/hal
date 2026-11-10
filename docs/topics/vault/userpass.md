summary: A username and password login stored in Vault, with metadata kept on an identity entity.
read: hal vault userpass | hal vault status | hal creds status
actions: hal vault userpass enable | hal vault userpass update | hal vault userpass disable
docs:
- Vault userpass auth: https://developer.hashicorp.com/vault/docs/auth/userpass#authentication
- Vault identity entities and aliases: https://developer.hashicorp.com/vault/docs/concepts/identity#entities-and-aliases

# Vault userpass auth with an identity entity

## The mechanism

Userpass is the simplest login method Vault offers. Vault keeps its own list of users,
each with a password it stores as a hash, and a set of policies. A person sends a
user name and a password to the login path for the method, Vault checks the hash,
and it returns a token that carries the user's policies. Nothing is looked up outside
Vault, so there is no directory or identity provider to run.

The userpass method has no field for extra facts about a user. Vault's identity
system fills that gap. An **entity** is one record for a person, and it can hold
metadata. An **alias** links an entity to an account in one auth method, by name and
by the method's mount. When a user logs in through an alias, Vault attaches the
entity to the token it issues, so the metadata follows the person across logins and
across auth methods, and it appears in what Vault reports about the token.

## How this lab stages it

- **No containers.** Userpass runs inside Vault, so `hal vault userpass enable` adds
  no service to the lab. It needs only the running Vault server (`hal-vault`).
- **One demo user,** `michaelScott`, with a password that is a lab secret and is
  not written here. `hal creds status` shows it.
- **Auth method at `auth/userpass`.** The user's record sets one policy,
  `userpass-demo`, that the token inherits.
- **The policy is broad on purpose.** `userpass-demo` grants create, read, update,
  delete and list on every path. It is a demo of an identity, not a model of least
  privilege. Do not copy it into a real deployment.
- **The entity** is named after the user and carries four metadata keys: `branch`,
  `company`, `role` and `fun_fact`. They are data for the lesson, not controls that
  Vault enforces.
- **The alias** joins that entity to the `userpass/` mount, using the name of the user.
  The entity and its alias are the two identity objects the lab creates.
- **Verified at enable.** Right after it creates the user, HAL logs that user in
  once to prove the login works. A failed check stops the command with an error.

The command is `hal vault userpass`, and `hal vault up` is an alias of it.

## What to expect

- The login returns a token. The token's policies are the ones on the user record,
  so `userpass-demo` and nothing more. The metadata is not a policy and grants nothing.
- The entity id is printed at enable. Looking it up in Vault shows the four metadata
  keys, and that is the place to read the identity side of the lesson.
- Because the entity is linked by alias, a second login method with a user of the same
  name would get its own alias, not a merged entity, unless an alias is added to the
  same entity. This lab has no second method for the same name.
- Enable removes the `userpass/` mount first, then mounts it again. Any user already on
  that mount is removed. The lab does not check first, so do not run it on a Vault
  that holds userpass users you want to keep.

## Commands

- `hal vault userpass` with no action is read-only. It reports whether `auth/userpass`
  is configured and names the next step. `hal vault status` lists `userpass` among the
  active auth methods.
- `enable` needs a running, healthy Vault and stops with an error otherwise. It
  creates the policy, the user, the entity and the alias, then does the login check.
- `update` runs the teardown and then `enable` again. That is a reset: the user and the
  entity are recreated with the same name and the same metadata.
- `disable` disables `auth/userpass`, deletes the policy `userpass-demo`, and looks up
  the entity by its name and deletes it. Disabling the auth mount also removes the
  users stored under it.

The demo login is a lab secret, so it is not written in this file. `hal creds status`
shows it.
