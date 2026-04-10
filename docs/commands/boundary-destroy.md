# HAL Boundary Destroy Command Spec

## Command
- `hal boundary destroy`

## Purpose
Destroy Boundary and associated target resources.

## Related
- Parent namespace: [boundary.md](boundary.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal boundary destroy --help`:
```text
-h, --help   help for destroy
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal boundary destroy
```
