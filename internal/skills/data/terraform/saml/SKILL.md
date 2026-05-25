---
name: saml
description: Deploy, verify, and troubleshoot the TFE SAML lab in hal. Use this skill when the user asks to enable SAML SSO for TFE, set up TFE single sign-on, debug SAML redirects or attribute mapping, configure SCIM provisioning for TFE teams, or reset the TFE SSO demo. Triggers include "enable saml", "TFE SSO", "TFE SAML", "Authentik TFE", "SAML callback", "configure saml", "hal tf saml", "tfe scim", "tfe teams scim", and "saml sso tfe".
---

# HAL TFE SAML Configurator

This skill covers the Authentik-backed SAML SSO demo implemented by `hal tf saml`.

## Lab Assumptions

- TFE runs locally at `https://tfe.localhost:8443` (primary) or `https://tfe-bis.localhost:9443` (twin)
- TFE admin credentials default to `haladmin / hal9000FTW`
- Authentik is exposed locally on port `9100` at `http://authentik.localhost:9100`
- Prefer `hal` for lifecycle actions and TFE Admin API for post-deploy inspection

## What The Command Actually Sets Up

**Authentik containers** (all on `hal-net`, shared with Vault OIDC if running):
- `hal-authentik-pg` — PostgreSQL database
- `hal-authentik-server` — Authentik API + UI (HTTP `9100`, HTTPS `9143`)
- `hal-authentik-worker` — Celery background tasks

**Authentik objects provisioned via REST API**:
- Groups: `admins`, `devs`
- Users: `alice / password` (admins), `bob / password` (devs)
- SAML property mapping `hal: SAML Username` → attribute `Username` (matches TFE `attr_username` default)
- SAML property mapping `hal: SAML Groups` → attribute `MemberOf` (matches TFE `attr_groups` default)
- SAML provider: `tfe-saml-provider` (implicit-consent flow, emailAddress NameID, signing key from Authentik)
- Application slug: `tfe-saml` (tile launch URL: `https://tfe.localhost:8443`)

**TFE SAML settings** (via `PATCH /api/v2/admin/saml-settings`):
- `enabled: true`, `attr_username: "Username"`, `attr_groups: "MemberOf"`
- `sso_endpoint_url`: Authentik SSO redirect-binding URL (from metadata XML)
- `idp_cert`: Authentik X509 certificate (from metadata XML)
- `slo_endpoint_url`: Authentik SLO post-binding URL

**With `--scim`**:
- TFE org-scoped SCIM token via `POST /api/v2/organizations/hal-org/scim-tokens`
- Authentik outbound SCIM provider `tfe-scim-provider` targeting `https://hal-tfe-proxy:8443/api/scim/v2` (`verify_ssl: false`)
- SCIM provider assigned as backchannel on `tfe-saml` application
- Initial users+groups sync via `syncTFESCIMObjects`

## Execution Order (`hal tf saml enable --scim`)

1. Load or generate Authentik secrets from `~/.hal/authentik/env`.
2. Start Authentik stack if needed: PostgreSQL → server → worker.
3. Wait for Authentik API health and bootstrap token readiness.
4. On first boot, wait for standard scope mappings (openid/profile/email).
5. Provision Authentik objects: groups, users, SAML property mappings, SAML provider, application.
6. Fetch SAML metadata from Authentik and parse SSO URL + X509 cert.
7. Bootstrap TFE admin token via `ensureTFEFoundation`.
8. Configure TFE SAML settings via Admin API (`PATCH /api/v2/admin/saml-settings`).
9. (--scim) Create TFE SCIM token, configure Authentik SCIM provider, assign backchannel, run initial sync.

## Workflow

### Step 1: Prerequisites

TFE must be running and healthy:

    hal tf create   # if not already running
    hal tf status

### Step 2: Choose the lifecycle action

    hal tf saml           # status (default)
    hal tf saml enable    # deploy Authentik IdP + TFE SAML
    hal tf saml update    # re-provision (keeps Authentik running)
    hal tf saml disable   # remove TFE SAML

With SCIM provisioning:

    hal tf saml enable --scim

Twin target:

    hal tf saml enable --target twin

### Step 3: Verify TFE SAML

Check TFE Admin Settings → Authentication → SAML:

    open https://tfe.localhost:8444/admin/auth-settings   # TFE admin console

Or via API:

    curl -sk -H "Authorization: Bearer $TFE_TOKEN" \
      https://tfe.localhost:8443/api/v2/admin/saml-settings | jq .data.attributes

### Step 4: Test SSO Login

    open https://tfe.localhost:8443
    # Click "SSO" → redirected to Authentik
    # Log in as: alice / password  OR  bob / password

## Handling Edge Cases

1. **SAML assertion attribute name mismatch**: TFE looks for `Username` (attr_username) and `MemberOf` (attr_groups). Both are set by `hal: SAML Username` and `hal: SAML Groups` Authentik property mappings. If users can log in but teams are wrong, verify the `MemberOf` attribute contains the correct group names.
2. **TFE is offline during enable**: Run `hal tf create` first. The saml command checks that the TFE core container is running before proceeding.
3. **First boot Authentik stuck**: See the Vault OIDC skill for Authentik startup troubleshooting — the same wait logic applies here.
4. **SCIM teams not created in TFE**: TFE creates teams automatically when the SCIM group push succeeds. Check the Authentik task log if `syncTFESCIMObjects` reported warnings. Re-run with `hal tf saml update --scim --sync` to force a fresh push.
5. **SCIM SSL errors from Authentik**: The TFE proxy uses a self-signed cert. The Authentik SCIM provider is created with `verify_ssl: false`. If the provider shows SSL errors, check that the proxy container name (`hal-tfe-proxy`) is reachable from `hal-authentik-server` on `hal-net`.
6. **ACS URL mismatch**: TFE's ACS URL is `https://tfe.localhost:8443/users/saml/auth`. Authentik's provider is configured with this URL. If TFE rejects the SAML assertion with "ACS URL mismatch", verify the Authentik SAML provider's `acs_url` matches the TFE callback URL exactly.
7. **Shared Authentik stack conflict with Vault OIDC**: If `hal vault oidc` is also running, Authentik is already up. `hal tf saml enable` detects this and skips the stack start. The demo users `alice` and `bob` are shared — they will have both `admin`/`user-ro` (from OIDC) and `admins`/`devs` (from SAML) groups. This is expected.
