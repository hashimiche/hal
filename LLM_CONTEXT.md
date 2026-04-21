# HAL CLI - Supplemental Repo Context

Use this file as a concise, repo-local supplement to [.github/copilot-instructions.md](.github/copilot-instructions.md).

Canonical behavior rules, user interaction rules, and build/test commands belong in [.github/copilot-instructions.md](.github/copilot-instructions.md). This file should stay focused on repo-specific architecture patterns and implementation lessons that are easy to forget during code generation.

## Command Architecture

### 1. Two-Tier CLI Structure
Separate infrastructure lifecycle from feature lifecycle to avoid flag collisions.

- Tier 1 core products use explicit verb subcommands.
    - Examples: `hal vault create`, `hal boundary delete`, `hal terraform status`
- Tier 2 feature/integration flows use noun subcommands with lifecycle actions.
    - Examples: `hal vault oidc enable`, `hal boundary mariadb disable`, `hal boundary ssh update`

### 2. Smart Status Default
If a command is run without lifecycle action, default to a read-only status view instead of Cobra help.

- Inspect Docker/Podman or the product API first.
- Summarize state as up, down, or degraded.
- Always end with a copy-pasteable `Next Step` command.

### 3. Destructive Cleanup Pattern
For product-level delete flows, prefer deleting the known local ecosystem directly instead of relying on the product API.

- Keep an explicit list of containers and volumes tied to the product.
- Use fast local teardown even if the service itself is unhealthy.

## Implementation Patterns

### 4. Output Hygiene
- Use `exec.Command(...).Output()` for read-only container checks when stderr noise from the engine would pollute UI output.
- Keep user-facing output short, state-oriented, and action-oriented.

### 5. Local Infrastructure Gotchas
- Docker volumes cannot be removed while attached containers are running; feature disable flows often need in-container cleanup instead of volume deletion.
- On rootless engines, privileged internal ports may need high host-port mappings while keeping the original internal port for container-to-container traffic.
- Multi-line payloads passed into Linux containers from Go should strip `\r` first with `strings.ReplaceAll(text, "\r", "")`.
- Some older Linux binaries bundled in images can fail on Apple Silicon under Rosetta; prefer avoiding those paths when possible.

### 6. Version Override Contract
- Any create/enable path that launches Docker/Podman containers, KinD clusters, Helm installs, or Multipass VMs must expose explicit flags for runtime versions/images.
- Keep sensible defaults for each flag, but never hardcode an image/channel/version without a user override path.
- For KinD/Helm, expose node image and chart version controls (example pattern: `--kind-node-image`, `--vso-chart-version`).
- For helper sidecars/proxies/support services (for example nginx/minio/openldap UI containers), expose image tag flags alongside primary product flags.

### 7. Engine Capacity Advisory
- Heavy HAL stacks should consult current engine capacity before large deploys, regardless of whether the engine is Docker or Podman.
- Prefer live engine data plus container stats for warnings, not guesses based only on static defaults.
- High-cost create paths should emit compact preflight notes when headroom is tight.
- Interactive confirmation prompts should only trigger when estimated post-create usage exceeds engine limits (CPU > 100% or RAM > machine RAM).
- `hal status` should surface current engine capacity and live usage so users can judge headroom before starting another stack.
- `hal capacity` defaults to the current view.
- `hal capacity --active` (or `--deployed`) shows active heavy deployment composition with per-stack footprint details.
- `hal capacity --pending` shows pending heavy deployment impact estimates.
- Capacity scenario labels should remain infra-centric where appropriate (for example shared GitLab runner and KinD/VSO flows), not exclusively product-centric.
- Memory pressure calculations should exclude cache/buffers (use pressure memory, not allocated/free-cache-inflated baselines).
- Podman on macOS can expose richer machine-runtime telemetry than Docker; use it when available, but keep the command functional for Docker too.

## Product Notes

- Boundary target setup has version-sensitive API behavior around auth methods, grant strings, target host-source actions, and brokered credential source attachment.
- HAL MCP command namespace (`hal mcp`) is a stdio-first MVP for external tool integration.
    - `hal mcp create|update|status|delete` should stay stable for operator UX consistency (with `up/down` aliases for compatibility).
    - Initial MCP tool surface is read-only and should leverage existing HAL command paths (`status`, `capacity`, `<product> status`) instead of reimplementing product logic.
    - Product status tools (for example `get_tfe_status`) should keep product-specific `recommended_commands` first (`hal terraform status`) so AI clients can answer quick health prompts without falling back to generic checks like `hal capacity`.
- Terraform Enterprise local deployment depends on a mocked PostgreSQL, Redis, and MinIO stack and uses local TLS material under `~/.hal/tfe-certs`.
    - Rootless Podman path uses `https://tfe.localhost:8443` through `hal-tfe-proxy`.
    - Twin TFE lifecycle is target-based on product CRUD commands (for example `hal terraform create --target twin`) instead of a dedicated `hal terraform twin` subcommand.
    - Terraform helper subcommands are lifecycle-only: `hal terraform api-workflow`, `hal terraform vcs-workflow`, and `hal terraform agent` accept `status|enable|disable|update` (no `create|delete` aliases).
    - `hal terraform api-workflow` target scope is `primary|twin` only; do not suggest `--target both` for this helper.
    - Custom local agent-pool flow uses `hal terraform agent enable` and should report running state via `hal terraform agent` before directing users to select Agent execution mode in TFE UI.
    - Task worker agent-run config must keep `/tmp/terraform` writable (not read-only) so remote plans can download Terraform binaries.
    - TFE API responses can emit archivist object links without `:8443`; proxy response rewriting keeps UI/raw plan/apply log links host-reachable.
    - `hal terraform vcs-workflow enable` should describe validation in terms of pushing a new commit to `main`; tag creation alone is not a reliable first-run trigger when the tagged SHA was already ingested from branch pushes.
- Shared runtime helpers live under `internal/global`, especially engine detection and network management.
- Engine resource advisory helpers live under `internal/global`; reuse them instead of open-coding engine-specific capacity checks in individual commands.
- Vault k8s demo (`hal vault k8s`) now supports two explicit demo modes behind the same nginx endpoint (`http://web.localhost:8088`):
    - Native mode: `VaultStaticSecret` sync to Kubernetes secret, injected as env var, HTML rendered in-pod.
    - CSI mode (`--csi`, Enterprise): `CSISecrets` projection via `csi.vso.hashicorp.com`, HTML rendered from mounted file.
- Observability product integration is centralized through shared artifact registration in `internal/global/obs.go`.
    - Product create commands no longer auto-register Prometheus targets/dashboards.
    - Observability onboarding is explicit and opt-in via `hal <product> obs <create|update|delete|status>`.
    - Official dashboards are auto-downloaded and imported into Grafana folder `HAL`.
    - Dashboard JSON is normalized so panel datasources resolve to local `hal-prometheus`.
    - For Terraform Enterprise, prefer `hal terraform obs <create|update|delete|status>` for monitoring artifact lifecycle.
    - Terraform obs actions should require the obs stack to already be running; they are refresh/manage actions, not pre-staging actions.
- Global teardown logic is centralized for `hal delete` and `hal daisy`.
    - KinD cleanup includes default cluster name `kind` plus `hal-*` clusters.
    - Leftover KinD containers are removed by cluster label as a fallback.
- `hal daisy` is a cinematic tribute teardown flow with minimum-duration rendering and reverse random memory-bar decay.

## Maintenance Rule

If guidance here starts duplicating `.github/copilot-instructions.md`, move the canonical rule there and keep only the repo-specific reminder here.

When lifecycle verbs/flags change (for example replacing force with update), keep all LLM-facing guidance synchronized in the same change set: this file, `.github/copilot/skills/**/*.md`, and MCP-facing docs/contracts (`docs/commands/mcp*.md`, `cmd/mcp/ops_api.go`, MCP test snapshots).

## Cross-Repo AI Sync Rule

- When changes affect AI-facing behavior (MCP tools, skills metadata, grounding contracts, prompt/response schemas, or deterministic intent routing), apply coordinated updates in both repos: `hal` (truth/tooling) and `hal-plus` (UX/orchestration).
- Do not ship AI contract changes in only one repo when the other side consumes or exposes the same contract.