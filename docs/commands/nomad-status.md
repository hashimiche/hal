# HAL Nomad Status Command Spec

## Command
- `hal nomad status`

## Purpose
Show health and status of the local Nomad deployment.

## Behavior
- Default when running `hal nomad` with no subcommand.

## Related
- Parent namespace: [nomad.md](nomad.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal nomad status --help`:
```text
-h, --help   help for status
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal nomad status
```
