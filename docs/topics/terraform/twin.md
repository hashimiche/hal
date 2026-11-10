summary: A second whole Terraform Enterprise instance beside the primary, sharing its database server, object store, proxy and certificate.
read: hal terraform status --target twin | hal creds status
actions: hal terraform create --target twin | hal terraform update --target twin
docs:
- Terraform Enterprise on Docker, external mode: https://developer.hashicorp.com/terraform/enterprise/deploy/docker#example-external-mode-configuration
- Data storage systems: https://developer.hashicorp.com/terraform/enterprise/deploy/configuration/storage#data-storage-systems
- Operational modes: https://developer.hashicorp.com/terraform/enterprise/deploy/configuration/storage#operational-modes

# The Terraform Enterprise twin

This topic is about the local Terraform Enterprise runtime. The lab has no HCP
Terraform mode. The twin is a second instance of the same self-hosted product.

## What it is, and what it is not

The twin is the one most easily confused with the others, so start there.

- **It is not the primary.** The primary is `hal-tfe` at `tfe.localhost`. The twin is
  `hal-tfe-bis` at `tfe-bis.localhost`.
- **It is not an agent.** An agent is a small worker that runs plans and applies for
  an instance. The twin is a whole instance, with its own UI, API, users,
  organizations and run containers. See the agent topic.
- **It is not a copy of the primary's data.** Nothing is replicated. The twin starts
  empty, with its own organization and project.

What it gives you is a second, independent instance in the same lab, with its own
organization, users and run containers. The code does not say what it is for, so a
lesson should not invent a use beyond that.

## The mechanism

A Terraform Enterprise instance in external mode keeps its state outside the
container: a PostgreSQL database and an S3-compatible object store, plus Redis. The
vendor's storage page describes what an instance expects of those services. This lab
runs one server of each kind and gives every instance its own data inside it, a
database and a bucket, instead of a second server.

## How this lab stages it

- **Separate data, shared servers.** The twin uses the same `hal-tfe-db`,
  `hal-tfe-redis` and `hal-tfe-s3` containers as the primary. HAL creates a database
  `tfe_bis` in the shared PostgreSQL and a bucket `tfe-bis-data` in the shared
  object store. The twin has its own core container and its own cache volume.
- **One proxy, one certificate.** There is no second proxy. The twin adds a virtual
  host to `hal-tfe-proxy`, which publishes port 9443 for it. The primary stays on
  8443. The single certificate under `~/.hal/tfe-certs` already lists the twin's
  hostname, and it is rotated if it does not. An old per-target proxy from before the
  sharing, `hal-tfe-bis-proxy`, is treated as legacy and is removed only when a
  primary delete also removes the shared backend.
- **The same image and settings as the primary.** It runs the same Terraform
  Enterprise image and tag, privileged, with the Docker socket mounted, in external
  mode, with `hal-net` as the network of its run containers. Its hostname is
  portless, like the primary's.
- **Its own foundation.** The organization is `hal-bis` and the project is
  `Dave-bis`, with a first admin user and an API token minted for it. The primary's
  names are `hal` and `Dave`.
- **It is a target, not a subcommand.** The twin is reached with `--target twin` on
  `hal terraform create`, `update`, `delete` and `status`, and on the feature
  commands. The target flag takes `primary`, `twin` or `both`. There is no
  `hal terraform twin` command and the target value `bis` is not accepted, whatever
  older notes say. With `--target both`, `create` builds the primary and then the
  twin.
- **The primary must be up first.** The twin command stops with a message when
  `hal-tfe` is not running, or when the database, Redis, object store or proxy
  containers are missing, and asks you to run `hal terraform create` first. It needs
  the same license in the environment.

## What differs from the textbook

- **The sharing is a lab shortcut.** A production deployment would give each
  instance its own database server and object store. Here the sharing ties the twin
  to the primary's backend: removing the backend would remove the twin's data with
  it. That is why `hal terraform delete` on the primary keeps the backend, the
  certificate and the proxy while a twin still runs, and only removes the primary's
  virtual host.
- **The twin has its own admin password.** It has its own admin credentials and
  its own flag. They are lab secrets: see `hal terraform status --target twin` and
  `hal creds status`, where the twin has its own section. The API token cache is
  shared, so there is one token.
- **Two features refuse `both`.** `saml` and `api-workflow` take `primary` or `twin`
  but not `both`. With `--target twin`, the other features switch to the twin's names:
  the organization `hal-bis`, the project `Dave-bis` and, for VCS, the workspace
  `tfe-agent-demo-bis`. `saml` is the exception: it keeps `hal` as its default
  organization unless told otherwise.
- **Flag and port names are easy to get wrong.** The flags are `--twin-https-port`
  and `--twin-tfe-org`, not `--twin-port` and `--twin-org`, and `create` has no
  `--twin` flag. 8444 is the primary's internal admin port. The twin is on 9443.

## What to expect

- The first boot is slow. The command waits for the twin to become healthy and says
  that it can take several minutes.
- `update --target twin` removes the twin core and its virtual host, then creates
  them again. It keeps the shared servers, and the twin's database and bucket.
- `hal terraform delete --target twin` removes only the twin core and its virtual
  host. The twin's database and bucket stay in the shared backend. Its agent and its
  API helper containers are not on that list, so run
  `hal terraform agent disable --target twin` first, and
  `hal terraform api-workflow disable --target twin` if you used the helper.
- After deleting the twin, HAL drops the consumer records of any runtimes that are
  no longer running, and stops GitLab or Authentik if nothing else needs them.

## Commands

- `hal terraform status --target twin` is read-only. It lists the twin's
  containers and shows the credentials it prints for it.
- `create --target twin` builds the twin. `update --target twin` rebuilds it.
- The remaining twin flags (port, hostname, database, bucket, container name, image
  and tag) are listed by `hal terraform create --help`. They share the `--twin-`
  prefix.

The twin's admin login is a lab secret, so it is not written in this file. It is
the Admin entry in the twin's own section of the credentials that
`hal terraform status --target twin` prints, and `hal creds status` shows the same
tree.
