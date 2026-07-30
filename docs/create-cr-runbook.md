# Create IssueResolution CR (GitHub ↔ GKE)

When a GitHub issue is opened, a workflow fills an `IssueResolution` template and
`kubectl apply`s CR `issue-<number>` in namespace `hal-agent`. The operator
owns all `status.*` transitions; this workflow writes **spec only**.

Workflow file: [`.github/workflows/create-cr.yml`](../.github/workflows/create-cr.yml).

Template: [`.github/issueresolution.template.yaml`](../.github/issueresolution.template.yaml).

## Trigger

| Event | Filter |
|---|---|
| `issues` (`opened`) | Issue only (not PR); all new issues |

## Spec fields written

| Field | Source |
|---|---|
| `repository` | `github.repository` |
| `issueNumber` | Issue number |
| `issueURL` | Issue HTML URL |
| `author` | Issue author login |
| `title` | Issue title (truncated to 500 chars) |
| `body` | Issue body (truncated to ~16KiB) |
| `labels` | Issue label names |
| `approved` | `false` |
| `maxFixAttempts` | `2` |

Never writes `status.*`.

## Security model

- **`issues` workflows run from the workflow file on the repository default
  branch**, not from a PR or fork.
- Minimal `permissions:` (`contents: read`, `id-token: write`).
- **No long-lived secrets**: GCP access uses OIDC → Workload Identity Federation
  → short-lived SA token. Cluster access vars are repository / environment
  **variables** (not secrets).

## Required configuration

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

## Idempotence

- CR name = `issue-<number>`. If the CR already exists, the workflow exits
  successfully without changes.

## Prerequisites

- T13 applied: GKE cluster, WIF, `gha-approver` RBAC (`create` on
  `issueresolutions`), smoke WIF workflow green.
- Operator running and reconciling `IssueResolution` CRs.
- Workflow merged to the repository **default branch** (not active from PR
  workflow edits alone).

## Handoff

After the CR exists, the operator runs triage (Job 1) and sets
`status.phase = PendingValidation`. Human gate #1 is
[`agent-approve`](agent-approval-runbook.md) (`agent go` / label `agent: go`).
