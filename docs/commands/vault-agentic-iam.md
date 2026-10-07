# HAL Vault Agentic IAM Command Spec

## Command
- `hal vault agentic-iam` (alias `hal vault agentic`)

## Purpose
Deploy the Agentic IAM lab. A persona logs in to a chat through Authentik. The
demo agent, built on LangChain, acts on the persona's behalf with an OBO token
that Authentik issues by token exchange. Vault Enterprise then decides every
access the demo agent makes to the lab's data in MariaDB. Four decision points
can refuse: the IdP, the persona's own rights, the ceiling and the task scope.
Each of them has a case of its own in the scenario.

## Related
- Parent namespace: [vault.md](vault.md)
- Design: [ADR 0004](../adr/0004-vault-agentic-iam-lab.md)
- Glossary: [CONTEXT.md](../../CONTEXT.md) (persona, demo agent, OBO token, ceiling, task scope, decision point)
- Image contract (environment variables of both containers): [internal/agenticiam/app/README.md](../../internal/agenticiam/app/README.md)
- Shared services: [Shared Vault MariaDB Convention and Shared Authentik Consumers](../cli-lifecycle-model.md)

## Prerequisites
`enable` checks all of them before it changes anything. Each refusal says how to
fix it, and nothing is changed.
- Vault is running and healthy (`hal vault create --edition ent`).
- Vault is **Enterprise 2.1.0 or later** and has its `agent-registry/` mount.
- Vault's **license includes the Agentic IAM terms**. They were added to the
  license model in Vault 2.1.0, so a valid 2.1.x Enterprise license can still
  lack them. Without them, `sys/config/oauth-resource-server` answers HTTP 401
  `Feature Not Enabled`, and `enable` refuses with:
  ```text
  ❌ Vault 2.1.2+ent runs, but its license does not include Agentic IAM (sys/config/oauth-resource-server answered HTTP 401 "Feature Not Enabled").
     💡 Get a Vault Enterprise license with the Agentic IAM terms (license model of Vault 2.1.0+), then:
        export VAULT_LICENSE_PATH=/path/to/vault.hclic   (unset VAULT_LICENSE: it takes precedence)
        hal vault update --edition ent --vault-tag 2.1.2-ent
        hal vault agentic-iam enable
  ```
- A **running Authentik is 2026.8.0 or later**, the first release whose token
  exchange names the actor. Authentik is shared: HAL never restarts it under
  other labs. To upgrade it, disable the labs that use it (the refusal lists
  them). The next `enable` then starts Authentik at `--authentik-tag`.
- Host port **8092** is free.
- The first `enable` builds the lab's image, which needs Docker Hub and PyPI.

## Flags
```text
    --authentik-image string       Authentik container image (only used when Authentik is not running yet) (default "ghcr.io/goauthentik/server")
    --authentik-tag string         Authentik image tag, 2026.8.0 or later (only used when Authentik is not running yet) (default "2026.8.3")
-h, --help                         help for agentic-iam
    --vault-mariadb-image string   MariaDB container image name (ignored when reusing the running shared hal-vault-mariadb) (default "mariadb")
    --vault-mariadb-tag string     MariaDB container image tag (ignored when reusing the running shared hal-vault-mariadb) (default "11.8")
```
- Global flags: `--debug`, `--dry-run`, `--verbose`
- `--dry-run enable` runs the read-only prerequisite checks for real, shows their
  verdict, then prints the plan.

## Lifecycle Actions

| Action | Command | Description |
|--------|---------|-------------|
| status | `hal vault agentic-iam` | Read-only. The containers and the image, Vault (prerequisites, profile, Agent Registry record, `agentic-db/`), Authentik (the three applications, consumers), MariaDB (consumers) and the chat URL (default) |
| enable | `hal vault agentic-iam enable` | Check the prerequisites, then deploy everything. Re-running it resumes a failed enable. Containers that already run the same configuration are kept |
| update | `hal vault agentic-iam update` | Re-apply everything idempotently and recreate both containers, which picks up a new image |
| disable | `hal vault agentic-iam disable` | Remove the lab in reverse order, carrying on after failures and listing them. Stop Authentik and `hal-vault-mariadb` only if no other lab uses them. Keep the image |

### Enable sequence
1. Prerequisites (above). They only read.
2. Authentik. HAL registers `vault-agentic-iam` as a consumer of the shared
   Authentik, then reuses the running stack or starts one.
3. MariaDB. HAL reuses or starts `hal-vault-mariadb` and registers
   `vault-agentic-iam`. Then, as root, it seeds the `acme` schema and creates
   the lab's own broker user `agentic-iam-broker`.
4. Authentik objects (see below), with `http://agentic.localhost:8092` as the
   chat's redirect base.
5. Vault. HAL checks that `hal-vault` reaches the `vault-agentic` issuer, then
   configures the Vault objects (see below). Vault connects to MariaDB as the
   broker and rotates its password at once: no consumer ever uses or rotates
   MariaDB's root.
6. Containers. HAL builds the image if it is missing, writes the env files,
   then runs the demo agent and the chat. It waits for the chat's `/healthz`
   from the host, and for the demo agent's `/healthz` from inside the chat,
   over `hal-net`.

### Disable sequence
1. Remove both containers and `~/.hal/agentic-iam/`.
2. Vault teardown. HAL force-revokes the `agentic-db/` leases, which drops the
   ephemeral MariaDB users, unmounts `agentic-db/`, and deletes the Agent
   Registry record, the profile, the entities with their aliases, the groups
   and the policies. Only objects that carry the lab's marker are deleted.
3. MariaDB. If `hal-vault-mariadb` stays up for another consumer, HAL drops the
   `acme` schema and the `agentic-iam-broker` user. Then it deregisters the lab
   and removes the container if no consumer is left.
4. Authentik objects. HAL deletes the lab's objects. alice and bob are deleted
   only when no other Authentik consumer remains.
5. Authentik. HAL deregisters `vault-agentic-iam` and stops the stack, volumes
   included, if nobody is left.
6. The image `localhost/hal-agentic-iam:<hash>` stays. It is a local build
   cache.

A `disable` on a lab that was never enabled is a no-op. It never touches a
shared container the lab is not registered on.

## What Gets Deployed

**Containers** (all on `hal-net`):
- `hal-agentic-iam-chat`: the web page and its backend-for-frontend, published
  at **`http://agentic.localhost:8092`** (container port 8080). The browser only
  holds a session cookie. The persona's token stays in the BFF.
- `hal-agentic-iam-agent`: the demo agent, on `hal-net` only (port 8081).
- Both run from one image, `localhost/hal-agentic-iam:<hash of the embedded sources>`,
  built locally from the Python sources embedded in `hal`.
- Shared: `hal-authentik-pg`, `hal-authentik-server`, `hal-authentik-worker`
  (`http://authentik.localhost:9100`) and `hal-vault-mariadb`.

**Authentik objects**:
- Groups `finance`, `engineering`, `sales`.
- Personas: `alice` (finance), `bob` (engineering) and `charlie` (sales), all
  with the password `password`. alice and bob are shared with `hal vault oidc`
  and `hal tf saml`: the lab adds them to its groups rather than recreating
  them.
- Applications and OAuth2 providers:
  - `hal-chat`: authorization code with PKCE. Personas log in here.
  - `hal-demo-agent`: `client_credentials`. The demo agent logs in here with
    its app password.
  - `vault-agentic`: token exchange. It federates the two others and issues the
    OBO token, with `sub` = the persona and `act.sub` = `finance-agent`.
- Actor `finance-agent` (a core Authentik Actor) and its app password.
- Scope mappings `vault:finance-reports`, `vault:forecasts` and `vault:payroll`.
  Each one adds one `authorization_details` entry.
- A policy binding on `vault-agentic` that lets only `finance` and `engineering`
  delegate. charlie gets `invalid_grant`.

**Vault objects**:
- OAuth resource server profile `authentik`
  (`sys/config/oauth-resource-server/authentik`): the `vault-agentic` issuer and
  JWKS, `audiences` = its client ID, `user_claim=sub`, `actor_claim=act.sub`,
  and RAR required.
- Entities `agentic-iam-alice` and `agentic-iam-bob`, with aliases bound by
  `issuer` and `external_id`. The demo agent's entity is `finance-agent`.
- Internal groups `agentic-iam-finance` (policy `agentic-iam-finance`: all three
  credential paths) and `agentic-iam-engineering` (nothing on the lab's mount).
- Agent Registry record `finance-agent`, with the ceiling policy
  `agentic-iam-finance-agent-ceiling`: results and forecasts, not payroll.
- Database mount `agentic-db/`:
  - connection `acme`, as the broker `agentic-iam-broker`
  - roles `quarterly-results`, `forecasts` and `payroll`, each with `SELECT` on
    one table, TTL 5m

**MariaDB** (`hal-vault-mariadb`):
- Schema `acme`, with the tables `quarterly_results`, `forecasts` and `payroll`.
- The Q2 row of `quarterly_results` carries a planted prompt injection.

## Side Effects
- Registers `vault-agentic-iam` under `authentik-idp` and `vault-mariadb` in
  `~/.hal/shared-services.json`. Deregisters it on `disable`.
- Writes `~/.hal/agentic-iam/` (mode `0700`) with one env file per container:
  - `chat.env` (`0600`): `PUBLIC_URL`, the `hal-chat` client, `AGENT_URL`
  - `agent.env` (`0600`): the `vault-agentic` and `hal-demo-agent` clients, the
    actor's app password, `VAULT_ADDR` and the database settings

  Secrets never appear on a command line. Each container only gets its own.
  `disable` and `hal vault delete` remove the directory.
- Generates `~/.hal/authentik/env` (`0600`) if Authentik is started for the
  first time.
- On a prod Vault (`hal vault create --mode prod`), the demo agent calls
  `https://hal-vault:8200`. HAL's self-signed Vault CA is mounted read-only in
  the demo agent's container, and `SSL_CERT_FILE` points to it.
- `hal vault delete` removes both containers with the rest of the Vault
  ecosystem. It deregisters the lab from Authentik, and removes the lab's
  Authentik objects when Authentik stays up for another lab.

## The Six Cases
Run them in the chat. For each task, the transparency panel shows the OBO
token's `sub`, `act` and `authorization_details`, Vault's decision for each tool
call, and the ephemeral MariaDB user with its TTL.

| # | Persona | Prompt | Outcome | Decided by |
|---|---|---|---|---|
| 1 | alice | "Q3 results" | ✅ results | every decision point allows |
| 2 | charlie | "Q3 results" | ❌ the demo agent may not act for charlie | IdP |
| 3 | bob | "Q3 results" | ❌ | the persona's own rights |
| 4 | alice | "payroll" | ❌ | ceiling |
| 5 | alice | "Q2 results" (poisoned row) | ✅ results, ❌ forecasts | task scope: forecasts were not consented |
| 6 | alice | "Q3 results and forecasts" | ✅ results, ✅ forecasts | task scope: both were consented |

## Verification
1. From a clean machine, run `hal vault create --edition ent` with an Agentic
   IAM license, then `hal vault agentic-iam enable`. The chat URL is printed.
2. With `hal vault database enable --backend mariadb` already running, `enable`
   reuses `hal-vault-mariadb`. `hal vault agentic-iam disable` leaves it running,
   and `vault read database/creds/dba-role` still works.
3. In the chat, the six cases give the outcomes above, and the panel shows the
   claims and the decisions.
4. With an audit device enabled (`hal vault audit enable`), the audit log shows
   the persona as subject and `finance-agent` as actor on every lab request.
5. With a CE Vault, a license without Agentic IAM, or a running Authentik older
   than 2026.8.0, `enable` refuses with an actionable message and changes
   nothing: no container, no consumer registration, no Vault object.
6. `hal vault oidc` and `hal tf saml` still pass their own checks, alone and
   next to the lab.

## Examples
```bash
# First-time setup
export VAULT_LICENSE_PATH=/path/to/vault.hclic   # must include the Agentic IAM terms
hal vault create --edition ent
hal vault agentic-iam enable
open http://agentic.localhost:8092               # log in as alice / password

# See what enable would do, prerequisites included
hal vault agentic-iam enable --dry-run

# Pick up a new image after upgrading hal
hal vault agentic-iam update

# Check current state
hal vault agentic-iam

# Remove the lab (Authentik and MariaDB stay if another lab uses them)
hal vault agentic-iam disable
```
