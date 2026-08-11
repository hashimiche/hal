# HAL Terraform VCS Workflow Command Spec

## Command
- `hal terraform vcs-workflow`
- Alias: `hal terraform vcs`

## Purpose
Configure a Terraform VCS-driven workflow lab with shared GitLab reuse and target-aware workspace wiring.

## Behavior
- Ensures prerequisites (target TFE and shared GitLab service).
- Wires/validates target-specific repository and workspace integration for VCS-driven workflows.
- Supports `--target primary|twin|both`; `both` provisions dedicated repo/workspace automation for each TFE target.

## Related
- Parent namespace: [terraform.md](terraform.md)
- API workflow helper: [terraform-cli.md](terraform-cli.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal terraform vcs-workflow --help`:
```text
--auto-approve          Skip interactive confirmation for destructive disable operations
--gitlab-image string   GitLab CE container image name (default "gitlab/gitlab-ce")
--gitlab-tag string     GitLab CE container image tag (default "18.11.9-ce.0")
--gitlab-port int       Host/container port for the shared GitLab service (default 8080)
-h, --help              help for vcs-workflow
-t, --target string     Terraform scope to act on: primary, twin, or both (default "primary")
```
- Global flags: `--debug`, `--dry-run`, `--verbose`

The GitLab tuning flags read identically on `hal vault jwt`, which shares the same GitLab singleton:
- `--gitlab-image` (default `gitlab/gitlab-ce`) and `--gitlab-tag` (default `18.11.9-ce.0`) select the GitLab CE container image and tag.
- `--gitlab-port` (default `8080`) sets the host/container port for the shared GitLab service; override it when host port `8080` is already in use.

Other advanced flags (`--gitlab-root-password`, `--tfe-*`, `--project-*`) stay hidden from default help to keep the command surface concise.

> Note: the shared GitLab service is reused if it is already running. `--gitlab-image`, `--gitlab-tag`, and `--gitlab-port` only take effect when HAL boots a fresh `hal-gitlab` container.

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal terraform vcs-workflow enable
hal terraform vcs-workflow enable -t both
hal terraform vcs-workflow enable --gitlab-port 8929
hal terraform vcs-workflow disable -t primary --auto-approve
```
