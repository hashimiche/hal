summary: Vault in this lab, a local dev or prod server with one scenario per auth method or secrets engine.
read: hal vault status | hal creds status
actions: hal vault create | hal vault update
docs:
- What is Vault: https://developer.hashicorp.com/vault/docs/about-vault/what-is-vault#why-should-i-use-vault
- Dev server mode: https://developer.hashicorp.com/vault/docs/concepts/dev-server#properties
- Auth methods: https://developer.hashicorp.com/vault/docs/auth#enabling-disabling-auth-methods

# Vault in this lab

HAL runs one Vault container, `hal-vault`, on the `hal-net` Docker network.
`hal vault create` starts it, `hal vault update` replaces its container and volumes
to reconcile it with the flags you pass, and `hal vault delete` removes it together
with everything attached to it. Vault is the
base the other `hal vault` commands build on: each of them adds one scenario to
this server.

## Modes and editions

- **Dev mode is the default.** Vault runs with `-dev`: in memory, already
  initialized and unsealed, plain HTTP, and a well-known root token. Everything is
  gone when the container goes. It is a classroom server, not a model of a
  production deployment.
- **Prod mode** is `hal vault create --mode prod`. It runs a single node with
  integrated Raft storage on a volume, TLS with a self-signed certificate HAL
  generates, and HAL initializes and unseals it. It is Vault Enterprise only, so
  `--mode prod` implies `--edition ent`.
- **Edition** is `--edition ce` (default), `ent`, or `ent-hsm` (Enterprise with a
  SoftHSM2 runtime, prod mode only). Enterprise needs a product license, which HAL
  takes from the environment (`VAULT_LICENSE` or `VAULT_LICENSE_PATH`) or asks for
  on a terminal. It is never stored by HAL and never printed.
- The image and the tag can be overridden with flags. `hal vault create --help`
  lists the defaults.

## Reaching it

The API answers at `vault.localhost:8200`: over HTTP in dev mode, over HTTPS in
prod mode. Other containers on the lab network reach it as `hal-vault`.

The root token is a lab secret and is not written in topics. In dev mode it is the
fixed, well-known one. In prod mode it is generated at init, and HAL keeps the init
output in a private file under `~/.hal`. `hal creds status` shows the current one.
When it answers that no lab service is active, the next step is to start Vault.

## Scenarios

A scenario is a feature enabled on Vault: `hal vault <scenario> enable`, then
`update` or `disable`. `hal vault --help` lists them. Running a scenario command
with no action only shows its status. `hal vault obs` is the exception: it follows
`create`, `update` and `delete`.

Some scenarios bring their own companion containers, and some share one with other
labs. GitLab and Authentik are shared services: a scenario registers as a consumer,
and the container stops only when no other consumer needs it any more (GitLab also
stays up while a Terraform Enterprise runtime runs).

A scenario that has its own topic is named `vault/<scenario>`. GitLab CI logging in
with a JWT is `vault/jwt`, and this page does not repeat it.

## Checking and removing

- `hal vault status` is the deep check. It lists the containers, the API health
  (version, initialized, sealed), the active auth methods, secrets engines and
  audit devices, and the credentials of what is running.
- `hal vault delete` removes the Vault container and the companions that belong to
  it (OpenLDAP and its admin UI, MariaDB, the Oracle database, the Agentic IAM
  containers), their volumes, the KinD cluster the Kubernetes scenarios share, and
  the saved prod state. Shared services that another product still uses are left
  running. The lesson does not offer this command as an action, because it
  destroys the whole Vault lab: the learner types it in a terminal.
