# HAL Consul Destroy Command Spec

## Command
- `hal consul destroy`

## Purpose
Destroy the local Consul server deployment.

## Related
- Parent namespace: [consul.md](consul.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal consul destroy --help`:
```text
-h, --help   help for destroy
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal consul destroy
```
