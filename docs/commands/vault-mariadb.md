# HAL Vault MariaDB Command Spec

## Command
- `hal vault mariadb`

## Purpose
Deploy MariaDB and configure Vault dynamic database credentials workflow.

## Related
- Parent namespace: [vault.md](vault.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal vault mariadb --help`:
```text
-d, --disable                  Remove MariaDB and clean up Vault configurations
-e, --enable                   Deploy MariaDB and configure Vault
-f, --force                    Force a clean redeployment of the database
-h, --help                     help for mariadb
--mariadb-version string   Version of the MariaDB container image to deploy (default "11.4")
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal vault mariadb
```
