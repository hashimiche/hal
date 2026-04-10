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
- Command flags from `hal boundary mariadb --help`:
```text
-d, --disable                  Remove MariaDB
-e, --enable                   Deploy MariaDB
-f, --force                    Force Reset
-h, --help                     help for mariadb
--mariadb-version string   Version (default "11.4")
--with-vault               Link with Vault Dynamic Creds
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal boundary mariadb
```
