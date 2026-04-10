# HAL Observability Destroy Command Spec

## Command
- `hal obs destroy`

## Purpose
Destroy the observability stack and local observability state.

## Related
- Parent namespace: [observability.md](observability.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal obs destroy --help`:
```text
-h, --help   help for destroy
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal obs destroy
```
