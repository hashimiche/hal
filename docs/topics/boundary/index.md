summary: Boundary in this lab, a dev-mode controller and worker with a Postgres backend, plus two demo targets added by their own commands.
read: hal boundary status | hal boundary logs | hal boundary obs status | hal creds status
actions: hal boundary create | hal boundary update
docs:
- What is Boundary: https://developer.hashicorp.com/boundary/docs/what-is-boundary#how-does-boundary-work
- Dev mode: https://developer.hashicorp.com/boundary/docs/commands/dev#command-options
- Boundary metrics: https://developer.hashicorp.com/boundary/docs/monitor/metrics#example-configuration

# Boundary in this lab

HAL runs Boundary as two containers on the `hal-net` Docker network: `hal-boundary`,
which holds the controller and a worker in one process, and `hal-boundary-backend`,
a PostgreSQL database. `hal boundary create` starts them, `hal boundary update`
replaces both, and `hal boundary delete` removes them together with the demo targets.

Boundary grants access to a target through a session. A person authenticates, a role
authorizes the session, and the worker proxies the connection, so the person needs a
route to Boundary and not to the target. A controller on its own has nothing to
connect to: each target command below adds one.

## Modes and editions

- **Dev mode is the only mode.** The container runs `boundary dev`, pointed at the
  backend container for its database. Dev mode comes with a ready-made admin account
  and is a classroom setup, not a model of a production deployment.
- **HAL mounts no volume for the backend.** What Boundary knows (scopes, targets,
  accounts, roles) lives with the backend container, and a replaced container starts
  empty.
- **The images are the stock ones by default.** `--boundary-image` and
  `--boundary-tag` change the controller, and `--boundary-pg-image` and
  `--boundary-pg-tag` change the database. HAL has no edition switch.
  `hal boundary create --help` lists the defaults.

## Reaching it

The UI and the API answer at `boundary.localhost:9200`. The cluster port 9201 and the
proxy port 9202 are published as well. Other containers on the lab network reach the
controller as `hal-boundary`.

The admin login is a lab secret and is not written in topics. `hal creds status` has
a Boundary entry while `hal-boundary` runs. It also lists the demo users of the SSH
target when the VM exists, and the database account of the standalone MariaDB target
when that container exists. HAL's own commands log in to the API on `127.0.0.1:9200`
as that admin through the password auth method, so the target commands need the
controller to be up.

Dev mode also prints its generated admin login at startup, so the start of the
controller's log carries it. `hal creds status` is the place to point a learner to.

A terminal session (`boundary connect`) needs the Boundary CLI on your machine. HAL
does not install it. The web UI works without it.

## Scenarios

A scenario is a demo target: `hal boundary <target> enable`, then `update` or
`disable`. With no action the command only shows its status.

- `hal boundary ssh` puts a Multipass VM behind Boundary. See `boundary/ssh`.
- `hal boundary mariadb` puts a MariaDB database behind Boundary, optionally with
  credentials brokered by Vault. See `boundary/mariadb`.

Each target gets its own organization scope, so the two scenarios do not share any
object that HAL creates in Boundary.

## Consul

`hal boundary create --join-consul` only requires that `hal-consul` is running, and
sets `CONSUL_HTTP_ADDR` to `http://hal-consul:8500` in the container. Nothing else
changes. Boundary does not join a Consul cluster, and HAL configures no service
registration or storage with it. The "tethered" line that `create` prints reports
that check, not a cluster membership. See the Consul topic.

## Observability

`hal boundary obs create` registers `hal-boundary:9200` as the Prometheus target
named `boundary`, once Boundary and the obs stack run (see the obs topic). Unlike
Vault, Consul and Nomad, no Grafana dashboard is downloaded for Boundary. Treat the
metrics as unconfirmed: HAL scrapes the API port at `/v1/metrics`, while the
Boundary documentation puts metrics at `/metrics` on a separate "ops" listener, port
9203 by default, and HAL configures and publishes neither. Check the Prometheus
targets page before relying on them.

## Checking and removing

- `hal boundary status` is the deep check, and the bare `hal boundary` runs it too.
  It reports the backend, the controller and worker, the MariaDB target and the SSH
  VM. `hal boundary obs status` reports the observability artifacts.
- `hal boundary logs` reads the most recent lines of a container and never follows.
  `--component` picks `boundary` (default), `boundary-backend` or
  `boundary-target-mariadb`. The SSH VM is not a log source.
- `hal boundary update` removes the controller and the backend and starts both again
  from the flags you pass. The demo targets are not touched, but their Boundary
  objects were in the removed backend, so run the target's own `update` to wire it
  again.
- `hal boundary delete` removes the controller, the backend and the standalone
  MariaDB target, deletes the Multipass VM `hal-boundary-ssh`, removes the network
  when nothing else uses it, and removes the Boundary Prometheus target file. It does
  not touch Vault or the MariaDB container that the Vault labs own. The lesson does
  not offer this command as an action: the learner types it in a terminal.
