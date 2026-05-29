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
    - `get_tfe_vcs_workflow_status` is a structured, read-only scenario tool for the Terraform VCS-driven workflow: it overlays live booleans (TFE runtime, `hal-gitlab` container, `~/.hal/tfe-app-api-token` presence) onto the canonical primary-target defaults (TFE org `hal` / project `Dave` / workspace `tfe-agent-demo`, GitLab `root/tfe-agent-demo` on `main`) and returns `data.{target,gitlab{web_url,...},tfe{workspace_url,runs_url,...},lab_credentials{gitlab,tfe_admin},ready}`. `lab_credentials` are non-secret demo values intentionally surfaced (lab-scoped, redaction-exempt). v1 covers the primary target only; twin degrades gracefully.
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
    - TFE admin HTTPS is exposed through the same proxy at `https://tfe.localhost:8444`.
    - Twin TFE lifecycle is target-based on product CRUD commands (for example `hal terraform create --target twin`) instead of a dedicated `hal terraform twin` subcommand.
    - Terraform helper subcommands are lifecycle-only: `hal terraform api-workflow`, `hal terraform vcs-workflow`, and `hal terraform agent` accept `status|enable|disable|update` (no `create|delete` aliases).
    - `hal terraform api-workflow` target scope is `primary|twin` only; do not suggest `--target both` for this helper.
    - Custom local agent-pool flow uses `hal terraform agent enable` and should report running state via `hal terraform agent` before directing users to select Agent execution mode in TFE UI.
    - Task worker agent-run config must keep `/tmp/terraform` writable (not read-only) so remote plans can download Terraform binaries.
    - TFE API responses can emit archivist object links without `:8443`; proxy response rewriting keeps UI/raw plan/apply log links host-reachable.
    - `hal terraform vcs-workflow enable` should describe validation in terms of pushing a new commit to `main`; tag creation alone is not a reliable first-run trigger when the tagged SHA was already ingested from branch pushes.
- `hal health` is a CRUD product that manages the `hal-health` sidecar container:
    - `hal health create` / `hal health update` / `hal health delete` are the operator surface.
    - `hal health update` is the manual escape hatch: refreshes the snapshot for the currently running ecosystem (e.g. after deploying a product extension outside the normal lifecycle).
    - `hal health _serve` is a hidden internal command run inside the `hal-health` container — do not surface it to users.
    - The `hal-health` container reuses `hashimiche/hal-mcp:latest` (same image as `hal-mcp`) with `--entrypoint /usr/local/bin/hal` and `health _serve` as args.
    - It reads a frozen `HAL_HEALTH_DATA` JSON env var at startup and serves it at `http://hal-health:9001/api/status` on `hal-net`.
    - The snapshot is built on the **host** (which has engine socket access) by `global.RefreshHalHealth(engine)`, injected as an env var, then the container is recreated. The container itself never touches the engine.
- `global.RefreshHalHealth(engine)` is called after every product lifecycle event that changes ecosystem state: all product `create`/`delete` commands, and all vault/boundary extension enable/disable commands (`vault k8s`, `vault oidc`, `vault jwt`, `vault ldap`, `vault database`, `boundary mariadb`, `boundary ssh`).
    - `RefreshHalHealth` is a no-op if `hal-net` does not exist or the `hashimiche/hal-mcp:latest` image is not present — safe to call unconditionally.
    - HAL Plus fetches `http://hal-health:9001/api/status` as its primary product state source (via `fetchHalStatusProducts()` in `server/index.mjs`), with `fallbackProductsFromEndpoints()` as a fallback for local dev without containers.
    - The snapshot shape: `{ timestamp, engine, products: [{ product, state, health, reason, endpoint, containers, features: [{ feature, state, health, reason }] }] }`.
- `hal vault oidc` deploys Authentik as a shared IdP and wires Vault OIDC auth. See `docs/scim-idp-spec.md` for full architecture.
    - **Command**: `hal vault oidc enable|disable|update|status`. Flags: `--scim` (Vault Enterprise), `--authentik-image`, `--authentik-tag`.
    - **Authentik containers**: `hal-authentik-pg` (postgres:16-alpine), `hal-authentik-server`, `hal-authentik-worker` — all on `hal-net`. No static IPs (Docker DNS). No Docker socket mounted.
    - **Ports**: HTTP `9100`, HTTPS `9143`. `AUTHENTIK_LISTEN__HTTP=0.0.0.0:9100` must be set explicitly so internal and external port match. Internal port defaults to 9000 otherwise.
    - **Canonical URL**: `authentik.localhost:9100` via `--network-alias authentik.localhost`. macOS resolves `*.localhost` → 127.0.0.1; Docker containers resolve via network alias. This keeps the OIDC issuer URL identical from host browser and from the Vault container.
    - **Bootstrap token**: `AUTHENTIK_BOOTSTRAP_TOKEN` must be set on **both server and worker** containers. The worker runs Celery tasks that actually create the token record in the database — if the worker does not have it, the token is never persisted and all API calls get 403.
    - **Bootstrap token race**: After `WaitAuthentikHealthy()` (polls `/api/v3/root/config/` — unauthenticated), always call `WaitAuthentikTokenReady(token)` before any API calls. It polls `GET /api/v3/core/groups/` with `Authorization: Bearer <token>` until 200 (60 s timeout). This ensures admin-level access is ready, not just server health.
    - **Default scope seeding lag**: On first boot, the bootstrap token can become usable before the worker finishes blueprint tasks that create the standard OAuth scope mappings (`openid`, `profile`, `email`). `WaitAuthentikScopesReady(token)` now polls `/api/v3/propertymappings/provider/scope/` for up to 180 s and prints visible progress (`0/3` -> `3/3`) before OIDC provisioning continues.
    - **First-start migration race fix**: `StartAuthentikStack()` now waits for server API readiness immediately after launching `hal-authentik-server` and only then starts `hal-authentik-worker`. This avoids worker blueprint tasks racing DB migrations (observed error: `relation "authentik_tenants_tenant" does not exist` on first `hal vault oidc enable --scim`).
    - **Execution order for `hal vault oidc enable --scim`** (authoritative runtime sequence):
        1. Load/create secrets (`~/.hal/authentik/env`).
        2. Start Authentik stack (pg -> server -> worker) if not already running.
        3. Wait API health, then wait bootstrap token readiness.
        4. On first boot, wait for default scope mappings (`openid/profile/email`) to be seeded. This wait is separate from token readiness and can take up to 180 s on a fresh Authentik database.
        5. Provision Authentik OIDC objects (groups/users, OAuth2 provider, app).
        6. Verify Vault can resolve/reach the Authentik OIDC discovery URL.
        7. Configure Vault OIDC (auth mount, role, policies, identity groups/aliases).
        8. Configure Vault SCIM (activation flag, `scim-client` policy incl. `patch`, entity, token role, SCIM client).
        9. Mint SCIM bearer token, upsert Authentik SCIM provider, assign as app backchannel.
        10. Run `syncSCIMObjects` (users first, then groups) for initial consistency.
    - **`update` pattern**: cleans Vault OIDC mounts/policies, deletes the Authentik application + OAuth2 provider by name, then falls through to the enable path for fresh re-provision. Does not bounce the Authentik stack. Always calls `WaitAuthentikTokenReady` even when Authentik is already running.
    - **Authentik REST API (version 2026.x / 2024.4+ changes)**:
        - Scope mappings: `/api/v3/propertymappings/provider/scope/` (was `/propertymappings/scope/` before 2024.4)
        - `CreateOAuth2Provider` requires both `authorization_flow` AND `invalidation_flow` fields — use `GetDefaultInvalidationFlowPK()` which prefers the `default-provider-invalidation-flow` slug.
        - `GetDefaultAuthorizationFlowPK()` prefers slugs containing `"implicit"` — prevents picking `explicit-consent` which orphans the OAuth state and causes "Expired or missing OAuth state" on the second login attempt.
        - Groups scope mapping no longer ships as a default in 2026.x — `GetGroupsScopeMappingPK()` creates it automatically if not found (`scope_name: groups`, expression: `list(request.user.ak_groups.values_list("name", flat=True))`).
        - `CreateApplication` sets `meta_launch_url` to `http://vault.localhost:8200/ui/vault/auth/oidc` so the Authentik tile points to the host-reachable URL, not the SCIM backchannel URL.
    - **Secrets file**: `~/.hal/authentik/env` (mode 0600). Keys: `PG_PASS`, `AUTHENTIK_SECRET_KEY`, `AUTHENTIK_BOOTSTRAP_TOKEN`, `AUTHENTIK_BOOTSTRAP_PASSWORD`. Loaded on every start — secrets are stable across restarts.
    - **Shared service registry**: service key `"authentik-idp"`, consumer `"vault-oidc"`. Authentik stack is torn down on disable only when no consumers remain.
    - **Demo users**: `alice / password` → group `admin` → Vault policy `admin` (all paths); `bob / password` → group `user-ro` → Vault policy `user-ro` (kv-oidc/team1 read).
    - **OIDC role redirect URIs**: `http://localhost:8250/oidc/callback`, `http://127.0.0.1:8250/oidc/callback`, `http://vault.localhost:8200/ui/vault/auth/oidc/oidc/callback` — all three required for CLI and browser flows.
    - **SCIM (`--scim`, Vault Enterprise only)**:
        - Performs an early Enterprise gate check (`sys/license/status`) before any provisioning. Exits with a clear error if not Enterprise.
        - Step 0: if a prior non-SCIM `hal vault oidc enable` ran, external groups `admin`/`user-ro` exist without a `scim_client_id`. These are deleted before SCIM setup so Authentik can take ownership without 409 conflicts.
        - Activates SCIM feature flag via `sys/activation-flags/enable-scim/activate` (one-way, permanent).
        - Creates `scim-client` policy covering `identity/scim/v2/*` with **create/read/update/patch/delete/list**. The `patch` capability is critical — Vault 1.14+ treats HTTP PATCH as a separate capability from `update`; without it group membership PATCH returns `permission denied` and member lists stay empty forever.
        - Creates entity `scim-client-authentik` and token role `authentik-scim` (32-day renewable orphan).
        - Creates SCIM client `authentik-scim` with `alias_mount_accessor = oidc/` — ensures SCIM-provisioned entities merge with OIDC-authenticated entities so group policies apply on login.
        - Creates Authentik outbound SCIM provider `vault-scim-provider` with `compatibility_mode: "aws"`. Without AWS mode, Authentik 2026.x includes `"schemas"` inside PATCH Operation objects which Vault SCIM rejects with 400/`invalidValue` — member lists stay empty.
        - Assigns SCIM provider as backchannel on `hashicorp-vault` application (required for outbound sync activation).
        - OIDC path skips pre-creating external groups when `--scim` is active; Authentik SCIM owns group creation.
        - **Group membership IS auto-propagated** in Authentik 2026.2.3+ — the previously known ManyToMany Django signal gap has been fixed. Membership changes (add/remove user from group) now fire outbound SCIM events automatically.
        - Users created *before* the SCIM provider was first configured are not retroactively pushed. `configureSCIM` and `syncSCIMObjects` call a full users+groups sync at the end to ensure initial consistency.
        - Valid `sync_object_model` enum values: `"authentik.core.models.Group"`, `"authentik.core.models.User"` (Python module path, from `SyncObjectModelEnum` in OpenAPI schema).
        - SCIM endpoint inside containers: `http://hal-vault:8200/v1/identity/scim/v2`.
    - **Future**: `hal tf saml enable [--scim]` on a separate branch will reuse the same Authentik stack with `AuthentikSharedServiceKey`.
    - **Implementation files**: `cmd/vault/oidc.go`, `cmd/vault/scim.go`, `internal/integrations/authentik.go`.
- `hal tf saml` deploys Authentik as a shared IdP and wires TFE SAML SSO. See `docs/scim-idp-spec.md` for full architecture.
    - **Command**: `hal tf saml enable|disable|update|status`. Flags: `--scim` (pending), `--target primary|twin`, `--tfe-url`, `--tfe-org` (default `hal-org`), `--authentik-image`, `--authentik-tag`.
    - Reuses the same Authentik stack (`AuthentikSharedServiceKey = "authentik-idp"`). Shared-service consumers: `"tfe-saml"` (primary), `"tfe-bis-saml"` (twin).
    - **Authentik objects**: groups `admins`/`devs`, users `alice`/`bob`, SAML property mappings `hal: SAML Username` (attr: `Username`) and `hal: SAML Groups` (attr: `MemberOf`), SAML provider `tfe-saml-provider`, application slug `tfe-saml`.
    - **TFE SAML**: configured via `PATCH /api/v2/admin/saml-settings`. Admin token is auto-bootstrapped via `ensureTFEFoundation`. SSO endpoint and IdP cert are parsed from Authentik SAML metadata XML (`GetSAMLProviderMetadata` + `ParseSAMLMetadata`). TFE_HOSTNAME must remain **portless** (`tfe.localhost`) — the HTTPS proxy port is separate from the hostname.
    - **Port 443 constraint (macOS/Podman rootless)**: TFE's ACS URL is always `https://tfe.localhost/users/saml/auth` (portless, port 443). Port 443 cannot be bound without root. Solution: `hal-authentik-saml-proxy` — an nginx:alpine container on `hal-net` at port 9102 that proxies to `hal-authentik-server:9100` and uses `sub_filter_types *; sub_filter_once off; sub_filter 'https://tfe.localhost/users/saml/auth' 'https://tfe.localhost:8443/users/saml/auth';` to rewrite the ACS URL in Authentik's flow-executor JSON response before the browser receives it. Config written to `~/.hal/authentik-saml-proxy.conf`. Container: `hal-authentik-saml-proxy`. Constants: `AuthentikSAMLProxyContainer`, `AuthentikSAMLProxyPort = "9102"`.
    - **Authentik 2026 SAML flow**: uses `ak-stage-autosubmit` — the ACS URL is delivered via `GET /api/v3/flows/executor/…` as `application/json`, not an HTML `<form action="...">`. Sub-filter must use `sub_filter_types *` (not `text/html`) and match the bare URL string.
    - **SSO URL port rewrite**: after parsing SSO URL from metadata, `saml.go` rewrites port `9100→9102` so `sso_endpoint_url` in TFE points through the proxy (`http://authentik.localhost:9102/application/saml/tfe-saml/sso/binding/redirect/`).
    - **`ParseSAMLMetadata`**: forward-scan all `X509Certificate>` occurrences, keep last non-empty base64 candidate between `>` and the next `<`. Closing tags (`</ds:X509Certificate>`) also contain the suffix but yield empty content and are skipped. Do NOT use `strings.LastIndex` — it matches the closing tag. Implemented in `integrations/authentik.go`.
    - **`clearOldTFESAMLCert(engine string)`**: runs `psql … UPDATE rails.admin_settings_saml SET old_idp_cert_encrypted = NULL` via `podman exec hal-tfe-db`. Called after `cleanTFESAML` in the `saml update` flow. Prevents `PEM_read_bio_X509` errors caused by a malformed `old_idp_cert` surviving from a previous failed provision. Best-effort (errors silently ignored).
    - **`provisionTFESAMLTeams(baseURL, apiToken, orgName string)`**: creates `admins` (manage-workspaces/projects/modules/providers) and `devs` (read-workspaces/projects) teams in the target org if they don't exist. Called as step 7b in `runTFESAMLEnable`. TFE only adds SSO users to *existing* teams whose names match `MemberOf` — it does not auto-create teams from SAML group attributes.
    - **Org consistency**: both `hal tf create` (`--tfe-org`, default `hal-org`) and `hal tf saml enable` (`--tfe-org`, default `hal-org`) use `hal-org` as the canonical default org name.
    - **Twin target**: uses `tfe-bis-saml-provider` / `tfe-bis-saml` slug / `https://tfe-bis.localhost:9443`. Same code path with target-specific URL helpers.
    - **SCIM (`--scim`)**: `--scim` is implemented as a demo feature. **⚠️ Limitation**: TFE Admin UI states SCIM is only compatible with Microsoft Entra ID and Okta — Authentik is unsupported, so team-membership reconciliation is partial. Users appear in TFE SCIM at the instance level and in org teams after the initial seed, but ongoing automatic reconciliation may not work reliably. For production org access, the SAML-only `attr_groups`/`MemberOf` path is the supported approach.
    - **SCIM flow**: (0) `cleanPreSCIMTFETeams` deletes manually-created `admins`/`devs` TFE teams so SCIM can own them. (0.5) `enableTFESCIMSettings`: `PATCH /api/v2/admin/scim-settings {enabled:true}` — TFE 2.0+ only; 404 silently skipped; 422 non-fatal. **Prerequisite**: `provider_type: "saml"` in TFE SAML settings — without it TFE rejects SCIM enable with 422. (1) Creates TFE SCIM token via `POST /api/v2/admin/scim-tokens` (site-admin scope; instance-level groups). (2) Authentik outbound SCIM provider `tfe-scim-provider` with `verify_certificates: false`, `compatibility_mode: "default"`, endpoint `https://hal-tfe-proxy:8443/scim/v2`. (3) Assigns as backchannel. (4) `authentikInitialSCIMSync` (see below). Re-sync: `hal tf saml update --scim --sync`.
    - **`authentikInitialSCIMSync`** (4-step initial seed replacing the old `directSCIMSeedTFE` direct-push approach): Step 1 — sync all users via Authentik per-object API `POST /api/v3/providers/scim/:pk/sync/object/` with `sync_object_model: "authentik.core.models.User"`. Step 2 — sync all groups (pass 1) with `sync_object_model: "authentik.core.models.Group"` so TFE SCIM has group records with members. Step 3 — **`scim-group-mapping`**: TFE 2.0 uses a site-admin SCIM token making groups instance-level (not org-scoped); `findTFESCIMGroupByName` queries `GET /scim/v2/Groups?filter=displayName eq "name"` to get the SCIM UUID, then `POST /api/v2/admin/teams/:id/scim-group-mapping` links each SCIM group to an org team (creating the team via `getOrCreateTFETeam` if needed). Step 4 — sync all groups again (pass 2) so TFE sees a group update while the mapping exists and reconciles team membership. `scim-group-mapping` is a TFE-proprietary API; Authentik has no knowledge of it and cannot create it — hal must run it for any new group to become an org team.
    - **Valid `sync_object_model` values**: `"authentik.core.models.User"` and `"authentik.core.models.Group"` (full Python module paths from `SyncObjectModelEnum` in Authentik OpenAPI schema — the Django app-label format `authentik_core.user` is **not** valid).
    - **Ongoing sync**: Authentik's event-driven SCIM handles user/group changes automatically after the initial seed. New groups reach TFE SCIM but won't appear as org teams until `hal tf saml update --sync --scim` is run (to create the `scim-group-mapping`).
    - **Disable flow SCIM cleanup**: `runTFESAMLDisable` calls `disableTFESCIMOnTFE` (in `saml_scim.go`) which deletes all SCIM tokens then `DELETE /api/v2/admin/scim-settings`. Without this, a subsequent `enable --scim` would fail with 422 because SCIM settings from the old run would remain stale.
    - **`update` pattern**: calls `cleanTFESAML` + `clearOldTFESAMLCert`, deletes Authentik application + SAML/SCIM providers, then falls through to the enable path.
    - **Implementation files**: `cmd/terraform/saml.go`, `cmd/terraform/saml_scim.go`, `internal/integrations/authentik.go` (SAML/proxy methods appended).
- Shared runtime helpers live under `internal/global`, especially engine detection and network management.
- Engine resource advisory helpers live under `internal/global`; reuse them instead of open-coding engine-specific capacity checks in individual commands.
- Vault k8s demo (`hal vault k8s`) now supports two explicit demo modes behind the same nginx endpoint (`http://web.localhost:8088`):
    - Native mode: `VaultStaticSecret` sync to Kubernetes secret, injected as env var, HTML rendered in-pod.
    - CSI mode (`--csi`, Enterprise): `CSISecrets` projection via `csi.vso.hashicorp.com`, HTML rendered from mounted file.
- `hal vault pki` manages a two-tier Vault PKI CA chain (Root CA `pki-root` + Intermediate CA `pki-int`, RSA-4096) and two optional K8s demo modes:
    - `--k8s`: deploys Jetstack cert-manager to a shared KinD cluster, configures a dedicated `kubernetes-pki/` Vault auth mount (always independent of `kubernetes/`), creates `ClusterIssuer vault-pki-issuer`, issues cert `hal-web-pki-cert`, exposes nginx web pod at `https://pki.localhost:8089` (NodePort 30082). The nginx pod installs openssl at startup to decode the cert into the page.
    - `--acme`: enables Vault's built-in ACME endpoint (`pki-int/config/acme`), creates role `acme-demo` with short TTL (default `5m`), deploys a Caddy pod that obtains its cert via ACME directly from Vault (no cert-manager), exposes a live web page at `https://acme.localhost:8090` (NodePort 30083) showing a countdown to cert expiry and a renewal badge when Caddy auto-renews.
    - Both modes share the same KinD cluster (via `writeHALKindConfig()` in `helper.go`) with all 3 port mappings declared upfront (30080→8088, 30082→8089, 30083→8090).
    - `update --acme` (without `--force`) preserves PKI engines, re-syncs `acme-demo` role TTL and `config/acme max_ttl` in Vault, and does a `kubectl rollout restart` on the Caddy deployment to clear its cert cache and force a fresh ACME exchange with the new TTL.
    - Vault 2.x ACME TTL control: `config/acme max_ttl` caps all ACME-issued certs on the mount — it **must** be set to the desired TTL because Vault defaults to `2160h` regardless of the role TTL. The role `acme-demo` also sets `ttl`/`max_ttl` and `allow_any_name: true` (needed for `acme.localhost` domain).
    - `--acme-cert-ttl` flag (default `5m`) controls both `config/acme max_ttl` and the role TTL.
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
    - `hal delete` also removes the `hal-net` Docker network after all containers are gone via `global.CleanNetworkIfEmpty()`.
    - If `hal-net` cannot be removed (non-HAL containers still attached), the command prints the blocker list and exits with code 1. The user should stop those containers and re-run `hal delete`.
    - Teardown output uses a `CleanStatus` type: `cleaned`, `not deployed`, or `clean failed` — so "MCP artifacts: not deployed" means MCP was never started, not an error.
- `hal daisy` is a cinematic tribute teardown flow with minimum-duration rendering and reverse random memory-bar decay.
- `hal-net` Docker network behaviour:
    - Created on demand by `global.EnsureNetwork()` before any product that needs inter-container networking.
    - No subnet is enforced by default — the engine picks freely. On Rancher Desktop this can conflict with static proxy IPs needed by TFE.
    - `--network-subnet <cidr>` (global persistent flag) pins the subnet on first creation only. Example: `hal --network-subnet 10.89.3.0/24 tf create --enable`.
    - `global.HalNetStaticIP(engine, hostNum)` inspects the live `hal-net` subnet at runtime and returns `<network-prefix>.<hostNum>`. TFE proxy uses host `.250`, twin proxy uses host `.249`. This makes static IPs portable across any subnet the engine assigned.
    - `global.HalNetName` and `global.HalNetSubnet` are exported constants/vars for use across packages.

## Maintenance Rule

If guidance here starts duplicating `.github/copilot-instructions.md`, move the canonical rule there and keep only the repo-specific reminder here.

When lifecycle verbs/flags change (for example replacing force with update), keep all LLM-facing guidance synchronized in the same change set: this file, `.github/copilot/skills/**/*.md`, and MCP-facing docs/contracts (`docs/commands/mcp*.md`, `cmd/mcp/ops_api.go`, MCP test snapshots).

Before making code changes in either `hal` or `hal-plus`, ask the user to create or confirm a working branch first. Every `hal` CLI change must land on a named branch — use `feature/<short-description>` for new capabilities and `bugfix/<short-description>` for fixes. Once the branch exists, keep code and LLM markdown updates aligned on that branch.

When AI-facing behavior, prompts, routing, skill guidance, docs policy, or UX behavior changes, update the LLM markdown surfaces across both repos in the same work cycle.

- `hal`: `.github/copilot-instructions.md`, `.github/copilot/skills/**/*.md`, `docs/**/*.md`, `LLM_CONTEXT.md`
- `hal-plus`: `llm/**/*.md`, `design*.md`, `UX_PARITY.md`, `LLM_BEHAVIOR.md`

## Cross-Repo AI Sync Rule

- When changes affect AI-facing behavior (MCP tools, skills metadata, grounding contracts, prompt/response schemas, or deterministic intent routing), apply coordinated updates in both repos: `hal` (truth/tooling) and `hal-plus` (UX/orchestration).
- Do not ship AI contract changes in only one repo when the other side consumes or exposes the same contract.