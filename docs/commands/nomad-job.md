# HAL Nomad Job Command Spec

## Command
- `hal nomad job`

## Purpose
Submit sample jobs/workloads to the local Nomad deployment.

## Related
- Parent namespace: [nomad.md](nomad.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal nomad job --help`:
```text
-h, --help   help for job
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal nomad job
```
