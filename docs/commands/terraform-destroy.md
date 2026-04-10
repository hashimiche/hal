# HAL Terraform Destroy Command Spec

## Command
- `hal terraform destroy`

## Purpose
Tear down the local Terraform Enterprise stack and related local state for a clean restart.

## Behavior
- Stops/removes TFE stack resources managed by HAL.
- Resets local deployment state used by the Terraform namespace.

## Related
- Parent namespace: [terraform.md](terraform.md)
- Deploy: [terraform-deploy.md](terraform-deploy.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal terraform destroy --help`:
```text
-h, --help   help for destroy
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal terraform destroy
```
