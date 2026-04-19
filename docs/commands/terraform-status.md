# HAL Terraform Status Command Spec

## Command
- `hal terraform status`
- `hal terraform status --target twin`
- `hal terraform status --target both`

## Purpose
Display health and readiness of the local Terraform Enterprise deployment.

## Behavior
- Reports key stack component status and endpoint readiness.
- Used as default behavior when running `hal terraform` with no subcommand.
- Uses `--target` to show primary, twin, or combined status (`primary` by default).

## Related
- Parent namespace: [terraform.md](terraform.md)
- Deploy: [terraform-deploy.md](terraform-deploy.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal terraform status --help`:
```text
-h, --help          help for status
-t, --target string Terraform scope to act on: primary, twin, or both (default "primary")
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal terraform status
hal terraform status --target twin
hal terraform status --target both
```
