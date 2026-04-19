# HAL Terraform CLI Command Spec

## Command
- `hal terraform cli`

## Purpose
Build and run the ephemeral Terraform/TFX helper shell for local TFE workflows.

## Core Lifecycle Actions
- `enable`: build or refresh helper image
- `--console`, `-c`: start helper container and open shell
- `disable`: remove helper container and HAL-managed scenario workspaces
- `update`: reconcile helper image/container state

## Deprecated
- Older HAL docs may reference `hal tf cli disable --force` or other `--force` helper flows. The force flag has been removed from the CLI.
- Use `hal tf cli update` for rebuild/recreate flows.
- Use `hal tf cli disable --update` for the non-interactive destructive confirmation path.

## Behavior
- Ensures helper image/container lifecycle for local TFE usage.
- Bootstraps helper auth context for Terraform and TFX.
- Ensures default scenario projects/workspaces in TFE (`Dave`, `Frank`, and the `hal-*` workspace set) during console bootstrap.
- Supports status view when run with no lifecycle action.

## Detailed Reference
- [../terraform-cli-container-spec.md](../terraform-cli-container-spec.md)

## Related
- Parent namespace: [terraform.md](terraform.md)
- Workspace flow: [terraform-workspace.md](terraform-workspace.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal terraform cli --help`:
```text
--banner                      Print helper welcome banner without opening a shell
--base-image string           Base image used to build the helper image (default "ghcr.io/straubt1/tfx:latest")
-c, --console                     Start helper container and open an interactive shell
-h, --help                        help for cli
--local-directory string      Optional host directory to mount into the helper at /workspaces
--tfe-admin-email string      Terraform Enterprise admin email used for helper token bootstrap (default "haladmin@localhost")
--tfe-admin-password string   Terraform Enterprise admin password used for helper token bootstrap (default "hal9000FTW")
--tfe-admin-username string   Terraform Enterprise admin username used for helper token bootstrap (default "haladmin")
--tfe-org string              Default Terraform Enterprise organization written to ~/.tfx.hcl (default "hal")
--tfe-project string          Optional Terraform Enterprise project to ensure during helper token bootstrap
--tfe-url string              Terraform Enterprise URL used for helper auth bootstrap (default "https://tfe.localhost:8443")
-u, --update                      Reconcile helper image/container and refresh runtime configuration
--verbose                     Show raw Docker build logs instead of HAL build animation
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal terraform cli
```
