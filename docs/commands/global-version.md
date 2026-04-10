# HAL Version Command Spec

## Command
- `hal version`

## Purpose
Print HAL CLI version information.

## Related
- Parent: [global.md](global.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal version --help`:
```text
-h, --help   help for version
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal version
```
