# HAL Vault Status Command Spec

## Command
- `hal vault status`

## Purpose
Check deep status of Vault container, API, and ecosystem integrations.

## Behavior
- Default when running `hal vault` with no subcommand.

## Related
- Parent namespace: [vault.md](vault.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal vault status --help`:
```text
-h, --help   help for status
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal vault status
```
