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
- Deprecated: older HAL docs may reference `hal vault audit --force --loki`. That flag has been removed from the CLI. Use `hal vault audit update --loki`.
- Command flags from `hal vault audit --help`:
```text
-h, --help          help for audit
--loki          Auto-configure the shared volume integration for Promtail/Loki
-p, --path string   Path to mount the audit device (e.g., file/) (default "file")
-t, --type string   Type of audit device (file, socket, syslog) (default "file")
-u, --update        Reconcile the audit configuration (disable then enable)
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal vault audit enable --loki
```
