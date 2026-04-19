# HAL Boundary SSH Command Spec

## Command
- `hal boundary ssh`

## Purpose
Deploy a Multipass Ubuntu VM as a Boundary SSH target.

## Related
- Parent namespace: [boundary.md](boundary.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Deprecated: older HAL docs may reference `hal boundary ssh --force`. That flag has been removed from the CLI. Use `hal boundary ssh update`.
- Command flags from `hal boundary ssh --help`:
```text
--cpus string           Number of CPUs for the SSH target VM (default "1")
-h, --help                  help for ssh
--mem string            Amount of RAM for the SSH target VM (default "512M")
-u, --update                Reconcile SSH target VM and Boundary target wiring
--ubuntu-image string   Multipass image/channel used for the SSH target VM (default "22.04")
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal boundary ssh enable
```
