# HAL SCIM / IdP Spec — Authentik Stack

## Overview

Authentik is the shared Identity Provider (IdP) for HAL lab products. It is managed as a shared service — multiple products can register against the same running stack.

**Current status**:
- ✅ `hal vault oidc enable` — Authentik IdP + Vault OIDC auth, fully working
- ✅ `hal vault oidc enable --scim` — Vault Enterprise SCIM provisioning, fully working
- 🔜 `hal tf saml enable [--scim]` — TFE SAML + optional SCIM (separate branch)

---

## User-Facing Commands

```bash
# Deploy Authentik, provision demo users/groups, configure Vault OIDC
hal vault oidc enable

# Re-provision Vault OIDC (keeps Authentik running, fresh clientID/secret)
hal vault oidc update

# Remove Vault OIDC; stop Authentik only if no other product is using it
hal vault oidc disable

# Show Authentik stack health + Vault OIDC mount state
hal vault oidc status
```

Flags:
| Flag | Default | Description |
|---|---|---|
| `--scim` | `false` | [Vault Enterprise] Also configure SCIM provisioning from Authentik |
| `--authentik-image` | `ghcr.io/goauthentik/server` | Authentik image name |
| `--authentik-tag` | `2026.2.3` | Authentik image tag |

---

## Container Architecture

### Names and roles

| Container | Image | Role |
|---|---|---|
| `hal-authentik-pg` | `docker.io/library/postgres:16-alpine` | Authentik database |
| `hal-authentik-server` | `ghcr.io/goauthentik/server:<tag>` | Authentik API + UI (cmd: `server`) |
| `hal-authentik-worker` | `ghcr.io/goauthentik/server:<tag>` | Celery background tasks (cmd: `worker`) |

All three containers join `hal-net`. **No static IPs** — Docker DNS handles name resolution. Static IPs require `hal-net` to be created with an explicit `--subnet`; HAL does not enforce one.

### Ports

| Host port | Container port | Service |
|---|---|---|
| `9100` | `9100` | Authentik HTTP |
| `9143` | `9143` | Authentik HTTPS |

Port 9100/9143 avoids conflicts with 9000 (hal-plus/MinIO), 9001 (hal-health/MinIO console), 9443 (TFE twin HTTPS).

`AUTHENTIK_LISTEN__HTTP=0.0.0.0:9100` **must** be set on the server container so the internal listening port matches the external port. Without this, Authentik listens on 9000 internally and the OIDC issuer URL becomes `http://authentik.localhost:9000/...` while the host port is 9100, breaking OIDC discovery.

### Canonical URL

`authentik.localhost:9100` via `--network-alias authentik.localhost` on the server container.
- macOS resolves `*.localhost` → 127.0.0.1 (host browser)
- Docker containers on `hal-net` resolve via network alias (container-to-container)

This keeps the OIDC issuer URL identical from both contexts.

### No Docker socket

`AUTHENTIK_OUTPOSTS__DISCOVER=false` on both server and worker. HAL does not use Authentik Outposts (proxy/LDAP) and does not mount the Docker socket. This works identically with podman rootless and Docker.

### Volumes

| Name | Mount | Type |
|---|---|---|
| `hal-authentik-db` | `/var/lib/postgresql/data` in pg container | Named volume |
| `~/.hal/authentik/data` | `/data` in server + worker | Bind mount |

### Secrets file

`~/.hal/authentik/env` (mode 0600). Generated once, reused on every start.

```
PG_PASS=<32-char hex>
AUTHENTIK_SECRET_KEY=<64-char hex>
AUTHENTIK_BOOTSTRAP_TOKEN=<40-char hex>
AUTHENTIK_BOOTSTRAP_PASSWORD=<24-char hex>
```

`AUTHENTIK_BOOTSTRAP_TOKEN` and `AUTHENTIK_BOOTSTRAP_PASSWORD` must be set on **both server and worker**. The worker runs the Celery tasks that create the API token record in the database — if the worker doesn't have `AUTHENTIK_BOOTSTRAP_TOKEN`, the token is never persisted.

---

## Startup Sequence and Known Race Conditions

### Bootstrap token race

`WaitAuthentikHealthy()` polls `GET /api/v3/root/config/` (unauthenticated) until 200. This returns 200 as soon as the server starts — but the worker creates the bootstrap token asynchronously after migrations. There is a window where the server is healthy but the token does not exist yet.

**Fix**: always call `WaitAuthentikTokenReady(token)` after `WaitAuthentikHealthy()`, on every path including when Authentik is already running. It polls `GET /api/v3/core/groups/` with `Authorization: Bearer <token>` until 200 (60 s timeout). This tests an admin-only endpoint — same permission class as the provisioning calls.

---

## Authentik REST API — Version Notes (2026.x / 2024.4+)

These endpoints changed and broke against the original implementation:

| What | Old path | New path |
|---|---|---|
| Scope property mappings | `/api/v3/propertymappings/scope/` | `/api/v3/propertymappings/provider/scope/` |

Additional required fields added in 2024.x:

- `POST /api/v3/providers/oauth2/` now requires **`invalidation_flow`** in addition to `authorization_flow`. Use `GetDefaultInvalidationFlowPK()` which prefers the `default-provider-invalidation-flow` slug.

Removed defaults:

- The built-in **groups scope mapping** (`scope_name: groups`) no longer ships in Authentik 2024.x+. `GetGroupsScopeMappingPK()` creates it automatically when absent:
  ```python
  # scope_name: groups
  return list(request.user.ak_groups.values_list("name", flat=True))
  ```

---

## Vault OIDC Topology

### What gets provisioned in Authentik

| Object | Value |
|---|---|
| Group | `admin` |
| Group | `user-ro` |
| User | `alice / password` → group `admin` |
| User | `bob / password` → group `user-ro` |
| Scope mapping | `hal: OIDC groups scope` (created if absent) |
| OAuth2 provider | `vault-oidc-provider` (confidential, implicit-consent flow, `sub_mode: user_username`) |
| Application | slug `hashicorp-vault`, `meta_launch_url: http://vault.localhost:8200/ui/vault/auth/oidc` |

### What gets provisioned in Vault

| Object | Value |
|---|---|
| KV-V2 mount | `kv-oidc` |
| Demo secret | `kv-oidc/data/team1` |
| Policy | `admin` (all paths) |
| Policy | `user-ro` (kv-oidc/team1 read) |
| OIDC auth method | `oidc/` mount |
| OIDC role | `default` (groups claim, allowed redirect URIs) |
| External group | `admin` + alias bound to OIDC accessor |
| External group | `user-ro` + alias bound to OIDC accessor |

### OIDC redirect URIs

```
http://localhost:8250/oidc/callback        (Vault CLI)
http://127.0.0.1:8250/oidc/callback       (Vault CLI fallback)
http://vault.localhost:8200/ui/vault/auth/oidc/oidc/callback   (Vault UI)
```

### Authorization flow

The OAuth2 provider uses the **implicit-consent** authorization flow so the Authentik popup redirects directly back to Vault without showing an intermediate consent page. The explicit-consent flow caused OAuth state to be orphaned on the second auth attempt ("Expired or missing OAuth state" error).

### Login

```bash
vault login -method=oidc     # CLI — browser opens Authentik login
# or open http://vault.localhost:8200 → select OIDC, leave role blank
```

---

## update Behavior

`hal vault oidc update`:
1. Cleans Vault: disables `oidc` auth, unmounts `kv-oidc`, deletes policies and identity groups.
2. Cleans Authentik: deletes the `hashicorp-vault` application and `vault-oidc-provider` OAuth2 provider by name (no-op if absent).
3. Falls through to the enable path — Authentik stack stays running, `WaitAuthentikTokenReady` always runs, full re-provision with fresh clientID/clientSecret.

---

## Shared Service Registry

Service key: `"authentik-idp"` in `~/.hal/shared-services.json`.

Consumer values registered today:
- `"vault-oidc"` — registered by `hal vault oidc enable`
- `"vault-saml"` — future: `hal tf saml enable`

Authentik stack is torn down on disable only when the consumer list reaches zero.

---

## SCIM — Vault Enterprise (`--scim`)

`hal vault oidc enable --scim` wires Authentik outbound SCIM to Vault. Requires Vault Enterprise (`hal vault create --edition ent`).

### What it does

1. **Activates SCIM flag** in Vault (`sys/activation-flags/enable-scim/activate`) — one-way, permanent.
2. **Writes `scim-client` policy** covering `identity/scim/v2/*` with create/read/update/patch/delete/list.
   - `patch` capability is required (Vault 1.14+) for group membership PATCH requests. Without it members list stays empty.
3. **Creates entity** `scim-client-authentik` — the Vault identity that the SCIM bearer token resolves to.
4. **Creates SCIM client** `authentik-scim` with `alias_mount_accessor = oidc/` so SCIM-provisioned entities and OIDC-logged-in users share the same Vault entity (group policies apply immediately on login).
5. **Mints a bearer token** via token role `authentik-scim` (32-day renewable orphan).
6. **Creates Authentik outbound SCIM provider** `vault-scim-provider` in `compatibility_mode: aws`.
   - AWS mode avoids including `"schemas"` inside PATCH Operation objects. Vault SCIM rejects that with 400/invalidValue, which leaves group membership permanently empty.
7. **Assigns SCIM provider as backchannel** on the `hashicorp-vault` application — required for Authentik to activate outbound sync.

### Group ownership

When `--scim` is used:
- External groups (`admin`, `user-ro`) are **not** pre-created in Vault — Authentik SCIM owns them.
- If a prior non-SCIM `hal vault oidc enable` created them, they are detected and removed before SCIM setup so Authentik can take ownership without 409 conflicts.

### SCIM event behaviour

| Event | Propagated automatically |
|-------|--------------------------|
| User created in Authentik | ✅ Yes (event-driven) |
| Group created in Authentik | ✅ Yes (event-driven) |
| User added to / removed from group | ✅ Yes — Authentik 2026.2.3+ fires outbound SCIM events for group membership changes |

> **Note:** Users that existed in Authentik *before* the SCIM provider was first configured are not retroactively pushed by event. `hal vault oidc enable --scim` (and `hal vault oidc update --scim`) call `syncSCIMObjects` at the end to push all users and groups explicitly — so the initial state is always consistent.

### Drift recovery

If Vault is re-created while Authentik stays running (SCIM groups/users still in Authentik, Vault is empty):

```bash
hal vault oidc update    # tears down and re-provisions everything including SCIM
```

This re-activates the SCIM flag, recreates the SCIM client + provider, and triggers fresh pushes for all objects.

---

## TFE SAML + SCIM — Future Track (`feature/tf-saml`)

`hal tf saml enable [--scim]`:
- Shares the same Authentik stack via `"authentik-idp"` shared service key.
- SAML provider in Authentik targeting TFE SAML endpoint.
- Optional SCIM to `https://tfe.localhost/api/scim/v2/`.
- Separate branch — do not implement on `feature/vault-scim`.

---

## File Locations

| File | Role |
|---|---|
| `cmd/vault/oidc.go` | `hal vault oidc` command — enable/disable/update/status flows |
| `internal/integrations/authentik.go` | Shared Authentik stack lifecycle + REST API client |
| `internal/global/shared_services.go` | Shared service consumer tracking |
| `~/.hal/authentik/env` | Generated secrets (mode 0600) |
| `~/.hal/shared-services.json` | Consumer registry |
