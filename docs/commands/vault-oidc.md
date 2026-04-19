# HAL Vault OIDC Command Spec

## Command
- `hal vault oidc`

## Purpose
Deploy Keycloak and configure Vault OIDC authentication flow.

## Related
- Parent namespace: [vault.md](vault.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Deprecated: older HAL docs may reference `hal vault oidc --force`. That flag has been removed from the CLI. Use `hal vault oidc update`.
- Command flags from `hal vault oidc --help`:
```text
-h, --help                      help for oidc
--keycloak-version string   Version of the Keycloak container image to deploy (default "24.0.4")
-u, --update                    Reconcile Keycloak and Vault OIDC integration
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal vault oidc enable
```
