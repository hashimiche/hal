# HAL CLI - Supplemental Repo Context

Use this file as a concise, repo-local supplement to [.github/copilot-instructions.md](.github/copilot-instructions.md).

Canonical behavior rules, user interaction rules, and build/test commands belong in [.github/copilot-instructions.md](.github/copilot-instructions.md). This file should stay focused on repo-specific architecture patterns and implementation lessons that are easy to forget during code generation.

## Command Architecture

### 1. Two-Tier CLI Structure
Separate infrastructure lifecycle from feature lifecycle to avoid flag collisions.

- Tier 1 core products use explicit verb subcommands.
    - Examples: `hal vault deploy`, `hal boundary destroy`, `hal terraform status`
- Tier 2 feature/integration flows use noun subcommands with lifecycle flags.
    - Examples: `hal vault oidc -e`, `hal boundary mariadb -d`, `hal boundary ssh -f`

### 2. Smart Status Default
If a command is run without lifecycle flags, default to a read-only status view instead of Cobra help.

- Inspect Docker/Podman or the product API first.
- Summarize state as up, down, or degraded.
- Always end with a copy-pasteable `Next Step` command.

### 3. Destructive Cleanup Pattern
For product-level destroy flows, prefer deleting the known local ecosystem directly instead of relying on the product API.

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

## Product Notes

- Boundary target setup has version-sensitive API behavior around auth methods, grant strings, target host-source actions, and brokered credential source attachment.
- Terraform Enterprise local deployment depends on a mocked PostgreSQL, Redis, and MinIO stack and uses local TLS material under `~/.hal/tfe-certs`.
- Shared runtime helpers live under `internal/global`, especially engine detection and network management.
- Observability product integration is centralized through shared artifact registration in `internal/global/obs.go`.
    - Product deploy commands auto-register Prometheus targets when obs is running.
    - Official dashboards are auto-downloaded and imported into Grafana folder `HAL`.
    - Dashboard JSON is normalized so panel datasources resolve to local `hal-prometheus`.
    - Product deploy commands also support `--configure-obs` to backfill monitoring artifacts without redeploying the product.
    - `--configure-obs` should require the obs stack to already be running; it is a refresh action, not a pre-staging action.
- Global teardown logic is centralized for `hal destroy` and `hal daisy`.
    - KinD cleanup includes default cluster name `kind` plus `hal-*` clusters.
    - Leftover KinD containers are removed by cluster label as a fallback.
- `hal daisy` is a cinematic tribute teardown flow with minimum-duration rendering and reverse random memory-bar decay.

## Maintenance Rule

If guidance here starts duplicating `.github/copilot-instructions.md`, move the canonical rule there and keep only the repo-specific reminder here.