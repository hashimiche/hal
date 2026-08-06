# HAL Terraform Deploy Command Spec

## Command

- `hal terraform create`
- `hal terraform create --target twin`
- `hal terraform create --target both`

## Purpose

Deploy the local Terraform Enterprise (TFE) stack for HAL labs.

## Behavior

- Provisions and starts the TFE stack components used by HAL.
- Prepares local endpoint access for TFE workflows.
- Uses `--target` to select deployment scope (`primary`, `twin`, or `both`).
- Optionally wires the primary TFE issuer to external `hal-vault` through
  Dynamic Provider Credentials.

## Related

- Parent namespace: [terraform.md](terraform.md)
- Status: [terraform-status.md](terraform-status.md)
- Destroy: [terraform-destroy.md](terraform-destroy.md)

## Prerequisites

- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
- **`--vault-enabled` only**: start `hal-vault` first with `hal vault create`.

## Flags

Deprecated: older HAL docs may reference `hal terraform create --force`. That
flag has been removed. Use `hal terraform update` or
`hal terraform create --update`.

Vault- and lifecycle-related flags from `hal terraform create --help` are shown
below. Run the command for the complete image, port, and twin override list.

```text
  -t, --target string       Terraform scope to act on: primary, twin, or both (default "primary")
  -u, --update              Reconcile an existing Terraform Enterprise deployment in place
      --vault-enabled       Wire the running hal-vault container as an external secrets backend
                            for TFE workspace runs (Dynamic Provider Credentials)
      --tfe-org string      Terraform Enterprise organization name to auto-bootstrap during deploy (default "hal")
```

Global flags include `--debug`, `--dry-run`, `--network-subnet`, and `--verbose`.

Observability artifacts are managed explicitly with
`hal terraform obs <create|update|delete|status>`.

## `--vault-enabled`: Dynamic Provider Credentials

After primary TFE is healthy and HAL has an API token, `--vault-enabled`
reconciles the following resources:

| What gets created | Where |
|---|---|
| JWT auth mount `jwt-tfe` | External `hal-vault` container |
| Vault policy `tfe-workspace-policy` | Token self-management plus `secret/data/*` and `secret/metadata/*` read access |
| JWT role `tfe-workspace-role` | Audience-bound to `vault.workload.identity` and scoped to the configured TFE organization |
| Global variable set `hal-vault` | Primary TFE organization; contains the required `TFC_VAULT_*` environment variables |

HAL reads TFE's public well-known document through the host-facing
`https://tfe.localhost:8443` endpoint, then configures the Vault JWT mount with
the exact canonical issuer returned by TFE (normally
`https://tfe.localhost`, reachable from `hal-vault` on internal port 443). HAL
also provides the TFE self-signed CA to Vault.

The TFE variable set contains:

```text
TFC_VAULT_PROVIDER_AUTH=true
TFC_VAULT_ADDR=http://hal-vault:8200
TFC_VAULT_RUN_ROLE=tfe-workspace-role
TFC_VAULT_AUTH_PATH=jwt-tfe
TFC_VAULT_WORKLOAD_IDENTITY_AUDIENCE=vault.workload.identity
```

Vault prod mode changes the address to `https://hal-vault:8200` and adds
`TFC_VAULT_ENCODED_CACERT` containing the Base64-encoded HAL CA certificate.
TFE derives `VAULT_ADDR` in the run environment from `TFC_VAULT_ADDR`.

If `hal-vault` is absent during initial TFE deployment, HAL prints a warning
and leaves TFE running. Start Vault and rerun
`hal terraform create --vault-enabled`; when primary TFE is already running,
HAL applies only the integration wiring.

The integration currently supports the primary issuer:

- `--target primary --vault-enabled`: supported.
- `--target both --vault-enabled`: wires primary, then continues twin deployment.
- `--target twin --vault-enabled`: rejected with an actionable error because a
  distinct twin issuer and JWT mount have not been implemented.

## Side Effects

- This command may create, mutate, or remove local lab resources depending on its operation.
- `--vault-enabled` writes a JWT auth mount, policy, and role in `hal-vault`.
- Its global TFE variable set affects all workspaces in the configured primary organization.

## Example

```bash
# Standard TFE deployment
hal terraform create

# Primary TFE + Vault Dynamic Provider Credentials
hal vault create
hal terraform create --vault-enabled

# Twin lifecycle without Vault issuer wiring
hal terraform create --target twin
hal terraform create --target both
```
