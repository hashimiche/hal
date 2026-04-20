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
--auto-approve    Skip interactive confirmation for destructive disable operations
-h, --help        help for vcs-workflow
-t, --target string Terraform scope to act on: primary, twin, or both (default "primary")
```
- Global flags: `--debug`, `--dry-run`

Advanced VCS and GitLab tuning flags remain available but are intentionally hidden from default help to keep the command surface concise.

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal terraform vcs-workflow enable
hal terraform vcs-workflow enable -t both
hal terraform vcs-workflow disable -t primary --auto-approve
```
