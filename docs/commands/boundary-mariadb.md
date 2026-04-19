# HAL Boundary MariaDB Command Spec

## Command
- `hal boundary mariadb`

## Purpose
Deploy a MariaDB database target for Boundary labs.

## Related
- Parent namespace: [boundary.md](boundary.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Deprecated: older HAL docs may reference `hal boundary mariadb --force` or `hal boundary mariadb enable --with-vault --force`. Those forms have been removed from the CLI. Use `update` instead.
- Command flags from `hal boundary mariadb --help`:
```text
-h, --help                     help for mariadb
--mariadb-version string   Version (default "11.4")
-u, --update                   Reconcile MariaDB target and Boundary target configuration
--with-vault               Link with Vault Dynamic Creds
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal boundary mariadb enable
```
