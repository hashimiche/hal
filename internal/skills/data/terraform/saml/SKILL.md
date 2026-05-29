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
- `hal-authentik-saml-proxy` — nginx:alpine port-rewrite proxy at `9102` (SAML-specific, started by `hal tf saml enable`)

**Authentik objects provisioned via REST API**:
- Groups: `admins`, `devs`
- Users: `alice / password` (admins), `bob / password` (devs)
- SAML property mapping `hal: SAML Username` → attribute `Username` (matches TFE `attr_username` default)
- SAML property mapping `hal: SAML Groups` → attribute `MemberOf` (matches TFE `attr_groups` default)
- SAML provider: `tfe-saml-provider` (implicit-consent flow, emailAddress NameID, signing key from Authentik)
- Application slug: `tfe-saml` (tile launch URL: `https://tfe.localhost:8443`)

**TFE SAML settings** (via `PATCH /api/v2/admin/saml-settings`):
- `enabled: true`, `attr_username: "Username"`, `attr_groups: "MemberOf"`
- `sso_endpoint_url`: Authentik SSO redirect-binding URL rewritten to port 9102 (via SAML proxy)
- `idp_cert`: Authentik X509 certificate (from metadata XML)
- `slo_endpoint_url`: Authentik SLO post-binding URL
- `provider_type: "saml"` — required so TFE treats Authentik as a recognized IdP; without this, `PATCH /api/v2/admin/scim-settings` returns 422 with "SCIM provisioning requires a recognized identity provider"

**TFE teams** (created in the target org, default `hal-org`):
- `admins` — org-level manage-workspaces/projects/modules/providers
- `devs` — org-level read-workspaces/projects

TFE only adds SSO users to *existing* teams whose names match `MemberOf`. It does not auto-create teams from SAML group attributes. `provisionTFESAMLTeams` handles this as step 7b.

**With `--scim`** (demo/experimental — see limitation note below):
- Deletes TFE teams `admins`/`devs` if manually pre-created by a prior non-SCIM enable (`cleanPreSCIMTFETeams`). Lets SCIM create them fresh so provisioning is visible.
- Skips `provisionTFESAMLTeams` — SCIM owns team creation.
- Enables TFE SCIM via `PATCH /api/v2/admin/scim-settings` with `enabled: true` (TFE 2.0+ only; SAML must be on first, `provider_type` must not be `"unknown"`). 404 silently skipped for TFE 1.x. 422 treated as non-fatal.
- Creates TFE SCIM token via `POST /api/v2/admin/scim-tokens` (site-admin scope, instance-level groups).
- Authentik outbound SCIM provider `tfe-scim-provider` targeting `https://hal-tfe-proxy:8443/scim/v2`. Critical fields: `verify_certificates: false`, `compatibility_mode: "default"`.
- SCIM provider assigned as backchannel on `tfe-saml` application.
- Initial sync via `authentikInitialSCIMSync`: (1) sync all users via Authentik per-object API (`authentik.core.models.User`), (2) sync all groups pass 1 (`authentik.core.models.Group`), (3) create `scim-group-mapping` for each group (TFE 2.0 two-stage model — links instance-level SCIM group to org team), (4) sync all groups pass 2 to trigger TFE team membership reconciliation.
- Re-sync manually with: `hal tf saml update --scim --sync`

**⚠️ TFE SCIM limitation**: TFE Admin UI explicitly states SCIM is only compatible with **Microsoft Entra ID** and **Okta**. Authentik is not in the supported IdP list. This means automatic team-membership reconciliation is partially broken (users appear in TFE SCIM at instance level but org-team linking via `scim-group-mapping` may not fully reconcile). The initial seed works as a demo. For reliable org access, the SAML-only path (`attr_groups: MemberOf`) is the documented and supported approach.

## Execution Order (`hal tf saml enable`)

1. Load or generate Authentik secrets from `~/.hal/authentik/env`.
2. Start Authentik stack if needed: PostgreSQL → server → worker.
3. Wait for Authentik API health and bootstrap token readiness.
3b. Start `hal-authentik-saml-proxy` (nginx:alpine, port 9102). Writes config to `~/.hal/authentik-saml-proxy.conf`. Rewrites Authentik's JSON flow-executor ACS URL from portless `https://tfe.localhost/users/saml/auth` to `https://tfe.localhost:8443/users/saml/auth` so the browser can POST the assertion without needing host port 443.
4. On first boot, wait for standard scope mappings (openid/profile/email).
5. Provision Authentik objects: groups, users, SAML property mappings, SAML provider, application.
6. Fetch SAML metadata from Authentik and parse SSO URL + X509 cert. Rewrite SSO URL port 9100→9102 so TFE routes the auth redirect through the proxy.
7. Bootstrap TFE admin token via `ensureTFEFoundation`.
8. Configure TFE SAML settings via Admin API (`PATCH /api/v2/admin/saml-settings`).
7b. Create `admins` and `devs` teams in the target org (`hal-org`) if they don't exist. **Skipped when `--scim` is active** — SCIM owns team creation.
9. (`--scim`) `cleanPreSCIMTFETeams`: delete any manually pre-created `admins`/`devs` teams so SCIM creates them fresh. Create TFE SCIM token, configure Authentik outbound SCIM provider, assign backchannel, run initial sync via `authentikInitialSCIMSync` (4-step: sync users → sync groups pass 1 → create `scim-group-mapping` per group → sync groups pass 2 for reconciliation).

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
4. **Teams not visible after SSO login**: TFE adds SSO users to *existing* teams. `provisionTFESAMLTeams` pre-creates `admins` and `devs` in `hal-org` during `saml enable`. If a team is still missing, check the org name matches `--tfe-org` and re-run `hal tf saml update`.
5. **SCIM SSL errors from Authentik**: The TFE proxy uses a self-signed cert. The Authentik SCIM provider is created with `verify_certificates: false` (the correct Authentik API field name; `verify_ssl` is wrong and silently ignored, leaving verification enabled). If the provider still shows SSL errors, verify the field is `False` via `GET /api/v3/providers/scim/{pk}/`.
5b. **SCIM sync shows "done" but TFE has no users/groups**: Check (a) `verify_certificates` is `False` in the SCIM provider — if `True`, Authentik worker can't reach TFE (self-signed cert). (b) `compatibility_mode` is `"default"` — `"aws"` sends a different SCIM schema that TFE may not handle. (c) `provider-type` in TFE SAML settings is not `"unknown"` — without a recognized IdP type, `PATCH /api/v2/admin/scim-settings` 422s even though it says "already enabled", and SCIM stays disabled. (d) TFE only officially supports SCIM with Entra ID and Okta — Authentik is unsupported, so team-membership reconciliation after `scim-group-mapping` may not fully sync.
9. **New Authentik group not appearing in TFE org**: Authentik's event-driven SCIM pushes new objects to TFE automatically (check Authentik system task log). However, even if the SCIM group lands in TFE, it won't become an org team until `hal tf saml update --sync --scim` is run — the `scim-group-mapping` admin API step can only be done by hal, Authentik has no knowledge of it.
6. **ACS URL / port 443 error**: TFE's ACS URL is portless (`https://tfe.localhost/users/saml/auth`, port 443). `hal-authentik-saml-proxy` rewrites this in Authentik's JSON flow response to port 8443 before the browser sees it. If SSO fails with a connection error on port 443, check the proxy is running (`podman ps | grep saml-proxy`) and its nginx config has `sub_filter_types *`.
7. **`PEM_read_bio_X509` error in TFE**: TFE tries to parse both `idp_cert` and `old_idp_cert`. If `old_idp_cert` is a malformed empty PEM (from a previous failed provision), TFE crashes. Fix: run `hal tf saml update` — it calls `clearOldTFESAMLCert` which NULLs `old_idp_cert_encrypted` in the TFE DB before re-provisioning.
8. **Shared Authentik stack conflict with Vault OIDC**: If `hal vault oidc` is also running, Authentik is already up. `hal tf saml enable` detects this and skips the stack start. The demo users `alice` and `bob` are shared — they will have both `admin`/`user-ro` (from OIDC) and `admins`/`devs` (from SAML) groups. This is expected.
