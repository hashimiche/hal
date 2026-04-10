# HAL Vault JWT Command Spec

## Command
- `hal vault jwt`

## Purpose
Simulate enterprise Secret Zero CI/CD pipeline auth flow with GitLab JWT.

## Related
- Parent namespace: [vault.md](vault.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal vault jwt --help`:
```text
-d, --disable                 Remove GitLab CE and strip JWT from Vault
-e, --enable                  Deploy GitLab CE and configure Vault JWT
-f, --force                   Force a clean redeployment of the entire environment
--gitlab-version string   Version of the GitLab CE container image to deploy (default "18.10.1-ce.0")
-h, --help                    help for jwt
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal vault jwt
```
