# HAL Consul Status Command Spec

## Command
- `hal consul status`

## Purpose
Show health and readiness of the local Consul deployment.

## Behavior
- Default when running `hal consul` with no subcommand.

## Related
- Parent namespace: [consul.md](consul.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal consul status --help`:
```text
-h, --help   help for status
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal consul status
```
