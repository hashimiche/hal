# HAL Terraform Agent Command Spec

## Command
- `hal terraform agent`

## Purpose
Manage custom TFE agent-pool runtime for local workspace runs.

## Core Lifecycle Actions
- `enable`: create or reuse an agent pool, mint token, and start the agent container
- `disable`: remove HAL-managed agent container and revoke HAL-managed token
- `update`: reconcile local agent runtime and rotate token material when needed
- `--target`, `-t`: choose `primary`, `twin`, or `both`

## Behavior
- Uses local TFE endpoint wiring and HAL cert material to register agent securely.
- Reuses or creates the configured organization-scoped agent pool.
- Persists target-specific local state (`~/.hal/tfe-agent-state.json` for primary, `~/.hal/tfe-agent-bis-state.json` for twin).
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
--auto-approve    Skip interactive confirmation for destructive disable operations
-h, --help            help for agent
-t, --target string   Terraform scope to act on: primary, twin, or both (default "primary")
```
- Global flags: `--debug`, `--dry-run`

Advanced agent tuning flags remain available but are intentionally hidden from default help to keep the command surface concise.

## Side Effects
- This command may create or revoke TFE authentication tokens and start/stop local agent runtime containers.

## Example
```bash
hal terraform agent
```
