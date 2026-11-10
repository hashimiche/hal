summary: Boundary proxies MariaDB sessions through a TCP target, with credentials brokered from Vault when asked.
read: hal boundary mariadb | hal boundary status | hal creds status
actions: hal boundary mariadb enable | hal boundary mariadb update | hal boundary mariadb disable
docs:
- Brokered credentials: https://developer.hashicorp.com/boundary/docs/vault#brokered-credentials
- Vault credential store: https://developer.hashicorp.com/boundary/docs/domain-model/credential-stores#vault-token-requirements
- Vault generic credential library: https://developer.hashicorp.com/boundary/docs/domain-model/credential-libraries#vault-generic-credential-library-attributes

# Boundary MariaDB target

## The mechanism

A database account that a person types in is a long-lived secret. Boundary can
remove that secret from the person's hands in two steps. First it proxies the
session, so the database is only reachable through a target. Then it can ask Vault
for a credential when the session starts, which is called brokering. Boundary keeps
a credential store that points at Vault, a credential library that names the Vault
path to read, and a link from the target to the library. The person receives a
credential that Vault created for that session, and Boundary can revoke the lease
when the session ends.

Brokering is not injection. A brokered credential is shown to the person, who then
uses it. Injection puts it into the session without showing it, and only the `ssh`
and `rdp` target types support it. A `tcp` target, which is what a database needs,
can only broker.

## How this lab stages it

There are two ways to run it, and they differ in which database the target points at.

- **Standalone is the default.** `hal boundary mariadb enable` starts a MariaDB
  container, `hal-boundary-target-mariadb`, on the lab network and publishes port
  3306 on the host. It has a database `targetdb` and an `admin` user. Their
  passwords are lab secrets: `hal creds status` lists them as the MariaDB target of
  Boundary. No Vault is involved.
- **With `--with-vault`, the target points at Vault's database.** HAL starts no
  MariaDB. It attaches the target to `hal-vault-mariadb`, the database of the Vault
  database lab, and wires Boundary to Vault so that the session gets a dynamic
  credential. This needs Vault running, with `hal vault database enable` already
  done: HAL checks that the Vault role `dba-role` exists and stops with a pointer to
  that command otherwise.

Both ways build the same Boundary objects, in their own scopes:

- An organization `hal-academy` holds a project `db-infrastructure`.
- The project has a static host catalog `mariadb-catalog`, a host `mariadb-server`
  whose address is the container name (the worker resolves it on the lab network), a
  host set `mariadb-set`, and the target `mariadb-secure-access`. The target is of
  type `tcp`, with default port 3306.
- A password auth method `lab-auth` in the organization has one account, `dba-user`,
  attached to the user `DBA Admin`. The role `dba-role` in the project carries the
  grant `ids=*;type=*;actions=*`, so that user can do everything in the project,
  including opening a session.

With `--with-vault`, HAL adds three things:

- **A Vault policy `boundary-controller`.** It holds the paths that Boundary's
  documentation lists for a Vault credential store (look up, renew and revoke its own
  token, renew and revoke leases, check its own capabilities) plus `database/creds/dba-role`.
- **A Vault token for Boundary.** HAL creates it as an orphan, periodic token with a
  24 hour period and that policy, and gives it to Boundary. Those are the properties
  Boundary requires of a store token. The value is never printed.
- **A credential store `hal-vault-store`** in the project, pointing at
  `http://hal-vault:8200`, and **a library `mariadb-dba-creds`** that reads
  `database/creds/dba-role` with GET. The library is attached to the target as a
  brokered credential source.

The Vault address is plain HTTP, and HAL's own calls to Vault use the well-known dev
token, so this path is written for Vault in dev mode. Prod mode serves TLS and
generates its own root token, and this lab has not been checked against it.

The role `dba-role` is the one `hal vault database enable` creates for its MariaDB
backend. Each read creates a new MariaDB user with all privileges on the server, and
Vault deletes it when the lease ends. The role for the Oracle backend has another
name, so the check does not accept it.

## What to expect

- `enable` needs the controller. Without `hal-boundary` running it stops and points
  to `hal boundary create`. With `--with-vault` it also needs `hal-vault`.
- **Port 3306 is taken on the host by whichever MariaDB starts first.** The
  standalone container and `hal-vault-mariadb` both publish it. Starting the
  standalone target while the Vault database lab runs fails to start the
  container. Pick one of the two ways.
- **`hal-vault-mariadb` is not Boundary's.** It belongs to the Vault labs, which
  register as its consumers (`vault-database`, `vault-agentic-iam`). Boundary is not
  a consumer: it creates nothing in that database and only borrows the `dba-role`
  that the Vault lab made. So `hal boundary mariadb disable` never removes it, and
  `hal vault database disable` or `hal vault delete` removes it once no Vault lab
  needs it, even while the Boundary target still points there. After that the target
  has nothing to connect to until the Vault database lab is enabled again.
- **Switching between the two ways takes `update`.** Setup reuses a Boundary object
  that already has the expected name and does not change it, so `enable` after the
  other way would keep the old host address. `update` clears the objects first. Pass
  `--with-vault` again with it, because the flag is not remembered.
- **`hal creds status` has no entry for `dba-user`.** It lists the controller admin,
  the SSH users and the standalone database. Only `--with-vault` prints a way in for
  this user: the end of its `enable` output has a `boundary authenticate` line and a
  `boundary connect mysql` line for the target, with the login in the first. Both
  need the Boundary CLI on your machine, which HAL does not install. The standalone
  way prints no login for `dba-user` at all. The UI at `boundary.localhost:9200`
  needs no CLI.
- Boundary object names are the identity. Setup is written to reuse an object that
  already has the expected name, and `disable` finds objects by name to delete them.
  HAL does not record which objects it created, so an organization you made yourself
  under the name `hal-academy` would be treated as HAL's. Keep those names for the lab.
- The library does not set a credential type, and Boundary's default is
  `unspecified`. How the `boundary connect mysql` helper uses such a credential is not
  something this lab checks.

## Commands

- `hal boundary mariadb` with no action is read-only, and `status` does the same. It
  reports only whether the container `hal-boundary-target-mariadb` exists, so in
  `--with-vault` mode it says not deployed even when the target works.
  `hal boundary status` shows the standalone target next to the rest of Boundary.
- `enable` checks the guards above, starts the standalone container unless
  `--with-vault` is set, and builds the Boundary objects. `--boundary-mariadb-image`
  and `--boundary-mariadb-tag` change the standalone image, which is `mariadb:11.8`
  by default. The two flags do not apply to Vault's database.
- `update` removes what `disable` removes, then runs `enable` again.
- `disable` deletes, by name, the credential library and store, the role, the target,
  the host set, host and catalog, the auth method and both scopes from Boundary. It
  removes the container `hal-boundary-target-mariadb` and nothing else. It leaves
  every Vault object in place: the policy `boundary-controller` stays. Boundary's own
  documentation says it revokes the store's token when the store is deleted, and HAL
  does not check that it did.
