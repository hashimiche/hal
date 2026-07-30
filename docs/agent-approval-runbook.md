# Agent approval workflow (GitHub ↔ GKE)

Human gate #1 for the HAL issue-resolver agent. A CODEOWNER approves agent
execution by posting an exact comment or applying a label; a GitHub Action
patches `spec.approved` on the `IssueResolution` CR. The operator owns all
`status.*` transitions.

Workflow file: [`.github/workflows/agent-approve.yml`](../.github/workflows/agent-approve.yml).

## Triggers

| Gesture | Event | Filter |
|---|---|---|
| Comment body exactly `agent go` | `issue_comment` (`created`) | Issue only (not PR); leading/trailing whitespace trimmed; case-sensitive |
| Label `agent: go` | `issues` (`labeled`) | Label name must match exactly |

Removing the label does **not** revoke approval.

## Security model

- **`issue_comment` and `issues` workflows always run from the workflow file on
  the repository default branch**, not from a PR or fork. Contributors cannot
  change approval logic via a PR — only what is merged to `main` executes.
- Restrict `GITHUB_TOKEN` with minimal `permissions:` in the workflow (`contents:
  read`, `id-token: write`, `issues: write`).
- **Authority comes from CODEOWNERS + repository permission**, not from
  `author_association` on the event payload (defense in depth).
- The workflow writes **only** `spec.*` via merge patch (no
  `--subresource=status`). Cluster RBAC (`gha-approver` Role) also omits
  `issueresolutions/status`.
- **No long-lived secrets**: GCP access uses OIDC → Workload Identity Federation
  → short-lived SA token. Non-sensitive Terraform outputs are repository /
  environment **variables** (not secrets).

## Required configuration

### Repository / environment variables

Set on the `hal-cluster` GitHub environment (or repository variables):

| Variable | Source (T13 Terraform) | Example |
|---|---|---|
| `GCP_WIF_PROVIDER` | `wif_provider` | `projects/…/locations/global/workloadIdentityPools/…/providers/…` |
| `GCP_DEPLOYER_SA` | `deployer_sa_email` | `gha-deployer@….iam.gserviceaccount.com` |
| `GKE_CLUSTER_NAME` | `cluster_name` | `hal-agent` |
| `GKE_CLUSTER_LOCATION` | `cluster_location` | `europe-west1` |
| `GCP_PROJECT_ID` | `project_id` | `my-project` |
| `HAL_AGENT_NAMESPACE` | `hal_agent_namespace` | `hal-agent` |

WIF trust conditions should pin `attribute.repository` to this repo and,
ideally, the `hal-cluster` environment name.

### GitHub environment

- Name: **`hal-cluster`** (protected environment; optional required reviewers).
- Must match the WIF attribute condition configured in T13.

### Labels

Pre-create the label **`agent: go`** in the repository (Settings → Labels).

### CODEOWNERS

A repo-wide owner line is required, e.g.:

```
* @hashimiche
```

Team owners (`@org/team`) are resolved via the GitHub API at runtime.

## Patch behavior

Merge patch on `IssueResolution` `issue-<number>` in namespace `hal-agent`:

```json
{"spec":{"approved":true,"approvedBy":"<login>","approvedAt":"<RFC3339>"}}
```

Guards (in order):

1. CR must exist — otherwise comment and fail.
2. `status.phase` must be `PendingValidation` — otherwise comment and fail.
3. If `spec.approved` is already `true` — no-op success (idempotent).

## Prerequisites

- T13 applied: GKE cluster, WIF, `gha-approver` RBAC, smoke WIF workflow green.
- T16 [`create-cr` workflow](create-cr-runbook.md) operational (CR created on
  `issues.opened`; same `hal-cluster` env vars as below).
- Operator running and reconciling `IssueResolution` CRs.

## Operator handoff

After `spec.approved=true`, the operator transitions
`PendingValidation → Ready`, creates Job 2, and eventually sets `PROpen`.
PR merge remains a separate human gate (#2).
