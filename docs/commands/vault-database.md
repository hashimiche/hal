# HAL Vault Database Command Spec

## Command
- `hal vault database`

## Purpose
Deploy MariaDB and configure Vault dynamic database credentials workflow.

## Related
- Parent namespace: [vault.md](vault.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal vault database --help`:
```text
-d, --disable                  Remove selected backend and clean up Vault database configuration
-e, --enable                   Deploy selected database backend and configure Vault
-f, --force                    Force a clean redeployment of the selected backend
-h, --help                     help for database
      --backend string           Database backend to use (mariadb; pgsql planned, postgres alias accepted) (default "mariadb")
      --mariadb-version string   Version of the MariaDB container image to deploy (default "11.4")
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal vault database --enable --backend mariadb
```
