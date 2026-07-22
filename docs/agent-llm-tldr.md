# HAL Issue-Resolver Agent — LLM TL;DR

**Goal:** An autonomous agent that watches GitHub issues on the public `hal` repo,
triages them, designs a fix, and opens a PR — with **two human gates** (plan
approval, then PR approval). Never auto-merges. Also a portfolio piece: the value
is the *engineering around the LLM* (guardrails, security, evals), not the wiring.

**Core rule — Separation of Privilege:** Models never touch secrets; untrusted
issue text never reaches a context holding credentials. A prompt-injected "print
your token" has nothing to print — the token only enters a non-model publish step
after human approval.

**Deployment — Option 3 (chosen):**
- **Trigger:** GitHub Actions on the public repo (free, no owner-hosted endpoint).
- **Compute:** 100% ephemeral GCP worker VM, one per stage, self-destructs after.
  No always-on server, no idle cost.
- **Secrets:** HCP Vault (managed). Only consumer is the agent VM.
- **Auth, zero static creds:** Actions→GCP via WIF; VM→Vault via GCP instance
  identity (`gcp`/`gce` auth).

**Model tiering:** cheap local Ollama triage → Opus writes `plan.md` (planner↔
executor contract) → Sonnet executes strictly from the approved plan.

**Guardrails:** treat issue text as hostile; GitHub App (not PAT) with minimal
scopes (`contents:write` + `pull_requests:write`, never merge/admin); CODEOWNERS-
lock control-plane paths + executor file allowlist so the agent can't disarm
itself; egress-filter the sandbox. Agent must follow repo conventions
(`copilot-instructions.md`, `LLM_CONTEXT.md`, `CONTRIBUTING.md` branch naming +
doc-sync rule) and may call `hal mcp serve` read-only.

**Scope:** in = docs, wording bugs, flags, tests, small refactors. Out
(`human-only`) = deep container-runtime integrations CI can't validate.

**Status:** Design only. Phase 0 assisted → Phase 1 guardrail scaffolding → Phase 2
full autonomous Option 3 pipeline. See `agent-architecture.md` for full detail.
