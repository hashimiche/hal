summary: A tfc-agent container in an agent pool of the local Terraform Enterprise, ready for a workspace you switch to Agent execution.
read: hal terraform agent | hal terraform status | hal creds status
actions: hal terraform agent enable | hal terraform agent update | hal terraform agent disable
docs:
- Create an agent pool: https://developer.hashicorp.com/terraform/cloud-docs/agents/agent-pools#create-an-agent-pool
- Configure workspaces to use the agent: https://developer.hashicorp.com/terraform/cloud-docs/agents/agent-pools#configure-workspaces-to-use-the-agent
- Agents on Terraform Enterprise: https://developer.hashicorp.com/terraform/cloud-docs/agents#terraform-enterprise

# Terraform Enterprise agents

This topic is about the local Terraform Enterprise runtime, reached with
`hal terraform agent`. The lab has no HCP Terraform mode. The agent image and its
`TFC_` settings carry the HCP Terraform name because the same agent works with both
products, and the vendor pages above are shared between them.

## What an agent is, in this lab

An agent is not a second Terraform Enterprise and not the twin. It is a small worker
that picks up the plan and apply steps of a run and executes them, in place of the
instance's own run containers. `hal-tfe` is Terraform Enterprise itself, with its UI
and API. `hal-tfe-bis` is a whole second instance, which is the twin topic. An agent
is just a worker that calls home.

## The mechanism

Terraform Enterprise groups agents in agent pools. A pool belongs to an organization.
Each pool issues agent tokens, and an agent that starts with one registers itself in
that pool and asks Terraform Enterprise for work. The agent needs only outbound
connectivity to its instance, so it can run where Terraform Enterprise cannot reach
it, for example in a private network that holds the systems a run has to touch.

A pool does nothing on its own. A workspace, or a project, has to be set to the
Agent execution mode and pointed at the pool. Only then do its runs go to an agent
instead of the instance's own run containers.

## How this lab stages it

- **A pool in the organization.** `hal terraform agent enable` creates an
  organization-scoped agent pool, or reuses one with the same name. The primary uses
  `hal-agent-pool` and the twin uses `hal-agent-pool-bis`.
- **A token for the agent.** HAL creates an agent token in that pool, described as
  `hal-managed-agent`, and gives it to the container. The token is a secret and is
  never written in a lesson. HAL keeps its id, not its value, in a state file under
  `~/.hal`, one for the primary and one for the twin.
- **A `hashicorp/tfc-agent` container.** It is named `hal-tfe-agent` (the twin's is
  `hal-tfe-bis-agent`), runs on `hal-net`, and is told the address of its instance,
  its display name (`hal-tfc-agent`, or `hal-tfc-agent-bis`) and that it should
  keep running. The agent image tag is a flag.
- **It trusts the lab certificate.** The self-signed certificate is mounted into the
  container, and the hostname of the instance is mapped to the shared TFE proxy on
  `hal-net`. So the agent reaches the instance through the same proxy the browser
  uses.
- **The core must run first.** `enable` stops with a message when the Terraform
  Enterprise core for that target is not running.

## What differs from the textbook

- **Nothing is assigned for you.** HAL creates the pool and starts the agent, but no
  code sets any workspace to the Agent mode. The command ends by telling you to open
  a workspace, switch its Execution Mode to Agent and pick the pool. Until you do,
  every run still uses the instance's own run containers, and the pool has an agent
  that is idle.
- **Check auto-apply before you switch.** The vendor page says to disable auto-apply
  on the workspace first, because changing the execution mode after a plan makes the
  apply fail. The workspaces that HAL creates for the VCS scenario have auto-apply on.
  So disable it, wait for pending runs, then change the mode.
- **Name resolution is a hosts entry.** The vendor page says an agent needs only
  outbound connectivity to its instance. Here that means it must resolve
  `tfe.localhost` (or `tfe-bis.localhost`) to the proxy. HAL adds that with a hosts
  entry on the container rather than with DNS.
- **Disable keeps the pool.** `disable` removes the container, revokes the token and
  deletes the state file, but leaves the pool in Terraform Enterprise so that a later
  `enable` can reuse it. A pool left behind is expected, not a leak.

## What to expect

- `enable` prints the pool and the image. The pool may list zero or more agents for
  a few moments while the container registers.
- A second `enable` with the container already running does nothing and says so.
  `update` removes the container first and starts a fresh one with a new token.
- `--target both` handles the primary and then the twin. Each has its own pool,
  container and state file.
- If the agent never registers, check the agent logs with
  `hal terraform logs --component tfe-agent` (or `tfe-bis-agent`), and check that the
  Terraform Enterprise core is running.

## Commands

- `hal terraform agent` with no action is read-only. It reports whether the core and
  the agent container run, and the pool and token ids from the state file.
- `enable` needs the core to be running. It gets an API token, creates or reuses the
  pool, creates the agent token, then starts the container and writes the state file.
- `update` is `enable` with the container removed first.
- `disable` asks for confirmation on a terminal unless `--auto-approve` is given.

**Cleanup.** The agent container is a `hal-` container and `hal terraform agent
disable` removes it. Separately, `hal terraform delete` and `hal teardown` also remove
agent containers whose name or image says `tfe-agent` or `tfc-agent`, but only when
they are attached to `hal-net`. HAL never deletes a container it did not create, so an
agent a learner runs elsewhere stays. The twin's agent container is not in the lists
`hal terraform delete` uses, so disable it first with `--target twin`.
