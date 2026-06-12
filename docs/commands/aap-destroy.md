# HAL AAP Destroy Command Spec

## Command
- `hal aap delete`

## Purpose
Delete local AAP runtime resources.

## Related
- Parent namespace: [aap.md](aap.md)

## Prerequisites
- HAL CLI is available in your local environment.

## Flags
- Command flags from `hal aap delete --help`:
```text
-h, --help   help for delete
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- Removes `hal-aap` container.
- Cleans `hal-net` when no remaining containers require it.

## Example
```bash
hal aap delete
```
