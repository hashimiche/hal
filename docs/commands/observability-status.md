# HAL Observability Status Command Spec

## Command
- `hal obs status`

## Purpose
Show health and component status for the observability stack.

## Behavior
- Default when running `hal obs` with no subcommand.

## Related
- Parent namespace: [observability.md](observability.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal obs status --help`:
```text
-h, --help   help for status
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal obs status
```
