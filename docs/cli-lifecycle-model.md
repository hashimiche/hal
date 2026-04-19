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
| `hal boundary` | `create`, `delete`, `status`, `mariadb`, `ssh` | Product lifecycle is command-based (`create`/`delete`) with `--update` on `create`. Feature lifecycle is action-based (`status|enable|disable|update`) with hidden compatibility flags. |
| `hal consul` | `create`, `delete`, `status` | Product lifecycle is command-based (`create`/`delete`) with `--update` on `create`. |
| `hal nomad` | `create`, `delete`, `status`, `job` | Product lifecycle is command-based (`create`/`delete`) with `--update` on `create`. Feature command `job` remains action-based. |
| `hal obs` | `create`, `delete`, `status` | Product lifecycle is command-based (`create`/`delete`) with `--update` on `create`. |
| `hal terraform` (alias `hal tf`) | `create`, `delete`, `status`, `agent`, `api-workflow` (alias `api`), `workspace` | Product lifecycle is command-based (`create`/`delete`) with `--update` on `create`. Twin lifecycle is target-based via `--target primary|twin|both` on `create`/`update`/`delete`/`status`. |
| `hal vault` | `create`, `delete`, `status`, `audit`, `database`, `jwt`, `k8s`, `ldap`, `oidc` | Product lifecycle is command-based (`create`/`delete`) with `--update` on `create`. Feature lifecycle is action-based (`status|enable|disable|update`) with hidden compatibility flags. |
| `hal mcp` | `create`, `update`, `delete`, `status`, `policy` | Product lifecycle is command-based (`create`/`update`/`delete`). `policy` is read-only today. |

## Target Command Model

Intent:

- Product resources use `create`, `update`, `delete`, `status`.
- Product features use `enable`, `update`, `disable`, `status`.
- Exceptions are explicit and documented in this file.

### Product-level verbs (target)

| Product | Target lifecycle verbs | Notes |
|---|---|---|
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
| `hal vault database` | `enable`, `update`, `disable`, `status` | Same as above. |
| `hal vault audit` | `enable`, `update`, `disable`, `status` | Same as above. |
| `hal boundary mariadb` | `enable`, `update`, `disable`, `status` | Target resource behavior fits feature model. |
| `hal boundary ssh` | `enable`, `update`, `disable`, `status` | Target resource behavior fits feature model. |
| `hal terraform agent` | `enable`, `update`, `disable`, `status` | Treat as product feature. |
| `hal terraform api-workflow` (alias `api`) | `enable`, `update`, `disable`, `status` | Treat as product feature. |
| `hal terraform workspace` | `enable`, `update`, `disable`, `status` | Treat as product feature. |
| `hal mcp policy` | `create`, `update`, `delete`, `status` | Explicitly modeled as a managed resource. |

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
