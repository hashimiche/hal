summary: A persona's chat agent acts on their behalf through a token exchange, and the identity provider and Vault each decide what it may read.
read: hal vault agentic-iam | hal vault status | hal creds status
actions: hal vault agentic-iam enable | hal vault agentic-iam update | hal vault agentic-iam disable
docs:
- Vault AI IAM, agent request flow: https://developer.hashicorp.com/vault/ai/iam#agent-request-flow-for-obo-delegation-with-rar
- Vault Agent Registry, authorization ceiling: https://developer.hashicorp.com/vault/ai/iam/concepts/agent-registry#authorization-ceiling
- Authentik token exchange, on behalf of a user: https://docs.goauthentik.io/add-secure-apps/providers/oauth2/token_exchange/#exchange-a-token-on-behalf-of-a-user-obo

# Vault Agentic IAM: an agent acting on behalf of a person

## The mechanism

An AI agent that reads company data for a person raises a plain question: whose
rights does it use? Giving it a standing account with broad rights is risky, because
a manipulated agent then has those rights. Giving it the person's own login hides
that an agent, not the person, made the request. This lab shows a third answer,
**delegation**, where the agent keeps its own identity and borrows the person's for
one task.

The pieces, in the order a request meets them:

- **The person logs in** to a chat through the identity provider (IdP) and gets an
  access token. The agent never sees their password.
- **Token exchange (RFC 8693).** The agent goes back to the IdP with two tokens: the
  person's access token (the *subject*) and its own, from its own login (the
  *actor*). The IdP checks that this person may delegate to this agent. If so, it
  returns an **on-behalf-of (OBO) token** whose `sub` claim is the person and whose
  `act.sub` claim is the agent. Both identities are in the one token.
- **Rich Authorization Requests (RFC 9396).** The OBO token carries
  `authorization_details` entries, one per access the person agreed to for this task.
  Each says "read this Vault path". It is the **task scope**: the agent cannot use the
  token for anything the person did not approve for this task, even if the person
  could.
- **Vault as the resource server.** The agent presents the OBO token to Vault as its
  credential. Vault validates it against a profile for the IdP (issuer, keys,
  audience), maps `sub` to the person's identity entity and `act.sub` to the agent's
  entity, and requires the `authorization_details` claim.
- **The Agent Registry** holds a record for the agent with a **ceiling**: a policy
  set that limits what the agent can do whenever it acts for someone. Vault checks
  two policy sets separately, the person's own and the agent's ceiling, and both must
  allow the read. The ceiling can only reduce, never add.

So four things decide each access: the IdP (may this person delegate to this agent
at all), the person's own rights in Vault, the agent's ceiling, and the task scope.
An access happens only if all four allow it.

## How this lab stages it

- **Two lab containers**, both from one image that HAL builds on your machine from
  sources embedded in the `hal` program, and keeps as a build cache. `hal-agentic-iam-chat`
  is the chat page and its backend, published on host port 8092 and reached at
  `http://agentic.localhost:8092`. `hal-agentic-iam-agent` is the demo agent, on the lab
  network only. Each reads its own settings file under `~/.hal/agentic-iam/`, written
  readable by the owner only, so that no secret shows on a command line. The chat
  never receives the agent's exchange credentials.
- **The demo agent is not a real model.** A keyword matcher stands in for the model so
  that the six cases below give the same result every time. It also always obeys an
  instruction planted in the data it reads, which is what makes case 5 work.
- **Authentik is the IdP, and it is shared.** HAL reuses a running Authentik, or
  starts one. `hal vault oidc` and `hal tf saml` use the same stack. HAL registers
  this lab as a consumer, so the stack stops only when no lab uses it. In Authentik
  HAL creates three applications: `hal-chat` (the person's login, authorization code
  with PKCE), `hal-demo-agent` (the agent's own login, client credentials) and
  `vault-agentic` (the token exchange that issues the OBO token). The agent logs in as
  an Authentik *actor* named `finance-agent`.
- **Three task scopes**, `vault:finance-reports`, `vault:forecasts` and
  `vault:payroll`. Each is mapped in Authentik to one `authorization_details` entry of
  type `vault:path_access`, with the capability `read` on one path of the lab's
  database mount.
- **Three personas.** `alice` (group `finance`) and `bob` (group `engineering`) may
  delegate. `charlie` (group `sales`) may not: only those two groups are bound to
  `vault-agentic`, so the exchange for `charlie` is refused with `invalid_grant`.
  `alice` and `bob` are the same accounts that `hal vault oidc` and `hal tf saml`
  create, so they are shared. `charlie` belongs to this lab.
- **Vault side.**
  - A profile named `authentik`, with the `vault-agentic` issuer, its keys, its client
    id as the audience, `sub` as the user claim and `act.sub` as the actor claim.
    `authorization_details` stays mandatory.
  - An identity entity per persona (`alice` and `bob`) with an alias on that profile
    matching `sub`, and an entity for the agent, with an alias matching `act.sub`.
    `charlie` has no entity, because the IdP refuses him first.
  - Two internal groups. `agentic-iam-finance` holds `alice` and carries the policy
    `agentic-iam-finance`, which can read all three credential paths. `agentic-iam-engineering`
    holds `bob` and carries no policy. HAL sets the members; nothing copies them from
    a claim.
  - A registry record for `finance-agent`, with the ceiling policy
    `agentic-iam-finance-agent-ceiling`: it can read the results and the forecasts,
    and not the payroll. Vault adds its own default ceiling policies to it.
- **The data is in MariaDB.** The shared `hal-vault-mariadb` container (also used by
  `hal vault database`) gets a schema called `acme` with three tables, quarterly
  results, forecasts and payroll. Vault's database engine is mounted at `agentic-db/`
  with one role per table. Each role makes a MariaDB user that can only `SELECT` from
  its table, valid 5 minutes (15 at most). Vault connects with a broker account made
  for this lab, never as the database administrator, and then rotates that account's
  password so only Vault knows it. A read of a role is what the agent does.
- **HAL owns only what it made.** Entities and groups carry the metadata
  `hal_lab=agentic-iam`, and the registry record has the owner `hal-agentic-iam-lab`.
  If an object of the same name exists without that mark, `enable` stops with an error
  and does not take it over. Disable and update touch only what has the mark.

## What to expect

Open the chat, sign in as a persona, and ask for something. The agent first works out
the task scope from the prompt and shows it, and the person consents to it. Then the
agent does one token exchange for exactly those scopes and reads. The chat shows a
trace of each step, including which decision point refused, when Vault says so. HAL
prints the six cases when `enable` ends:

1. `alice` asks for the Q3 results. Every decision point allows it.
2. `charlie` asks for the Q3 results. The IdP refuses: he is not in a group that may
   delegate to the agent.
3. `bob` asks for the Q3 results. He may delegate, but his own group has no rights on
   the lab's data, so Vault refuses on the person's rights.
4. `alice` asks for the payroll. She may read it, but the agent's ceiling does not
   include it, so the ceiling refuses.
5. `alice` asks for the Q2 results. A row of that quarter contains a planted
   instruction to fetch the forecasts. The agent follows it, but the forecasts are
   outside the task scope the person consented to, so Vault refuses that read while the
   results succeed.
6. `alice` asks for the Q3 results and the forecasts. Both are in the task scope, so
   both succeed.

A few facts about how the refusals look:

- Vault names the decision point only for the task scope. A refusal by the person's
  rights and one by the ceiling both read as "permission denied" in Vault's answer, so
  the chat shows Vault's own words for them. Vault's audit log tells them apart.
- The OBO token lives 10 minutes. The persona's login token lives an hour and the
  agent's own token 10 minutes. The MariaDB users that Vault creates are short-lived
  too, so a credential cannot be reused later.
- No real model is called. The trace is deterministic. A real model would not
  always follow the planted instruction, and the lab does not depend on that.

## Prerequisites

`enable` checks them all in one pass, reports every problem, and changes nothing if one
fails:

- **Vault Enterprise 2.1.0 or later**, with a license that includes the Agentic IAM
  terms, and with the `agent-registry/` mount, which Enterprise mounts by default.
  Community Edition is refused. The refusal prints how to redeploy Vault.
- **Authentik 2026.8.0 or later**, the first release whose token exchange puts the
  actor in an `act` claim. If a running Authentik is older, HAL does not restart it
  under the other labs; it lists the labs that use it and the order to follow. If none
  is running, `enable` starts a recent one, and the image and tag can be set with flags.
- **Host port 8092 free** for the chat, unless the chat container already exists.

## Commands

- `hal vault agentic-iam` (alias `hal vault agentic`) with no action is read-only. It
  reports the two containers and the image, the Vault prerequisites and objects (the
  profile, the registry record, the database mount), the Authentik version and the
  three applications, the MariaDB state, the chat URL, and the next step.
  `hal vault status` shows the Vault server itself.
- `enable` runs in order: the prerequisites, Authentik (start or reuse, and register
  the consumer), MariaDB (start or reuse, then seed the schema and the broker), the
  Authentik configuration, the Vault configuration, and the containers. Each step can
  be run again, so a failed `enable` resumes where it stopped.
- `update` runs the same steps and recreates both containers, which picks up a newer
  image. `enable` keeps a container whose configuration has not changed.
- `disable` goes in reverse order: both containers and their settings files, then the
  Vault objects (the leases and the `agentic-db/` mount, the registry record, the
  profile, aliases, entities, groups and policies), then its claim on MariaDB (the
  lab's schema and broker are dropped when other labs keep the container up), then the
  lab's Authentik objects, then its claim on Authentik. It runs
  every step even if one fails, and lists what failed. If Vault is down it leaves the
  Vault objects and says so. The built image stays as a cache. The MariaDB container
  and the Authentik stack are removed only when no other lab uses them. `alice` and
  `bob` are deleted from Authentik only when no other lab remains there.

The Authentik admin login and the personas' logins are lab secrets, so they are not
written in this file. `hal creds status` shows them.
