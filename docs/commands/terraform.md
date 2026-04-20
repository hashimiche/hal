# HAL Terraform Command Spec

## Base Command
- Command: `hal terraform`
- Alias: `hal tf`
- Purpose: manage local Terraform Enterprise workflows
- Default behavior: runs `hal terraform status`

## Subcommands
- `hal terraform create`
  - Deploy local Terraform Enterprise stack
  - Supports `--target primary|twin|both` (default `primary`)
  - Spec: [terraform-deploy.md](terraform-deploy.md)

- `hal terraform status`
  - Show Terraform Enterprise stack health/status
  - Supports `--target primary|twin|both` (default `primary`)
  - Spec: [terraform-status.md](terraform-status.md)

- `hal terraform delete`
  - Destroy Terraform Enterprise stack and local state
  - Supports `--target primary|twin|both` (default `primary`)
  - Spec: [terraform-destroy.md](terraform-destroy.md)

- `hal terraform vcs-workflow`
  - Alias: `hal terraform vcs`
  - Configure GitLab-backed VCS workflow lab flow with target-aware workspace wiring
  - Spec: [terraform-workspace.md](terraform-workspace.md)

- `hal terraform api-workflow`
  - Alias: `hal terraform api`
  - Build/start Terraform+TFX API helper shell for local TFE workflows
  - Lifecycle actions: `enable`, `disable`, `update`, plus `--target/-t`
  - Spec: [terraform-cli.md](terraform-cli.md)

- `hal terraform agent`
  - Manage local TFE custom agent pool runtime for primary, twin, or both targets
  - Lifecycle actions: `enable`, `disable`, `update`
  - Supports `--target primary|twin|both` (default `primary`)
  - Spec: [terraform-agent.md](terraform-agent.md)

## Related Detailed Specs
- [Terraform API Workflow Spec](../terraform-cli-container-spec.md)

## Sources
- Namespace: `cmd/terraform/terraform.go`
- Subcommands: `cmd/terraform/*.go`
