summary: A helper shell with terraform and tfx, logged in to the local Terraform Enterprise, for CLI-driven and API-driven runs.
read: hal terraform status | hal creds status
actions: hal terraform api-workflow enable | hal terraform api-workflow update
docs:
- The CLI-driven remote run workflow: https://developer.hashicorp.com/terraform/cloud-docs/workspaces/run/cli#the-cli-driven-remote-run-workflow
- The API-driven run workflow: https://developer.hashicorp.com/terraform/cloud-docs/run/api#the-api-driven-run-workflow
- API authentication: https://developer.hashicorp.com/terraform/cloud-docs/api-docs#authentication

# Terraform Enterprise API and CLI workflow

This topic is about the local Terraform Enterprise runtime, reached with
`hal terraform api-workflow` (alias `api`). The lab has no HCP Terraform mode. The
vendor pages linked above describe the two products together.

The command is named for the API, but what it gives you is a shell where both the
Terraform CLI and the API are ready to use. There is no separate skill or scenario
for the CLI workflow: this one covers it.

## The mechanism

Terraform Enterprise has three ways to start a run, and this one is the other two
combined. In the VCS-driven workflow a commit starts the run. In the CLI-driven
workflow a person runs `terraform plan` or `terraform apply` in a local directory
whose configuration has a `cloud` block. The CLI uploads the configuration, the run
executes remotely, and the output streams back to the terminal. In the API-driven
workflow some other tool makes the calls itself: it creates a workspace, uploads a
configuration version and creates a run.

All of them authenticate with a bearer token. A user token acts as one person, with
that person's rights. A team token and an organization token have their own scopes.
A token is a secret and is never written in a lesson.

## How this lab stages it

- **A helper container, built on first use.** HAL builds a local image
  (`hal-tfe-api:latest` for the primary, `hal-tfe-bis-api:latest` for the twin) on
  top of the `tfx` image, adds git and curl, and downloads the current Terraform CLI
  release at build time. So the first build needs internet access, and the Terraform
  version in the helper is whatever is current that day, not the version of the
  instance. The image runs as `hal-tfe-api` on `hal-net`, idles in the background,
  and the command opens a shell inside it.
- **It trusts the lab certificate.** The self-signed certificate is mounted into the
  container and added to its trust store. Without that, `terraform` and `tfx` would
  refuse to talk to the proxy.
- **Auth is already bootstrapped.** HAL creates a user token in Terraform Enterprise
  with the description `hal-tfe-cli-session`, then writes it into the container for
  both tools: a `tfx` config file and the Terraform CLI credentials file. So you do
  not run `terraform login`. The banner says to avoid it unless you want to rotate
  the token. Because those files hold a live token, treat the container as private.
- **Five scenario directories under `/workspaces`.** Each has a `main.tf` whose
  `cloud` block names the organization and a workspace of the same name, and which
  outputs a repository name, a theme and a joke. They are `hal-lucinated`,
  `hal-ogen`, `hal-lelujah`, `hal-oween` and `hal-ibut`. HAL creates the matching
  workspaces too, in two projects: `Dave` holds `hal-lucinated`, `hal-lelujah` and
  `hal-ibut`, and `Frank` holds `hal-ogen` and `hal-oween`.
- **Some runs are started for you.** In the background, HAL plans `hal-ogen` and
  `hal-oween`, and plans then applies `hal-lucinated` and `hal-lelujah`. `hal-ibut`
  is left untouched on purpose. So a fresh lab already shows runs in different
  states.
- **A local directory can be mounted.** A hidden `--local-directory` flag mounts a
  host folder at `/workspaces`, the same path where the scenario directories are
  written, so those directories would be created in that folder.

## What differs from the textbook

- **The CLI workflow is already wired.** In practice you would run `terraform login`
  and write the `cloud` block yourself. Here the credentials exist and the blocks
  are written. That is a shortcut for the lesson, not the usual setup.
- **A user token, not a team or organization token.** The token HAL creates belongs
  to the lab's admin user. It can do everything that user can, which is more than a
  CI system should have.
- **Runs execute on the instance, not on your machine.** The workspaces use remote
  execution, so a plan started from the helper runs in a run container on `hal-net`
  and streams back. Nothing is planned in the helper itself.
- **The helper is ephemeral.** Leaving the shell only exits the shell. The container
  and the image stay until `update` rebuilds them or `disable` removes them.

## What to expect

- `enable` ends with an interactive shell, so it needs a terminal. Leave it with
  CTRL+D or `exit`, and come back later with `enable` again. It reuses the image
  and the container.
- `update` removes the container and the image, builds them again, then does the
  same as `enable`, so it also ends in a shell.
- The twin has its own helper and its own organization and project names (`hal-bis`
  and `Dave-bis` as its foundation). `--target both` is refused.
- If the helper cannot start, the usual cause is a Terraform Enterprise core that is
  not running for that target.

## Commands

- `hal terraform api-workflow` with no action shows the status, but it is not purely
  read-only. When the helper image exists it starts a throwaway container to read the
  tool versions. `hal terraform status` is the plain read.
- `enable` needs the core to be running and the certificate to exist. It builds or
  reuses the image and the container, refreshes trust, creates the user token, writes
  the auth files, seeds the scenario directories and workspaces, starts the
  background runs and opens the shell.
- `disable` is destructive and is not offered as an action here. It deletes the
  scenario workspaces that HAL created in Terraform Enterprise, then the container
  and the image. Without a terminal it refuses unless `--auto-approve` is given.
  Type it yourself when you want to clean up.

The admin login and the API token of the instance are lab secrets, so they are not
written in this file. `hal terraform status` prints them, and `hal creds status`
shows the same tree.
