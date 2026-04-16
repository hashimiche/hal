---
name: jwt
description: Deploy, verify, and troubleshoot the Vault JWT auth lab in hal. Use this skill when the user asks to enable JWT auth, configure CI/CD authentication with Vault, debug bound claims, test GitLab or pipeline logins, or reset the JWT demo. Triggers include "enable jwt", "configure jwt", "gitlab vault auth", "pipeline auth", "bound claims", and "hal vault jwt".
---

# Hal Vault JWT Configurator

This skill covers the local GitLab-backed JWT auth demo implemented by `hal vault jwt`.

## Lab Assumptions

- Vault runs at `http://127.0.0.1:8200`
- Root token defaults to `root`
- The JWT demo deploys GitLab CE and a runner, then configures Vault JWT auth
- Prefer `hal` for lifecycle actions and `vault read/write` for post-deploy inspection

## What The Command Actually Sets Up

- GitLab container: `hal-gitlab`
- GitLab runner: `hal-gitlab-runner`
- Vault auth mount: `jwt/`
- KV engine: `kv-jwt/`
- Policy: `cicd-read`
- Role: `auth/jwt/role/cicd-role`
- Bound issuer: `http://gitlab.localhost:8080`
- Bound claims: `project_path=root/secret-zero` and `ref=v*`
- Bound claims type: `glob` (tag-based guardrail)

## Workflow

### Step 1: Choose the lifecycle action

Use smart status mode if needed:

    hal vault jwt

Then use the correct lifecycle command:

    hal vault jwt --enable
    hal vault jwt --force
    hal vault jwt --disable

### Step 2: Verify the resulting Vault config

If Vault MCP is available, inspect:

1. `auth/jwt/config`
2. `auth/jwt/role/cicd-role`
3. `sys/auth`
4. `sys/mounts`

If MCP is unavailable, use:

    export VAULT_ADDR='http://127.0.0.1:8200'
    export VAULT_TOKEN='root'

    vault read auth/jwt/config
    vault read auth/jwt/role/cicd-role
    vault list auth/jwt/role

### Step 3: Present structured results

Synthesize the output from the `hal` CLI and the Vault MCP into a clean, markdown-formatted response.

**Tier 1 — Success Summary**
Provide a brief confirmation that the JWT auth method is enabled and configured.

**Tier 2 — Configuration Details Table**

| Component | Value | Description |
|-----------|-------|-------------|
| Auth Path | `auth/jwt/` | The mount point for the JWT auth method |
| Bound Issuer | `http://gitlab.localhost:8080` | The trusted GitLab issuer |
| JWKS URL | `http://gitlab.localhost:8080/oauth/discovery/keys` | Public key source |
| Role | `cicd-role` | The role used by the GitLab pipeline |
| Policy | `cicd-read` | Read access to the demo secret |
| Bound Claims | `project_path=root/secret-zero`, `ref=v*` | Restricts access to this repository and protected tag pattern |

**Tier 3 — Actionable Testing Commands**

    export VAULT_ADDR='http://127.0.0.1:8200'
    export VAULT_TOKEN='root'

    vault read auth/jwt/config
    vault read auth/jwt/role/cicd-role

## Handling Edge Cases

1. **Vault is not running:** Instruct the user to run `hal vault deploy` first.
2. **GitLab is not ready yet:** Tell the user the GitLab boot sequence can take several minutes.
3. **Missing or mismatched claims:** Point the user to `vault read auth/jwt/role/cicd-role` and compare `bound_claims`, `bound_claims_type`, and `bound_audiences`.
4. **Tag/branch mismatch in CI:** Explain that the default lab policy is tag-focused (`ref=v*`) and suggest explicit role updates if branch-based auth is desired.
5. **User wants to modify the configured role after deployment:** Provide exact `vault write auth/jwt/role/...` commands rather than suggesting Go code edits.