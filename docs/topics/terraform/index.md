summary: Terraform Enterprise in this lab, a local Docker runtime with one scenario per feature and a second instance called the twin.
read: hal terraform status | hal terraform logs | hal creds status
actions: hal terraform create | hal terraform update
docs:
- Terraform Enterprise on Docker, external mode: https://developer.hashicorp.com/terraform/enterprise/deploy/docker#example-external-mode-configuration
- Operational modes: https://developer.hashicorp.com/terraform/enterprise/deploy/configuration/storage#operational-modes
- HCP Terraform Agents on Terraform Enterprise: https://developer.hashicorp.com/terraform/cloud-docs/agents#terraform-enterprise

# Terraform Enterprise in this lab

HAL runs Terraform Enterprise (TFE) as a local runtime on the `hal-net` Docker
network. `hal terraform` (aliases `tf` and `tfe`) manages it. `hal terraform create`
stands it up, `hal terraform update` reconciles it to the flags you pass, and
`hal terraform delete` removes it. Every other `hal terraform` command adds one
scenario on top of it.

This is the self-hosted product only. The lab has no HCP Terraform mode, so every
topic under `terraform/` is about the local TFE runtime. HCP Terraform appears only
in the vendor documentation, where the pages for agents and VCS are shared between
the two products.

## Modes and editions

- **External operational mode, on one node.** The core container `hal-tfe` runs in
  `external` mode: PostgreSQL and object storage live outside it, in sibling
  containers. The vendor table lists Redis as local to the instance in this mode.
  HAL starts a separate Redis container anyway and points TFE at it, with no TLS and
  no authentication. That is a lab choice, so do not read it as the vendor's layout.
- **A license is required.** HAL reads it from the environment of the terminal that
  runs the command, `TFE_LICENSE` or `TFE_LICENSE_PATH`, and stops with a message if
  neither is set. The same license signs HAL in to HashiCorp's image registry so it
  can pull the image.
- **The image and tag can be overridden.** `hal terraform create --help` lists the
  defaults, among them the image tag and the tags of the backend containers.
- **A capacity check comes first.** Before it boots anything, HAL estimates whether
  the container engine has room, and asks a yes or no question on the terminal when
  it looks too small.

## What create stands up

- **A shared backend**, one set for every TFE instance: `hal-tfe-db` (PostgreSQL),
  `hal-tfe-redis`, `hal-tfe-s3` (an S3-compatible object store, published on host
  port 19000) and `hal-tfe-proxy`.
- **The core, `hal-tfe`.** It runs privileged with the Docker socket mounted, and
  `hal-net` is set as the network for the run containers it spawns. So the
  containers that execute plans and applies sit on `hal-net` too.
- **One ingress proxy and one certificate.** `hal-tfe-proxy` is an nginx with one
  virtual host per TFE instance, at a fixed address on `hal-net`. A single
  self-signed certificate under `~/.hal/tfe-certs` covers the hostnames of every
  instance. The browser warns about it, and containers that talk to TFE have to be
  given the certificate to trust.
- **A foundation.** An organization `hal` with a project `Dave`, a first admin user
  created through TFE's initial-admin flow, and an API token that HAL mints and
  caches under `~/.hal`.

`hal terraform update` removes the core, proxy, database, Redis and object-store
containers and the certificate files, then creates them again. The named volumes are
not touched.

## Reaching it

The primary answers at `https://tfe.localhost:8443`. The twin answers at
`https://tfe-bis.localhost:9443`. TFE's own hostname setting is portless on purpose,
and the proxy handles the ports. A few links that TFE builds from that hostname come
out without the port, which the VCS topic explains.

The admin login and the API token are lab secrets and are not written in topics.
`hal terraform status` and `hal creds status` print them, under Terraform
Enterprise for the primary and under the twin's own section for the twin.

## Scenarios

A scenario is a feature of TFE: `hal terraform <feature> enable`, then `update` or
`disable`. `hal terraform --help` lists them. A feature command with no action shows
its status, with two exceptions that are not read-only. Bare `saml` may mint and
cache an API token and create the organization in order to read its state. Bare
`api-workflow` starts a throwaway container to read tool versions.

The `--target` flag picks `primary` (the default), `twin` or `both`. `saml` and
`api-workflow` refuse `both`.

- `terraform/saml`: single sign-on through Authentik, with optional SCIM.
- `terraform/vcs-workflow`: a workspace linked to a GitLab repository.
- `terraform/api-workflow`: a helper shell for CLI-driven and API-driven runs.
- `terraform/agent`: a `tfc-agent` container in an agent pool.
- `terraform/twin`: a second TFE instance.

The twin and the agent are easy to mix up with each other and with the main
runtime. The primary runtime `hal-tfe` is TFE itself, with its UI, API and run
containers. An agent is a small `hashicorp/tfc-agent` worker that runs a workspace's
plans and applies in place of those run containers. It is not a second TFE. The twin
is a second whole TFE with its own organization, database and bucket. It is not a
worker and not a copy of the primary's data.

GitLab and Authentik are shared services. A scenario registers as a consumer, and
the container stops only when no other consumer needs it any more. GitLab also stays
up while a TFE runtime runs, and Authentik is shared with `hal vault oidc`. Deleting
a TFE instance, and disabling the VCS scenario, also drop the consumer records of
TFE runtimes that are no longer running.

## Checking and removing

- `hal terraform status` is the deep check. It reports each target's containers and
  health, the feature rows and the credentials of what is running. The feature row
  for VCS only says that `hal-gitlab` is running, whichever target you asked about.
- `hal terraform logs` prints the most recent lines of one component, read-only and
  bounded to 1000 lines. `--component` picks `tfe`, `tfe-db`, `tfe-redis`,
  `tfe-s3`, `tfe-proxy`, `tfe-agent`, `tfe-api`, `tfe-bis`, `tfe-bis-agent` or
  `tfe-bis-api`.
- `hal terraform delete` removes the primary core, its helper and agent, and, unless
  the twin still runs, the shared backend with its volumes, the certificate and the
  cached token. With the twin still running it keeps the backend and the certificate
  and only removes the primary's virtual host. `--target twin` removes only the
  twin core and its virtual host, and leaves the twin's database and bucket in the
  shared backend. The lesson does not offer this command as an action, because it
  destroys the lab: the learner types it in a terminal.
- **What HAL does not delete.** Beyond its own `hal-` containers, `delete` also
  removes a container whose name or image says `tfe-agent` or `tfc-agent`, but only
  when it is attached to `hal-net`. An agent a learner runs for their own purposes,
  on another network, is left alone. In the code read for this lesson, the twin's
  agent and helper (`hal-tfe-bis-agent`, `hal-tfe-bis-api`) are in no `delete` list,
  and the rule above skips names that start with `hal-`. So after deleting the twin,
  check for them and disable the twin's agent first with
  `hal terraform agent disable --target twin`. `hal teardown` removes every `hal-`
  container.
