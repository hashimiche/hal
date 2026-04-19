# HAL Terraform Agent Command Spec

## Command
- `hal terraform agent`

## Purpose
Manage custom TFE agent-pool runtime for local workspace runs.

## Core Lifecycle Actions
- `enable`: create or reuse an agent pool, mint token, and start the agent container
- `disable`: remove HAL-managed agent container and revoke HAL-managed token
- `update`: reconcile local agent runtime and rotate token material when needed

## Behavior
- Uses local TFE endpoint wiring and HAL cert material to register agent securely.
- Reuses or creates the configured organization-scoped agent pool.
- Persists minimal local state in `~/.hal/tfe-agent-state.json`.
- Shows status when run without lifecycle action.

## Related
- Parent namespace: [terraform.md](terraform.md)
- Workspace flow: [terraform-workspace.md](terraform-workspace.md)

## Prerequisites
- TFE runtime is deployed and healthy (`hal terraform create`).
- TFE organization exists or can be bootstrapped by HAL.

## Flags
- Deprecated: older HAL docs may reference `hal terraform agent enable --force`. That flag has been removed from the CLI. Use `hal terraform agent update`.
- Command flags from `hal terraform agent --help`:
```text
--agent-name string           Display name advertised by the running agent (default "hal-tfc-agent")
-h, --help                        help for agent
--image string                Docker image used for the custom TFE agent (default "hashicorp/tfc-agent:1.28")
--pool-name string            TFE agent pool name to create or reuse (default "hal-agent-pool")
--tfe-admin-email string      Initial TFE admin email used when bootstrapping via IACT (default "haladmin@localhost")
--tfe-admin-password string   Initial TFE admin password used when bootstrapping via IACT (default "hal9000FTW")
--tfe-admin-username string   Initial TFE admin username used when bootstrapping via IACT (default "haladmin")
--tfe-api-token string        Terraform Enterprise app API token (or set TFE_API_TOKEN)
--tfe-org string              Terraform Enterprise organization name (default "hal")
--tfe-url string              Terraform Enterprise base URL (default "https://tfe.localhost:8443")
-u, --update                      Reconcile existing agent pool/runtime settings
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create or revoke TFE authentication tokens and start/stop local agent runtime containers.

## Example
```bash
hal terraform agent
```
