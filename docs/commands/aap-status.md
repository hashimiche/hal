# HAL AAP Status Command Spec

## Command
- `hal aap status`

## Purpose
Show AAP runtime status for the local container deployment.

## Related
- Parent namespace: [aap.md](aap.md)

## Prerequisites
- HAL CLI is available in your local environment.

## Flags
- Command flags from `hal aap status --help`:
```text
-h, --help   help for status
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- Read-only local runtime checks.

## Example
```bash
hal aap status
```
