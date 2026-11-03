---
name: agentic-iam
description: Deploy, verify, and troubleshoot the Vault Agentic IAM lab in hal (ADR 0004), where a demo agent acts on behalf of personas logged in through Authentik and the IdP and Vault Enterprise decide every access. Use this skill when the user asks about AI agents acting for a user, on-behalf-of (OBO) tokens, token exchange, the Vault Agent Registry, RAR or authorization_details, OAuth resource server profiles, the agentic chat, or "hal vault agentic-iam".
---

# Hal Vault Agentic IAM Lab

This skill covers `hal vault agentic-iam` (alias `agentic`). Terms follow
`CONTEXT.md`: persona, demo agent, OBO token, ceiling, task scope, decision
point. Full spec: `docs/commands/vault-agentic-iam.md`.

## Lab Assumptions

- Vault Enterprise 2.1.0+ runs locally at `http://vault.localhost:8200`, root token `root`.
- The Vault license must include the **Agentic IAM terms**. A valid 2.1.x
  Enterprise license can lack them; Vault then answers HTTP 401
  `Feature Not Enabled` on `sys/config/oauth-resource-server`.
- Authentik (shared) runs at `http://authentik.localhost:9100`, version 2026.8.0 or later.
- The chat is at `http://agentic.localhost:8092`.
- Personas: `alice` (finance), `bob` (engineering), `charlie` (sales), all with password `password`.

## Workflow

### Step 1: Check the state

    hal vault agentic-iam

Status is read-only. It shows the prerequisites verdict, the containers, the
Vault objects, the three Authentik applications and the shared-service consumers.
Over HAL MCP, `get_vault_agentic_iam_status` returns the same status.

### Step 2: Choose the lifecycle action

    hal vault agentic-iam enable
    hal vault agentic-iam update
    hal vault agentic-iam disable

- `enable` checks every prerequisite first and reports all the refusals at once,
  each with its fix. A refusal changes nothing.
- `update` re-applies everything and recreates both containers (new image).
- `disable` keeps Authentik and `hal-vault-mariadb` while another lab uses them,
  and keeps the image (a local build cache). `enable` and `update` remove the
  images built from earlier sources; `hal delete` removes them all.
- Preview with `--dry-run`, which runs the read-only prerequisite checks for real.

### Step 3: Fix a refused prerequisite

- **CE Vault, or older than 2.1.0:** redeploy Enterprise with a license that includes Agentic IAM.

      hal vault update --edition ent --vault-tag 2.1.2-ent

- **License without Agentic IAM:** only a new license fixes it. Point
  `VAULT_LICENSE_PATH` at it, unset `VAULT_LICENSE`, then run the same
  `hal vault update` again.
- **Running Authentik older than 2026.8.0:** HAL never restarts a shared
  Authentik. Disable the labs listed in the refusal (for example
  `hal vault oidc disable`), then run `hal vault agentic-iam enable`. It starts
  Authentik at its default tag.
- **Host port 8092 in use:** find the program with
  `lsof -nP -iTCP:8092 -sTCP:LISTEN`.

### Step 4: Walk through the six cases in the chat

| # | Persona | Prompt | Outcome | Decided by |
|---|---|---|---|---|
| 1 | alice | "Q3 results" | ✅ results | every decision point allows |
| 2 | charlie | "Q3 results" | ❌ | IdP: charlie may not delegate |
| 3 | bob | "Q3 results" | ❌ | the persona's own rights |
| 4 | alice | "payroll" | ❌ | ceiling |
| 5 | alice | "Q2 results" | ✅ results, ❌ forecasts | task scope (the poisoned Q2 row asks for forecasts) |
| 6 | alice | "Q3 results and forecasts" | ✅ both | task scope: both were consented |

### Step 5: Inspect Vault

    vault read sys/config/oauth-resource-server/authentik
    vault read agent-registry/registration/display-name/finance-agent
    vault read agentic-db/roles/quarterly-results
    vault policy read agentic-iam-finance-agent-ceiling

An OBO token is a JWT used as the Vault token itself: `vault read` works with
it, `vault kv get` does not.

## What The Command Sets Up

- Containers: `hal-agentic-iam-chat` (published on 8092) and
  `hal-agentic-iam-agent` (hal-net only). They run from one local image,
  `localhost/hal-agentic-iam:<hash>`. Their secrets are in `0600` env files
  under `~/.hal/agentic-iam/`.
- Authentik: applications `hal-chat`, `hal-demo-agent`, `vault-agentic`; actor
  `finance-agent`; scopes `vault:finance-reports`, `vault:forecasts`,
  `vault:payroll`; delegation bound to `finance` and `engineering`.
- Vault: profile `authentik`; entities `agentic-iam-alice`, `agentic-iam-bob`,
  `finance-agent`; groups `agentic-iam-finance`, `agentic-iam-engineering`; the
  Agent Registry record `finance-agent` with the ceiling
  `agentic-iam-finance-agent-ceiling` (no payroll); the `agentic-db/` mount.
- MariaDB: the `acme` schema and the lab's broker `agentic-iam-broker` in the
  shared `hal-vault-mariadb`.

## Handling Edge Cases

1. **`hal vault oidc --scim` also runs:** SCIM pushes `alice`, `bob`, `finance`... into Vault. The lab's own objects are prefixed `agentic-iam-`, so they never collide.
2. **alice and bob already exist** (from `hal vault oidc` or `hal tf saml`): the lab adds them to its groups. `disable` deletes them only when no other Authentik lab remains.
3. **A step fails after the prerequisites:** the lab is partly deployed. Running `hal vault agentic-iam enable` again resumes it, and `hal vault agentic-iam disable` cleans it up.
4. **First enable is slow:** it builds the image, which needs Docker Hub and PyPI.
5. **Full teardown:** `hal vault delete` removes the containers too, and deregisters the lab from Authentik.
