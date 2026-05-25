# HAL Terraform SAML Command Spec

## Command
- `hal tf saml`
- Aliases: `hal terraform saml`

## Purpose
Deploy Authentik as a shared Identity Provider and configure TFE SAML SSO.
With `--scim`, also wire Authentik outbound SCIM to provision users and teams into TFE automatically.

## Related
- Parent namespace: [terraform.md](terraform.md)
- Architecture: [../scim-idp-spec.md](../scim-idp-spec.md)

## Prerequisites
- HAL CLI is available in your local environment.
- TFE must be running and healthy (`hal tf create`).
- For `--target twin`: twin TFE must be running (`hal tf create --target twin`).

## Flags
```text
-h, --help                        help for saml
    --authentik-image string      Authentik container image (default "ghcr.io/goauthentik/server")
    --authentik-tag string        Authentik image tag (default "2026.2.3")
    --scim                        Also configure SCIM provisioning from Authentik to TFE
    --sync                        With --scim: re-push all group membership without full re-provision
-t, --target string               TFE instance scope: primary or twin (default "primary")
    --tfe-url string              TFE base URL (default: https://tfe.localhost:8443 for primary)
    --tfe-org string              TFE organization name (default "hal-org")
    --tfe-token string            TFE admin API token (auto-bootstrapped if omitted)
    --tfe-admin-username string   TFE admin username (default "haladmin")
    --tfe-admin-email string      TFE admin email (default "haladmin@localhost")
    --tfe-admin-password string   TFE admin password (default "hal9000FTW")
-u, --update                      Re-provision TFE SAML providers (keeps Authentik running)
-e, --enable                      Start Authentik and configure TFE SAML SSO
-d, --disable                     Remove TFE SAML and tear down Authentik if unused
```
- Global flags: `--debug`, `--dry-run`

## Lifecycle Actions

| Action | Command | Description |
|--------|---------|-------------|
| status | `hal tf saml` | Show Authentik stack health + TFE SAML state (default) |
| enable | `hal tf saml enable` | Start Authentik, provision demo users/groups, configure TFE SAML |
| update | `hal tf saml update` | Re-provision TFE SAML with fresh credentials (Authentik stays running) |
| disable | `hal tf saml disable` | Remove TFE SAML; stop Authentik only if no other product uses it |

## What Gets Deployed

**Authentik containers** (all on `hal-net`, shared with Vault OIDC if running):
- `hal-authentik-pg` — PostgreSQL database
- `hal-authentik-server` — API + UI at `http://authentik.localhost:9100`
- `hal-authentik-worker` — Celery background tasks

**Authentik objects** (primary target):
- Groups: `admins`, `devs`
- Users: `alice / password` (admins), `bob / password` (devs)
- SAML property mappings: `hal: SAML Username` (attr: `Username`), `hal: SAML Groups` (attr: `MemberOf`)
- SAML provider: `tfe-saml-provider` (implicit-consent flow, email NameID)
- Application slug: `tfe-saml` (launch URL: `https://tfe.localhost:8443`)

**TFE SAML settings** (via Admin API `PATCH /api/v2/admin/saml-settings`):
- SAML enabled with Authentik as IdP
- `attr_username: "Username"`, `attr_groups: "MemberOf"`
- SSO endpoint and IdP certificate parsed from Authentik SAML metadata

**With `--scim`**:
- TFE SCIM token created via `POST /api/v2/organizations/:org/scim-tokens`
- Authentik outbound SCIM provider `tfe-scim-provider` targeting `https://hal-tfe-proxy:8443/api/scim/v2`
- SCIM provider assigned as backchannel on `tfe-saml` application
- Initial users+groups sync run immediately

## SCIM Behaviour

| Event | Propagated automatically |
|-------|--------------------------|
| User created in Authentik | ✅ Yes |
| Group created in Authentik | ✅ Yes |
| User added to / removed from group | ✅ Yes — Authentik 2026.2.3+ |

TFE auto-creates teams from SCIM group names. No manual team setup needed when `--scim` is active.

## Side Effects
- Creates/removes containers `hal-authentik-pg`, `hal-authentik-server`, `hal-authentik-worker`.
- Registers/deregisters `"tfe-saml"` (or `"tfe-bis-saml"` for twin) as a consumer of `"authentik-idp"`.
- Authentik stack is stopped only when all consumers deregister (shared with Vault OIDC if running).
- Modifies TFE global SAML settings (Admin API) — affects all TFE users on that instance.

## Twin Target

```bash
hal tf saml enable --target twin
hal tf saml enable --target twin --scim
hal tf saml disable --target twin
```

Twin uses `https://tfe-bis.localhost:9443`, Authentik app slug `tfe-bis-saml`, and registers consumer key `"tfe-bis-saml"`.

## Example Usage

```bash
# Enable SAML SSO
hal tf saml enable

# Enable SAML + SCIM provisioning
hal tf saml enable --scim

# Re-provision after changing Authentik config
hal tf saml update

# Force re-sync SCIM objects without full re-provision
hal tf saml update --scim --sync

# Check status
hal tf saml

# Disable
hal tf saml disable
```
