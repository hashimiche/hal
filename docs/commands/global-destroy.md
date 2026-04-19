# HAL Global Destroy Command Spec

## Command
- `hal delete`

## Purpose
Destroy all HAL-managed infrastructure globally.

## Safety
- Interactive confirmation by default
- `--force` bypasses prompt
- Supports global `--dry-run`

## Related
- Parent: [global.md](global.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal delete --help`:
```text
-f, --force   Force destruction without confirmation prompt
-h, --help    help for delete
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal delete
```
