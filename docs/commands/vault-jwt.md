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
- Deprecated: older HAL docs may reference `hal vault jwt --force`. That flag has been removed from the CLI. Use `hal vault jwt update`.
- Command flags from `hal vault jwt --help`:
```text
-u, --update                 Reconcile GitLab and Vault JWT integration settings
--gitlab-image string        GitLab CE container image name (default "gitlab/gitlab-ce")
--gitlab-tag string          GitLab CE container image tag (default "18.10.1-ce.0")
--gitlab-port int            Host/container port for the shared GitLab service (default 8080)
-h, --help                   help for jwt
```
- Global flags: `--debug`, `--dry-run`, `--verbose`

> GitLab is a shared singleton (`hal-gitlab`). The `--gitlab-{tag,image,port}` flags read identically on `hal vault jwt` and `hal terraform vcs-workflow`. `--gitlab-port` only applies when HAL boots a fresh instance; when a GitLab already exists (e.g. started by `hal terraform vcs-workflow` on a custom port), this command detects and reuses that live port automatically.

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal vault jwt enable
```
