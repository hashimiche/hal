# HAL Vault Destroy Command Spec

## Command
- `hal vault destroy`

## Purpose
Destroy local Vault instance and associated integration resources.

## Related
- Parent namespace: [vault.md](vault.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal vault destroy --help`:
```text
-h, --help   help for destroy
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal vault destroy
```
