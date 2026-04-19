# HAL Terraform Destroy Command Spec

## Command
- `hal terraform delete`
- `hal terraform delete --target twin`
- `hal terraform delete --target both`

## Purpose
Tear down the local Terraform Enterprise stack and related local state for a clean restart.

## Behavior
- Stops/removes TFE stack resources managed by HAL.
- Removes Terraform API helper containers/images (including legacy CLI helper artifacts) as part of stack teardown.
- Resets local deployment state used by the Terraform namespace.
- Uses `--target` to remove primary, twin, or both scopes (`primary` by default).
- Smart guard: `--target primary` preserves shared backend containers when the twin TFE core is still running.

## Related
- Parent namespace: [terraform.md](terraform.md)
- Deploy: [terraform-deploy.md](terraform-deploy.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal terraform delete --help`:
```text
-h, --help          help for delete
-t, --target string Terraform scope to act on: primary, twin, or both (default "primary")
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal terraform delete
hal terraform delete --target twin
hal terraform delete --target both
```
