# HAL Boundary Status Command Spec

## Command
- `hal boundary status`

## Purpose
Show health and connectivity status for Boundary and targets.

## Behavior
- Default when running `hal boundary` with no subcommand.

## Related
- Parent namespace: [boundary.md](boundary.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal boundary status --help`:
```text
-h, --help   help for status
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal boundary status
```
