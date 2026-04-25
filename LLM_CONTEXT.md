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
- HAL MCP command namespace (`hal mcp`) supports two transports:
    - **stdio** (default, local/dev): `hal mcp serve` — spawned directly by HAL Plus; protocol `2024-11-05`.
    - **streamable-HTTP** (container): `hal mcp serve --transport streamable-http --http-host 0.0.0.0 --http-port 8080 --http-path /mcp`; protocol `2025-03-26`.
    - `hal mcp create --http` pulls `ghcr.io/hashimiche/hal-mcp:latest` from GHCR. No source tree or Go toolchain required on the user machine.
    - The image is published automatically by the release workflow (`Dockerfile.mcp`) on every version tag as `ghcr.io/hashimiche/hal-mcp:latest` and `ghcr.io/hashimiche/hal-mcp:<version>`. It runs as a non-root user (`hal`, uid 10001).
    - `--http-tag` flag overrides the pulled image tag (e.g. to pin a specific version).
    - `hal mcp create|serve|status|delete` remains the primary operator surface.
    - MCP tool surface is read-only and leverages existing HAL command paths (`status`, `capacity`, `<product> status`) instead of reimplementing product logic.
    - Product status tools (for example `get_tfe_status`) should keep product-specific `recommended_commands` first (`hal terraform status`) so AI clients can answer quick health prompts without falling back to generic checks like `hal capacity`.
    - The `hal-mcp` container does **not** mount the host container engine socket and does not run as root. Tool calls that require engine access (for example `hal_status_baseline`) will return an engine-unavailable error in rootless podman deployments — this is expected; HAL Plus handles it gracefully.
    - AI clients must treat this engine-unavailable baseline as **runtime unknown**, not as product up/down evidence. For quick status prompts, respond with `Unknown` and recommend the product-specific status command (for example `hal vault status`).
    - There is no SSH-based MCP transport pattern. Do not introduce or suggest SSH tunnelling for MCP.
- HAL Plus stack lifecycle is managed via `hal plus create|status|delete`:
    - `hal plus create` runs preflight checks (Ollama reachability, model availability, local MCP image presence), ensures `hal-net` exists, then starts `hal-mcp` and `hal-plus` containers on `hal-net`.
    - `hal plus create --image <tag>` uses a local image directly if it exists (no forced pull); pulls from registry only when image is absent.
    - `hal plus delete` tears down both containers.
    - `hal plus status` reports image presence, container state, and endpoint health.
    - Ollama must run on the **host**. HAL Plus contacts it from inside the container via `host.containers.internal:11434` (podman) or `host.docker.internal:11434` (docker). `OLLAMA_BASE_URL` env var overrides the resolved URL.
    - No socket mounts, no `--user` overrides, no `DOCKER_HOST` injection into `hal-mcp`. Podman stays rootless.
- Terraform Enterprise local deployment depends on a mocked PostgreSQL, Redis, and MinIO stack and uses local TLS material under `~/.hal/tfe-certs`.
    - Rootless Podman path uses `https://tfe.localhost:8443` through `hal-tfe-proxy`.
    - Twin TFE lifecycle is target-based on product CRUD commands (for example `hal terraform create --target twin`) instead of a dedicated `hal terraform twin` subcommand.
    - Terraform helper subcommands are lifecycle-only: `hal terraform api-workflow`, `hal terraform vcs-workflow`, and `hal terraform agent` accept `status|enable|disable|update` (no `create|delete` aliases).
    - `hal terraform api-workflow` target scope is `primary|twin` only; do not suggest `--target both` for this helper.
    - Custom local agent-pool flow uses `hal terraform agent enable` and should report running state via `hal terraform agent` before directing users to select Agent execution mode in TFE UI.
    - Task worker agent-run config must keep `/tmp/terraform` writable (not read-only) so remote plans can download Terraform binaries.
    - TFE API responses can emit archivist object links without `:8443`; proxy response rewriting keeps UI/raw plan/apply log links host-reachable.
    - `hal terraform vcs-workflow enable` should describe validation in terms of pushing a new commit to `main`; tag creation alone is not a reliable first-run trigger when the tagged SHA was already ingested from branch pushes.
- `hal status` is a CRUD product that manages the `hal-status` sidecar container:
    - `hal status create` / `hal status update` / `hal status delete` are the operator surface.
    - `hal status update` is the manual escape hatch: refreshes the snapshot for the currently running ecosystem (e.g. after deploying a product extension outside the normal lifecycle).
    - `hal status _serve` is a hidden internal command run inside the `hal-status` container — do not surface it to users.
    - The `hal-status` container reuses `hashimiche/hal-mcp:latest` (same image as `hal-mcp`) with `--entrypoint /usr/local/bin/hal` and `status _serve` as args.
    - It reads a frozen `HAL_STATUS_DATA` JSON env var at startup and serves it at `http://hal-status:9001/api/status` on `hal-net`.
    - The snapshot is built on the **host** (which has engine socket access) by `global.RefreshHalStatus(engine)`, injected as an env var, then the container is recreated. The container itself never touches the engine.
    - `global.RefreshHalStatus(engine)` is called after every product lifecycle event that changes ecosystem state: all product `create`/`delete` commands, and all vault/boundary extension enable/disable commands (`vault k8s`, `vault oidc`, `vault jwt`, `vault ldap`, `vault database`, `boundary mariadb`, `boundary ssh`).
    - `RefreshHalStatus` is a no-op if `hal-net` does not exist or the `hashimiche/hal-mcp:latest` image is not present — safe to call unconditionally.
    - HAL Plus fetches `http://hal-status:9001/api/status` as its primary product state source (via `fetchHalStatusProducts()` in `server/index.mjs`), with `fallbackProductsFromEndpoints()` as a fallback for local dev without containers.
    - The snapshot shape: `{ timestamp, engine, products: [{ product, state, health, reason, endpoint, containers, features: [{ feature, state, health, reason }] }] }`.
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

Before making code changes in either `hal` or `hal-plus`, ask the user to create or confirm a working branch first. Once the branch exists, keep code and LLM markdown updates aligned on that branch.

When AI-facing behavior, prompts, routing, skill guidance, docs policy, or UX behavior changes, update the LLM markdown surfaces across both repos in the same work cycle.

- `hal`: `.github/copilot-instructions.md`, `.github/copilot/skills/**/*.md`, `docs/**/*.md`, `LLM_CONTEXT.md`
- `hal-plus`: `llm/**/*.md`, `design*.md`, `UX_PARITY.md`, `LLM_BEHAVIOR.md`

## Cross-Repo AI Sync Rule

- When changes affect AI-facing behavior (MCP tools, skills metadata, grounding contracts, prompt/response schemas, or deterministic intent routing), apply coordinated updates in both repos: `hal` (truth/tooling) and `hal-plus` (UX/orchestration).
- Do not ship AI contract changes in only one repo when the other side consumes or exposes the same contract.