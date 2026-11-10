summary: Vault mints short-lived database users on demand, MariaDB by default and Oracle Free with an Enterprise plugin.
read: hal vault database | hal vault status | hal creds status
actions: hal vault database enable | hal vault database enable --k8s | hal vault database update | hal vault database disable | hal vault database disable --k8s
docs:
- Database secrets engine capabilities: https://developer.hashicorp.com/vault/docs/secrets/databases#database-capabilities
- Database secrets engine usage: https://developer.hashicorp.com/vault/docs/secrets/databases#usage
- Vault Secrets Operator, VaultDynamicSecret: https://developer.hashicorp.com/vault/docs/deploy/kubernetes/vso/sources/vault#vaultdynamicsecret-custom-resource

# Vault dynamic database credentials

## The mechanism

An application that talks to a database needs a database user. The usual way is a
long-lived account whose password sits in a configuration file and is shared by
everyone who has to connect. Dynamic credentials turn that around. Nobody holds a
standing database password. When a client asks Vault for a credential, Vault
creates a new database user on the spot, with a name and password only that
request knows. The user is valid for a lease, and when the lease ends Vault runs
the revocation statements that drop it.

For Vault to create users, it needs a connection account of its own on the
database. This lesson calls it the broker. Vault rotates the broker's password as
soon as it is configured, so only Vault knows it. The Vault docs recommend a user
created for this purpose alone.

A role links the two halves. It holds the statements that create a user, the
statements that revoke it, and the lease lengths. A client reads the role's
credential path and gets a user back.

## How this lab stages it

This is one Vault engine, mounted at `database/`, with one role for each backend.
The backend is a value of the `--backend` flag (`mariadb` by default, or
`oracle`), not a separate command. The command also answers to the short name
`hal vault db`. The `pgsql` backend is refused with a message that it is not
implemented yet.

- **MariaDB is a shared container.** It is `hal-vault-mariadb`, on the `hal-net`
  network under the name `mariadb.localhost`, port 3306. The Agentic IAM lab uses
  the same container, each lab with its own Vault mount and its own broker. HAL
  counts the labs that use it and keeps the container while any is left.
- **The MariaDB broker is `vaultadmin`.** HAL creates it with all privileges on
  every database and the right to grant them (`WITH GRANT OPTION`), and then Vault
  rotates its password. The role `dba-role` creates users with all privileges on
  every database. So the users Vault mints are administrators of that container.
  Their names are the prefix `v` (change it with `--username-prefix`), a hyphen and
  ten random characters. HAL sets that name template for MariaDB only.
- **Oracle is a separate container**, `hal-vault-oracle-db`, from
  `gvenzl/oracle-free:slim`, with the pluggable database `FREEPDB1` on port 1521
  under the name `oracle.localhost`. It needs Vault Enterprise, and it needs
  `--oracle-plugin-path`, the path to the Oracle plugin binary that the user
  supplies. `enable` stops if either is missing. HashiCorp publishes that binary
  for linux/amd64 only, so on an arm64 machine the user builds it, and the lab's
  notes in `docs/vault-oracle-plugin-build.md` cover how. HAL then builds a local
  runtime image on Debian with the Oracle client libraries, copies the plugin into
  Vault's plugins volume, registers it by its checksum, and replaces the Vault
  container with one running that image. The broker, named `vault`, gets its
  grants through SQL run as the Oracle system user. The role `oracle-dba-role`
  creates users that can connect and create a session.
- **The lease lengths differ by backend.** The MariaDB role has a default lease of
  two minutes and a maximum of two hours. The Oracle role has one hour and 24 hours.
- **With `--k8s` the lease is 15 seconds**, for both backends, and the maximum is
  the same, so a lease cannot be renewed. VSO has to ask for a new user every cycle.
- **`--k8s` also stages a demo app.** HAL creates a dedicated Kubernetes auth mount,
  `kubernetes-db`, with the role `db-app-role` and the policy `db-app-read`, which
  reads only the database role's credential path. It installs the Vault Secrets
  Operator and applies a `VaultDynamicSecret` named `hal-db-dynamic`, which writes
  the credential into the Kubernetes Secret `hal-db-creds` and restarts the demo
  deployment when it changes. The namespace is `db-app`, and the page is at
  `http://db.localhost:8091`. HAL has a single KinD cluster, named `kind`, which
  the Kubernetes scenarios share, and this demo is one tenant of it.

## What to expect

- **The enable output prints a user and a password.** They are the credential Vault
  generated for the first test, and they are short-lived. This lesson does not
  copy them.
- **Two sentences of the MariaDB enable output do not match the code.** It calls
  the broker "least-privileged", but the grants above are the widest there are. It
  also says the user is gone after an hour, but the MariaDB role's default lease is
  two minutes. Trust the role, not the sentence.
- **`disable` and `update` need `--k8s` too, to clean the Kubernetes side.** The
  flag is not remembered. Without it, they remove the engine and the backend but
  leave the Vault mount `kubernetes-db`, its policy and the `db-app` namespace
  where they are.
- **Disable does not always remove MariaDB.** While another lab still uses the
  shared container, the container stays and only this lab's broker and mount go.
- **Oracle takes a while to start.** HAL's own message says 60 to 120 seconds. The
  wait loop polls up to 120 times, five seconds apart, before it gives up.
- **Oracle replaces the Vault container, whatever mode it was in.** The new
  container is started in dev mode, with the fixed dev root token, and dev mode
  keeps its state in memory. So anything configured in Vault before is gone, and
  HAL does not put it back. `disable` does not return Vault to the official image
  either. This follows from the code and was not run against the other scenarios.
- **The `--k8s` manifests point VSO at Vault over plain `http`.** That fits dev
  mode, which is where this scenario is meant to run.

## Commands

- `hal vault database` with no action is read-only. It shows the mount and both
  backends: whether their containers run and whether Vault is configured for them,
  the Oracle runtime image and the plugin, and the next step. `hal vault status`
  lists the mounted secrets engines, and `database/` appears there when it is
  enabled.
- `enable` starts the backend, creates the broker, mounts the engine, writes the
  role and generates one test credential. `enable --k8s` goes on to build the demo
  in Kubernetes. `kind`, `kubectl` and `helm` must be installed for that.
- `update` runs the disable steps and then `enable` again. Use it when the status
  shows the environment is partly degraded.
- `disable` revokes every lease of the engine, unmounts `database/`, and then
  removes the MariaDB broker and releases the shared container, or removes the
  Oracle container and its plugin entry. With `--k8s` it also removes the demo's
  Vault mount and policy, and then deletes the cluster only if no other Kubernetes
  scenario uses it and only a cluster that HAL created. Otherwise it keeps the
  cluster and removes just the `db-app` namespace.

The Oracle backend is not an action here, because enabling it needs a binary the
user has to bring. The database logins are lab secrets and are not written in this
file. For the credentials of what is running, `hal creds status` shows them.
