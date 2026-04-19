# HAL Terraform API Command Spec

## Command
- `hal terraform api-workflow`
- Alias: `hal terraform api`

## Purpose
Build and run the ephemeral Terraform/TFX API helper shell for local TFE workflows.

## Core Lifecycle Actions
- `enable`: build helper image if missing and open shell
- `disable`: remove helper container and HAL-managed scenario workspaces
- `update`: remove helper container/image, rebuild, and open shell
- `--target`, `-t`: choose `primary` or `twin`

## Deprecated
- Older HAL docs may reference `hal tf cli ...` helper flows. Use `hal tf api-workflow ...` (or alias `hal tf api`) instead.
- Older HAL docs may reference `--force` helper flows. The force flag has been removed from the CLI.
- Use `hal tf api-workflow update` for rebuild/recreate flows.
- Use `hal tf api-workflow disable --auto-approve` for the non-interactive destructive confirmation path.

## Behavior
- Ensures helper image/container lifecycle for local TFE usage.
- Bootstraps helper auth context for Terraform and TFX.
- Ensures default scenario projects/workspaces in TFE (`Dave`, `Frank`, and the `hal-*` workspace set) during console bootstrap.
- Supports status view when run with no lifecycle action, including helper runtime versions (TFX/Terraform/Alpine).

## Detailed Reference
- [../terraform-cli-container-spec.md](../terraform-cli-container-spec.md)

## Related
- Parent namespace: [terraform.md](terraform.md)
- Workspace flow: [terraform-workspace.md](terraform-workspace.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal terraform api-workflow --help`:
```text
	--auto-approve    Skip interactive confirmation for destructive disable operations
-h, --help            help for api-workflow
-t, --target string   Terraform scope to act on: primary or twin (default "primary")
```
- Global flags: `--debug`, `--dry-run`

Advanced helper and twin-tuning flags remain available but are intentionally hidden from default help to keep the command surface concise.

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal terraform api-workflow
```
