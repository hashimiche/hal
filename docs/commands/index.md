# HAL Command Spec Index

This index maps the HAL command tree to one spec file per command area.

## Root Contract
- [Global and Root Commands](global.md)
- [Global status](global-status.md)
- [Global capacity](global-capacity.md)
- [Global catalog](global-catalog.md)
- [Global destroy](global-destroy.md)
- [Global version](global-version.md)
- [Global daisy](global-daisy.md)

## Product Namespaces
- [Vault](vault.md)
- [Vault deploy](vault-deploy.md)
- [Vault status](vault-status.md)
- [Vault destroy](vault-destroy.md)
- [Vault obs](vault-obs.md)
- [Vault audit](vault-audit.md)
- [Vault oidc](vault-oidc.md)
- [Vault jwt](vault-jwt.md)
- [Vault k8s](vault-k8s.md)
- [Vault ldap](vault-ldap.md)
- [Vault database](vault-database.md)
- [Boundary](boundary.md)
- [Boundary deploy](boundary-deploy.md)
- [Boundary status](boundary-status.md)
- [Boundary destroy](boundary-destroy.md)
- [Boundary obs](boundary-obs.md)
- [Boundary mariadb](boundary-mariadb.md)
- [Boundary ssh](boundary-ssh.md)
- [Consul](consul.md)
- [Consul deploy](consul-deploy.md)
- [Consul status](consul-status.md)
- [Consul destroy](consul-destroy.md)
- [Consul obs](consul-obs.md)
- [Nomad](nomad.md)
- [Nomad deploy](nomad-deploy.md)
- [Nomad status](nomad-status.md)
- [Nomad destroy](nomad-destroy.md)
- [Nomad obs](nomad-obs.md)
- [Nomad job](nomad-job.md)
- [Terraform](terraform.md)
- [Terraform deploy](terraform-deploy.md)
- [Terraform status](terraform-status.md)
- [Terraform destroy](terraform-destroy.md)
- [Terraform obs](terraform-obs.md)
- [Terraform vcs-workflow](terraform-workspace.md)
- [Terraform api-workflow](terraform-cli.md)
- [Terraform agent](terraform-agent.md)
- [Observability](observability.md)
- [Observability deploy](observability-deploy.md)
- [Observability status](observability-status.md)
- [Observability destroy](observability-destroy.md)
- [MCP](mcp.md)
- [MCP create](mcp-create.md)
- [MCP update](mcp-up.md)
- [MCP status](mcp-status.md)
- [MCP delete](mcp-down.md)

## Command Defaults
When called without a subcommand, namespaces default to status-style behavior:
- `hal vault` -> `hal vault status`
- `hal boundary` -> `hal boundary status`
- `hal consul` -> `hal consul status`
- `hal nomad` -> `hal nomad status`
- `hal terraform` / `hal tf` -> `hal terraform status`
- `hal obs` -> `hal obs status`
- `hal mcp` -> `hal mcp status`

## Drift Rule
If command wiring changes in `cmd/` (`AddCommand(...)`, `Use`, `Aliases`, flags), update the corresponding file in this folder in the same PR.
