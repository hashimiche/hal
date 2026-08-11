# HAL Vault OIDC Command Spec

## Command
- `hal vault oidc`

## Purpose
Deploy Authentik as a shared Identity Provider and configure Vault OIDC authentication.
With `--scim` (Vault Enterprise only), also wire Authentik outbound SCIM to provision users and groups into Vault automatically.

## Related
- Parent namespace: [vault.md](vault.md)
- Architecture: [../scim-idp-spec.md](../scim-idp-spec.md)

## Prerequisites
- HAL CLI is available in your local environment.
- Vault must be running and healthy (`hal vault create`).
- For `--scim`: Vault Enterprise image required (`hal vault create --edition ent`).

## Flags
```text
-h, --help                        help for oidc
    --authentik-image string      Authentik container image (default "ghcr.io/goauthentik/server")
    --authentik-tag string        Authentik image tag (default "2026.5.6")
    --scim                        [Vault Enterprise] Also configure SCIM provisioning from Authentik
-u, --update                      Re-provision Vault OIDC providers (keeps Authentik running)
-e, --enable                      Start Authentik and configure Vault OIDC
-d, --disable                     Remove Vault OIDC and tear down Authentik if unused
```
- Global flags: `--debug`, `--dry-run`, `--verbose`

## Lifecycle Actions

| Action | Command | Description |
|--------|---------|-------------|
| status | `hal vault oidc` | Show Authentik stack health + Vault OIDC mount state (default) |
| enable | `hal vault oidc enable` | Start Authentik, provision demo users/groups, configure Vault OIDC |
| update | `hal vault oidc update` | Re-provision Vault OIDC with fresh credentials (Authentik stays running) |
| disable | `hal vault oidc disable` | Remove Vault OIDC; stop Authentik only if no other product uses it |

## What Gets Deployed

**Authentik containers** (all on `hal-net`):
- `hal-authentik-pg` — PostgreSQL database
- `hal-authentik-server` — API + UI at `http://authentik.localhost:9100`
- `hal-authentik-worker` — Celery background tasks

**Authentik objects**:
- Groups: `admin`, `user-ro`
- Users: `alice / password` (admin), `bob / password` (user-ro)
- OAuth2 provider: `vault-oidc-provider` (implicit-consent flow, groups scope)
- Application slug: `hashicorp-vault` (tile launch URL: `http://vault.localhost:8200/ui/vault/auth/oidc`)

**Vault objects (OIDC only)**:
- OIDC auth mount: `oidc/`
- KV-V2 mount: `kv-oidc/`
- Policies: `admin` (all paths), `user-ro` (kv-oidc/team1 read)
- External identity groups `admin` and `user-ro` with OIDC accessor aliases

**Additional Vault objects with `--scim` (Enterprise)**:
- SCIM feature flag activated (one-way, permanent)
- Policy `scim-client` (`identity/scim/v2/*` — create/read/update/patch/delete/list)
- Entity `scim-client-authentik` with alias on token mount
- Token role `authentik-scim` (32-day renewable, orphan)
- SCIM client `authentik-scim` with `alias_mount_accessor = oidc/`
- Authentik: outbound SCIM provider `vault-scim-provider` (AWS compatibility mode) targeting `http://hal-vault:8200/v1/identity/scim/v2`
- Authentik: SCIM provider assigned as backchannel on `hashicorp-vault` application
- External groups (`admin`, `user-ro`) are **not** pre-created — Authentik SCIM owns group creation

## SCIM Behaviour

| Event | Propagated automatically |
|-------|--------------------------|
| User created in Authentik | ✅ Yes |
| Group created in Authentik | ✅ Yes |
| User added to / removed from group | ✅ Yes — Authentik 2026.2.3+ |

> Users that existed *before* the SCIM provider was first configured are not retroactively pushed by event. `hal vault oidc enable --scim` and `hal vault oidc update --scim` run a full `syncSCIMObjects` pass (all users + groups) at the end to ensure initial consistency.

## Side Effects
- Creates/removes containers `hal-authentik-pg`, `hal-authentik-server`, `hal-authentik-worker`.
- Registers/deregisters `vault-oidc` in `~/.hal/shared-services.json` under key `authentik-idp`.
- Authentik secrets are generated once and persisted at `~/.hal/authentik/env` (mode 0600).
- Authentik stack is only torn down when no other product is registered as a consumer.

## Examples
```bash
# First-time setup
hal vault oidc enable

# First-time setup with SCIM (Vault Enterprise)
export VAULT_LICENSE='...'
hal vault create --edition ent
hal vault oidc enable --scim

# Re-provision after Vault restart (keeps Authentik running)
hal vault oidc update

# Check current state
hal vault oidc

# Login with OIDC (browser opens Authentik login)
vault login -method=oidc
```

