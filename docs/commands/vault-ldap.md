# HAL Vault LDAP Command Spec

## Command
- `hal vault ldap`

## Purpose
Deploy OpenLDAP and configure Vault LDAP auth and related secrets integrations.

## Related
- Parent namespace: [vault.md](vault.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal vault ldap --help`:
```text
-f, --force                         Force a clean redeployment of the entire environment
-h, --help                          help for ldap
--openldap-version string       OpenLDAP image tag for the LDAP demo (default "1.5.0")
--phpldapadmin-version string   phpLDAPadmin image tag for the LDAP demo UI (default "0.9.0")
-u, --update                        Reconcile OpenLDAP and Vault LDAP integration
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal vault ldap enable
```
