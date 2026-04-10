# HAL Daisy Command Spec

## Command
- `hal daisy` (hidden)

## Purpose
Run cinematic global teardown easter egg.

## Behavior
- Hidden command
- Uses global teardown backend

## Related
- Parent: [global.md](global.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.

## Flags
- Command flags from `hal daisy --help`:
```text
-h, --help   help for daisy
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal daisy
```
