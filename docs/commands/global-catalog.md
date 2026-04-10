# HAL Catalog Command Spec

## Command
- `hal catalog`

## Purpose
List available HAL products/features and suggested command paths.

## Related
- Parent: [global.md](global.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal catalog --help`:
```text
-h, --help   help for catalog
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
