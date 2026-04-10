# HAL Vault Audit Command Spec

## Command
- `hal vault audit`

## Purpose
Manage and inspect Vault audit logging state in local lab environments.

## Related
- Parent namespace: [vault.md](vault.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal vault audit --help`:
```text
-d, --disable       Disable the audit configuration
-e, --enable        Enable the audit configuration
-f, --force         Force a clean reconfiguration (disable then enable)
-h, --help          help for audit
--loki          Auto-configure the shared volume integration for Promtail/Loki
-p, --path string   Path to mount the audit device (e.g., file/) (default "file")
-t, --type string   Type of audit device (file, socket, syslog) (default "file")
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal vault audit
```
