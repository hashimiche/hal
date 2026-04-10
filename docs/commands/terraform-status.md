# HAL Terraform Status Command Spec

## Command
- `hal terraform status`

## Purpose
Display health and readiness of the local Terraform Enterprise deployment.

## Behavior
- Reports key stack component status and endpoint readiness.
- Used as default behavior when running `hal terraform` with no subcommand.

## Related
- Parent namespace: [terraform.md](terraform.md)
- Deploy: [terraform-deploy.md](terraform-deploy.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal terraform status --help`:
```text
-h, --help   help for status
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal terraform status
```
