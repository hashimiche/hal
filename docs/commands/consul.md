# HAL Consul Command Spec

## Base Command
- Command: `hal consul`
- Purpose: manage local Consul control plane
- Default behavior: runs `hal consul status`

## Subcommands
- `hal consul create`
  - Deploy a standalone local Consul server
  - Spec: [consul-deploy.md](consul-deploy.md)

- `hal consul status`
  - Show Consul deployment health and status
  - Spec: [consul-status.md](consul-status.md)

- `hal consul delete`
  - Destroy local Consul server resources
  - Spec: [consul-destroy.md](consul-destroy.md)

- `hal consul obs`
  - Manage Consul observability artifacts
  - Subcommands: `create`, `update`, `delete`, `status`
  - Spec: [consul-obs.md](consul-obs.md)

## Sources
- Namespace: `cmd/consul/consul.go`
- Subcommands: `cmd/consul/*.go`
