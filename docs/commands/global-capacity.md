# HAL Capacity Command Spec

## Command
- `hal capacity`

## Purpose
Display local runtime capacity and what-if deployment estimates.

## Related
- Parent: [global.md](global.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal capacity --help`:
```text
--active     Show active heavy deployment details
--deployed   Alias for --active
-h, --help       help for capacity
--pending    Show pending heavy deployment impact estimates
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
