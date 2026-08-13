# HAL CLI Lifecycle Model

Status: Draft contract for command consistency. Documentation-only until implemented in code.

## Why This Exists

This file is the source of truth for HAL CLI lifecycle semantics.

When command behavior changes, keep this file and `.github/copilot-instructions.md` aligned.

## Current Command Inventory (Observed via `hal ... --help`)

### Global (non-product)

| Namespace | Commands |
|---|---|
| `hal` | `boundary`, `consul`, `nomad`, `obs`, `terraform` (alias `tf`), `vault`, `mcp`, `capacity`, `catalog`, `daisy`, `delete`, `status`, `version`, `completion` |

### Product namespaces

| Product namespace | Subcommands | Lifecycle expression today |
|---|---|---|
| `hal aap` | `create`, `update`, `delete`, `status` | Product lifecycle is command-based (`create`/`update`/`delete`/`status`). |
| `hal boundary` | `create`, `delete`, `status`, `obs`, `mariadb`, `ssh` | Product lifecycle is command-based (`create`/`delete`) with `--update` on `create`. Feature lifecycle is action-based (`status|enable|disable|update`) with hidden compatibility flags. Observability artifacts are managed explicitly via `hal boundary obs <create|update|delete|status>`. |
| `hal consul` | `create`, `delete`, `status`, `obs` | Product lifecycle is command-based (`create`/`delete`) with `--update` on `create`. Observability artifacts are managed explicitly via `hal consul obs <create|update|delete|status>`. |
| `hal nomad` | `create`, `delete`, `status`, `obs`, `job` | Product lifecycle is command-based (`create`/`delete`) with `--update` on `create`. Feature command `job` remains action-based. Observability artifacts are managed explicitly via `hal nomad obs <create|update|delete|status>`. |
| `hal obs` | `create`, `delete`, `status` | Product lifecycle is command-based (`create`/`delete`) with `--update` on `create`. |
| `hal terraform` (alias `hal tf`) | `create`, `delete`, `status`, `obs`, `agent`, `api-workflow` (alias `api`), `workspace` | Product lifecycle is command-based (`create`/`delete`) with `--update` on `create`. Twin lifecycle is target-based via `--target primary|twin|both` on `create`/`update`/`delete`/`status`. Terraform observability artifacts are managed under `hal terraform obs <create|update|delete|status>`. |
| `hal vault` | `create`, `delete`, `status`, `obs`, `audit`, `database`, `jwt`, `k8s`, `ldap`, `oidc`, `aap` | Product lifecycle is command-based (`create`/`delete`) with `--update` on `create`. Feature lifecycle is action-based (`status|enable|disable|update`) with hidden compatibility flags. Observability artifacts are managed explicitly via `hal vault obs <create|update|delete|status>`. |
| `hal mcp` | `create`, `update`, `delete`, `status`, `policy` | Product lifecycle is command-based (`create`/`update`/`delete`). `policy` is read-only today. |

## Target Command Model

Intent:

- Product resources use `create`, `update`, `delete`, `status`.
- Product features use `enable`, `update`, `disable`, `status`.
- Exceptions are explicit and documented in this file.

### Product-level verbs (target)

| Product | Target lifecycle verbs | Notes |
|---|---|---|
| `hal aap` | `create`, `update`, `delete`, `status` | Local container lifecycle only. |
| `hal vault` | `create`, `update`, `delete`, `status` | Replace product `deploy/destroy` with `create/delete`; add explicit `update`. |
| `hal mcp` | `create`, `update`, `delete`, `status` | Consolidate `up/down` into `create/delete`; add explicit `update` if needed for reconfiguration. |
| `hal tfe` (or `hal terraform` if alias retained) | `create`, `update`, `delete`, `status` | Align Terraform Enterprise product lifecycle. |
| `hal boundary` | `create`, `update`, `delete`, `status` | Align product lifecycle. |
| `hal consul` | `create`, `update`, `delete`, `status` | Align product lifecycle. |
| `hal nomad` | `create`, `update`, `delete`, `status` | Align product lifecycle. |
| `hal obs` | `create`, `update`, `delete`, `status` | Align product lifecycle. |

### Feature-level verbs (target)

| Product feature command | Target lifecycle verbs | Notes |
|---|---|---|
| `hal vault k8s` | `enable`, `update`, `disable`, `status` | Keep HashiCorp-style engine/integration enablement model. |
| `hal vault ldap` | `enable`, `update`, `disable`, `status` | Same as above. |
| `hal vault oidc` | `enable`, `update`, `disable`, `status` | Same as above. |
| `hal vault jwt` | `enable`, `update`, `disable`, `status` | Same as above. |
| `hal vault aap` | `enabled` (preferred), plus `oidc` lifecycle `enable`, `update`, `disable`, `status` | Configure Vault JWT auth for local AAP OIDC integration. |
| `hal vault database` | `enable`, `update`, `disable`, `status` | Same as above. `--k8s` flag extends enable/update/disable onto the shared KinD cluster using a dedicated `kubernetes-db/` Vault auth mount. |
| `hal vault audit` | `enable`, `update`, `disable`, `status` | Same as above. |
| `hal boundary mariadb` | `enable`, `update`, `disable`, `status` | Target resource behavior fits feature model. |
| `hal boundary ssh` | `enable`, `update`, `disable`, `status` | Target resource behavior fits feature model. |
| `hal vault obs` | `create`, `update`, `delete`, `status` | Observability artifacts are modeled as a managed feature resource. |
| `hal consul obs` | `create`, `update`, `delete`, `status` | Observability artifacts are modeled as a managed feature resource. |
| `hal nomad obs` | `create`, `update`, `delete`, `status` | Observability artifacts are modeled as a managed feature resource. |
| `hal boundary obs` | `create`, `update`, `delete`, `status` | Observability artifacts are modeled as a managed feature resource. |
| `hal terraform agent` | `enable`, `update`, `disable`, `status` | Treat as product feature. |
| `hal terraform api-workflow` (alias `api`) | `enable`, `update`, `disable`, `status` | Treat as product feature. |
| `hal terraform vcs-workflow` | `enable`, `update`, `disable`, `status` | Treat as product feature. |
| `hal terraform obs` | `create`, `update`, `delete`, `status` | Observability artifacts are modeled as a managed feature resource (Prometheus targets + Grafana dashboard artifacts). |
| `hal mcp policy` | n/a | Introspection/export command for HAL MCP runtime answer/tool policy; not a managed lifecycle resource. |

## Password Retrieval Command Family (Target)

Add a password discovery command per product namespace:

- `hal <product> password status`

Examples:

- `hal vault password status`
- `hal mcp password status`
- `hal tfe password status` (or `hal terraform password status` depending on final namespace choice)
- `hal boundary password status`
- `hal consul password status`
- `hal nomad password status`
- `hal obs password status`

## Update Semantics and `--target`

### Replace most `--force` behavior with `update`

Rationale:

- `--force` hides intent and mixes multiple behaviors.
- `update` communicates reconciliation explicitly.
- This aligns with CRUD-style discoverability and docs.

Contract:

- Product: `create`, `update`, `delete`, `status`.
- Feature: `enable`, `update`, `disable`, `status`.
- `update` reconciles existing state to desired state without full teardown unless implementation requires it.

### Scoped updates

When a scope controls multiple components, allow selective update:

- `hal <scope> update --target <component-id>`
- Example: `hal obs update --target hal-grafana`

Rules:

- `--target` is optional.
- No target means update all components in that scope.
- `<component-id>` maps to stable internal IDs (container/service/resource names used by HAL).
- Invalid target fails fast and prints allowed target values.

## Single Source of Truth for Shared Values

HAL provisions multi-container product stacks, and the same identity, endpoint,
credential, image, and path values are consumed by many sibling commands within
a product package (create, delete, status, twin, agent, api/vcs workflow, saml,
obs, foundation). Hardcoding those literals per file causes silent configuration
drift: one command bootstraps `org=hal` while another looks for `org=hal-org`,
and the stack appears healthy while downstream flows fail. This class of bug is
expensive to diagnose because nothing errors at the duplication site.

Rule for every product package under `cmd/<product>/`:

1. Each product owns one `defaults.go` file that declares every value shared
	across more than one file in that package, plus every identity / credential /
	endpoint / URL flag default — even when currently referenced once — because
	those are the values users and downstream automation depend on staying stable.
2. Cobra flag defaults AND their `if !cmd.Flags().Changed(...)` fallback
	overrides MUST reference the same constant. Never repeat the literal in both
	the `StringVar(..., "<literal>", ...)` default and the fallback assignment.
3. Cross-product values (e.g. the Docker network name) live in `internal/global`
	(`global.HalNetName`, `global.HalNetStaticIP`) and are referenced, never
	re-declared inside a product package.
4. Genuinely feature-local values that are used in exactly one file and have no
	identity/credential/endpoint meaning (e.g. a single workflow's demo project
	name) may remain inline in that file.
5. Twin / secondary-instance defaults that intentionally mirror the primary
	(image, version, encryption password, admin identity) reference the primary
	constants so an upgrade in one place propagates; only the values that are
	deliberately different (twin org, twin container name) stay as local literals.

`cmd/terraform/defaults.go` is the reference implementation. The same pattern is
now applied across every product package — `terraform`, `vault`, `consul`,
`nomad`, `observability`, `plus` and `boundary` each own a `defaults.go`. The
Docker network name is never written as a `"hal-net"` literal in a product
package; it always comes from `global.HalNetName`. When adding a new product
package, create its `defaults.go` first and wire every shared value through it.

## Shared KinD Cluster Convention

Several `hal vault` features can run simultaneously on the same KinD cluster.
Each feature that uses `--k8s` must follow these rules:

### Auth mount isolation
Every `--k8s` feature owns a **dedicated** Vault Kubernetes auth mount. No two
features share a mount. Sharing a mount means the second feature to `enable`
overwrites the Kubernetes CA/token config for the first, and the first feature's
`disable` tears down auth for the second.

| Feature | Vault auth mount |
|---------|-----------------|
| `hal vault k8s` | `kubernetes/` |
| `hal vault database --k8s` | `kubernetes-db/` |
| `hal vault pki --k8s` | `kubernetes-pki/` |

When adding a new `--k8s` feature, register a new mount path here and in
`cmd/vault/defaults.go`. Never reuse an existing mount.

### Co-tenant cluster teardown guard
Before running `kind delete cluster`, every `disable` path must check whether
the other features' namespaces are still active and preserve the cluster if any
are. The canonical namespace list to check:

| Namespace | Owned by |
|-----------|---------|
| `app1` | `hal vault k8s` |
| `db-app` | `hal vault database --k8s` |
| `pki-demo` | `hal vault pki --k8s` |
| `pki-acme-demo` | `hal vault pki --acme` |

When the guard preserves the cluster, the `disable` path must still delete its
**own** namespace(s) before returning. A feature that leaves its namespace
behind after disable would be reported as "still active" by every other
feature's guard, and no disable path could ever remove the cluster.

`hal vault delete` is exempt — it is a full ecosystem teardown and always
removes the cluster unconditionally.

### Port map
All `--k8s` features share the same KinD cluster config (`writeHALKindConfig()`
in `cmd/vault/helper.go`). All host port mappings are declared upfront:

| Host port | KinD NodePort | Feature |
|-----------|--------------|---------|
| `8088` | `30080` | `hal vault k8s` |
| `8089` | `30082` | `hal vault pki --k8s` |
| `8090` | `30083` | `hal vault pki --acme` |
| `8091` | `30084` | `hal vault database --k8s` |

When adding a new `--k8s` feature, reserve the next available pair and add it
to `writeHALKindConfig()` so the port is pre-mapped on any existing cluster.

### Network
KinD nodes must be on `hal-net` so Vault and other HAL containers can reach the
API server. All `--k8s` enable paths must call `ensureHALKindCluster()` rather
than invoking `kind create` directly. That helper:

1. Sets `KIND_EXPERIMENTAL_DOCKER_NETWORK` and `KIND_EXPERIMENTAL_PODMAN_NETWORK`
   (the Podman provider ignores the Docker env).
2. Connects every KinD node to `hal-net` if it is missing after create or reuse.
3. Resolves container IPs from the `hal-net` NIC only. Inspecting every attached
   network concatenates addresses when the node is dual-homed (`kind` + `hal-net`).

### Teardown discovery
`hal delete` / `hal daisy` must not depend on `kind get clusters` succeeding.
Older kind CLIs index `.Labels` as a map; Podman 6 stores Labels as a slice, so
that command fails with `cannot index slice/array with type string`. Teardown
lists cluster names with `{{.Label "io.x-k8s.kind.cluster"}}`, always
force-removes leftover `kind-control-plane` nodes, and does not warn for that
known template error.


## Migration Policy

- New UX/docs should prefer explicit `update` over `--force`.
- Legacy docs may mention removed `--force` flows, but the CLI should not keep active `--force` aliases unless there is a hard technical reason.
- New features should not introduce new `--force` flags unless there is a hard technical reason.

## Documentation Maintenance Rule

Whenever CLI behavior, verbs, or lifecycle semantics change:

1. Update this file: `docs/cli-lifecycle-model.md`.
2. Update `.github/copilot-instructions.md` with concise policy deltas and a pointer back to this file.
3. If contributor-facing behavior changed, reflect it in `README.md`.
4. Update LLM-oriented markdown guidance so AI assistants do not emit stale commands:
	- `LLM_CONTEXT.md`
	- `.github/copilot/skills/**/*.md`
	- MCP command docs under `docs/commands/mcp*.md` and `docs/commands/mcp.md`
5. Update MCP implementation/fixtures when syntax changes affect generated command guidance:
	- `cmd/mcp/ops_api.go`
	- `cmd/mcp/testdata/*_help_snapshot.json`
	- `HAL_MCP_CONTRACT.json` if response contract/schema changed
