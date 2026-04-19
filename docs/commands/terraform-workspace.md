# HAL Terraform Workspace Command Spec

## Command
- `hal terraform workspace`
- Alias: `hal terraform ws`

## Purpose
Configure a Terraform workspace lab with shared GitLab reuse and VCS workflow wiring.

## Behavior
- Ensures prerequisites (TFE and shared GitLab service).
- Wires/validates repository and workspace integration for lab workflows.

## Related
- Parent namespace: [terraform.md](terraform.md)
- CLI helper: [terraform-cli.md](terraform-cli.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal terraform workspace --help`:
```text
--gitlab-root-password string     Root password used to bootstrap GitLab when HAL starts it (default "hal9000FTW")
--gitlab-token-id string          Alias of --tfe-vcs-oauth-token-id
--gitlab-version string           Version of the GitLab CE image used for shared Terraform workspace setup (default "18.10.1-ce.0")
-h, --help                            help for workspace
--project-name string             GitLab project name for the Terraform workspace demo (default "tfe-agent-demo")
--project-path string             GitLab project path for the Terraform workspace demo (default "tfe-agent-demo")
--tfe-admin-email string          Initial TFE admin email used when bootstrapping via IACT (default "haladmin@localhost")
--tfe-admin-password string       Initial TFE admin password used when bootstrapping via IACT (default "hal9000FTW")
--tfe-admin-username string       Initial TFE admin username used when bootstrapping via IACT (default "haladmin")
--tfe-api-token string            Terraform Enterprise app API token (or set TFE_API_TOKEN)
--tfe-org string                  Terraform Enterprise organization name to bootstrap (default "hal")
--tfe-project string              Terraform Enterprise project name to bootstrap (default "Dave")
--tfe-tags-regex string           Regex for VCS tag-triggered runs (set empty string to disable tag triggers) (default "^v\\d+\\.\\d+\\.\\d+(?:-\\w+)?$")
--tfe-url string                  Terraform Enterprise base URL (default "https://tfe.localhost:8443")
--tfe-vcs-branch string           Git branch to trigger VCS runs from (set non-main for tag-focused workflows) (default "main")
--tfe-vcs-oauth-token-id string   Terraform Enterprise VCS OAuth token id for linking the workspace to GitLab (or set TFE_GITLAB_OAUTH_TOKEN_ID)
--tfe-workspace string            Terraform Enterprise workspace name to bootstrap (default "tfe-agent-demo")
-u, --update                          Reconcile existing Terraform workspace automation without full teardown
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal terraform workspace enable
```
