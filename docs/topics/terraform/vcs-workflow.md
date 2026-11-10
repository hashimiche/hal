summary: A Terraform Enterprise workspace linked to a GitLab repository, so a push to main starts a run.
read: hal terraform vcs-workflow | hal terraform status | hal creds status
actions: hal terraform vcs-workflow enable | hal terraform vcs-workflow update | hal terraform vcs-workflow disable
docs:
- VCS webhooks: https://developer.hashicorp.com/terraform/cloud-docs/vcs#webhooks
- GitLab EE and CE VCS provider: https://developer.hashicorp.com/terraform/cloud-docs/vcs/gitlab-eece#set-up-the-gitlab-ee-and-ce-vcs-provider

# Terraform Enterprise VCS-driven workflow

This topic is about the local Terraform Enterprise runtime, reached with
`hal terraform vcs-workflow`. The lab has no HCP Terraform mode. The vendor pages
linked above are shared between the two products.

## The mechanism

In the VCS-driven workflow a workspace is tied to a branch of a repository, and the
repository, not a person, decides when a run starts. Terraform Enterprise holds a
connection to the VCS provider, called a VCS provider or OAuth client, which gives
it a credential to read the repository. When a workspace is linked, Terraform
Enterprise registers a webhook on the repository. A new commit on the tracked
branch makes the VCS send an event to Terraform Enterprise, which queues a plan.
With auto-apply on, the apply follows without anyone approving it. A merge request
gets a speculative plan, which shows the result but can never be applied.

Two network paths have to work, in opposite directions. Terraform Enterprise must
reach the VCS to read the repository and create the webhook. The VCS must reach
Terraform Enterprise to deliver the event. With a self-hosted VCS on a private
network, the second one is the one that usually fails.

## How this lab stages it

- **GitLab CE is the VCS.** `hal terraform vcs-workflow enable` starts the shared
  GitLab container `hal-gitlab`, or reuses the one that another scenario started.
  The default GitLab port is 8080. When GitLab is already running, HAL adopts the
  port it runs on instead of its own flag, so all the URLs point at the same
  instance. HAL itself talks to GitLab on `127.0.0.1`. Terraform Enterprise talks to
  it on the `gitlab.localhost` alias on `hal-net`.
- **GitLab is a shared service.** The scenario registers one consumer per target,
  and GitLab stops only when no consumer is left and no Terraform Enterprise runtime
  runs. Stopping it also clears the recorded root password. Vault's JWT scenario
  shares the same GitLab.
- **One demo project, not several.** The project is `root/tfe-agent-demo` (public,
  default branch `main`). HAL commits two files to it: a `main.tf` with a single
  `null_resource`, and a `.gitlab-ci.yml` with one job that runs `terraform init`
  without a backend, `terraform fmt -check` and `terraform validate` in a
  `hashicorp/terraform` image. That pipeline is plain GitLab CI, not what Terraform
  Enterprise runs. This scenario registers no GitLab Runner, so do not expect the job
  to execute unless a runner is online. The Terraform Enterprise run does not need it.
- **The connection is an OAuth client named `hal-gitlab`,** created with the GitLab
  Community Edition service provider, falling back to the generic GitLab one. If
  attaching it with an OAuth access token fails, HAL creates a personal access token
  in GitLab and retries with that.
- **The workspace is `tfe-agent-demo`,** in organization `hal` and project `Dave`.
  It is created with auto-apply on, remote execution, and `queue-all-runs` on, so a
  run started by the VCS is queued at once without a manual first run. It tracks
  `main`. For the twin the names are `tfe-agent-demo-bis`,
  `hal-bis` and `Dave-bis`, and the repository is `root/tfe-agent-demo-bis`.
- **The webhook path is prepared for you.** HAL changes a GitLab application
  setting so webhooks may call local addresses, and it edits `/etc/hosts` inside
  `hal-gitlab` so `tfe.localhost` resolves to the TFE proxy on `hal-net`. That
  covers the direction a self-hosted VCS usually breaks.

## What differs from the textbook

- **A run link that does not open.** Terraform Enterprise posts a commit status to
  GitLab with a link back to the run. The link is built from the portless hostname
  `tfe.localhost`, so it comes out without `:8443` and cannot be opened from the
  host. This is a known limit of the lab, not a failure. The workspace page itself
  opens at the normal address.
- **The hosts edit covers one hostname.** It maps only `tfe.localhost`. A twin
  workspace uses `tfe-bis.localhost`. If pushes to the twin repository do not start
  runs, check whether GitLab can resolve that name.
- **The workspace may exist without the link.** When the OAuth token id is not
  available, HAL still creates the workspace but with no VCS repository. Running
  `enable` again, once the token exists, completes the link. When a webhook cannot
  be created, HAL rotates the OAuth token and retries once.
- **Most flags are hidden.** `--help` shows only the target, the GitLab image, tag
  and port, and `--auto-approve`. The branch, the tag regex, the organization and
  the GitLab root password have hidden flags. The workspace follows `main`, and a
  tag trigger is off unless a tag regex is given.
- **The pool is not involved.** Runs use the instance's own run containers, with
  remote execution. An agent only matters if you switch the workspace to agent
  execution yourself, which is the agent topic.

## What to expect

- After `enable`, the command prints the repository address and the next step:
  push a commit to `main`. The push is what starts the first run, and with
  auto-apply it should apply without approval.
- `hal terraform vcs-workflow --target both` configures the primary and then the
  twin, one after the other.
- If the workspace shows no VCS connection, the OAuth client or the token step
  failed. The command prints a warning for it.

## Commands

- `hal terraform vcs-workflow` with no action is read-only. It reports whether the
  Terraform Enterprise core is running and whether `hal-gitlab` runs, then names the
  next step.
- `enable` needs the Terraform Enterprise core to be running for that target. It
  boots or reuses GitLab, seeds the project, creates the OAuth client and the
  workspace, and registers the consumer.
- `update` runs `enable` again, which re-applies the seeded files and the workspace
  settings.
- `disable` asks for confirmation on a terminal unless `--auto-approve` is given. It
  only removes this target's consumer record, prunes stale ones, and stops GitLab
  when nobody uses it. It does not delete the workspace, the OAuth client or the
  GitLab project.

The GitLab web UI needs a login. It is a lab secret, so it is not written in this
file: it is the GitLab entry under Terraform Enterprise in the credentials that
`hal terraform status` prints, and `hal creds status` shows the same tree.
