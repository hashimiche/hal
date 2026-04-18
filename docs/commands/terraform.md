# HAL Terraform Command Spec

## Base Command
- Command: `hal terraform`
- Alias: `hal tf`
- Purpose: manage local Terraform Enterprise workflows
- Default behavior: runs `hal terraform status`

## Subcommands
- `hal terraform deploy`
  - Deploy local Terraform Enterprise stack
  - Spec: [terraform-deploy.md](terraform-deploy.md)

- `hal terraform status`
  - Show Terraform Enterprise stack health/status
  - Spec: [terraform-status.md](terraform-status.md)

- `hal terraform destroy`
  - Destroy Terraform Enterprise stack and local state
  - Spec: [terraform-destroy.md](terraform-destroy.md)

- `hal terraform workspace`
  - Alias: `hal terraform ws`
  - Configure GitLab-backed workspace/VCS lab flow
  - Spec: [terraform-workspace.md](terraform-workspace.md)

- `hal terraform cli`
  - Build/start Terraform+TFX helper shell for local TFE workflows
  - Lifecycle flags: `--enable/-e`, `--console/-c`, `--disable/-d`, `--force/-f`
  - Spec: [terraform-cli.md](terraform-cli.md)

- `hal terraform agent`
  - Manage local TFE custom agent pool runtime and `hal-tfe-agent` lifecycle
  - Lifecycle flags: `--enable/-e`, `--disable/-d`, `--force/-f`
  - Spec: [terraform-agent.md](terraform-agent.md)

## Related Detailed Specs
- [Terraform CLI Container Spec](../terraform-cli-container-spec.md)

## Sources
- Namespace: `cmd/terraform/terraform.go`
- Subcommands: `cmd/terraform/*.go`
