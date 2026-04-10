# HAL Consul Command Spec

## Base Command
- Command: `hal consul`
- Purpose: manage local Consul control plane
- Default behavior: runs `hal consul status`

## Subcommands
- `hal consul deploy`
  - Deploy a standalone local Consul server
  - Spec: [consul-deploy.md](consul-deploy.md)

- `hal consul status`
  - Show Consul deployment health and status
  - Spec: [consul-status.md](consul-status.md)

- `hal consul destroy`
  - Destroy local Consul server resources
  - Spec: [consul-destroy.md](consul-destroy.md)

## Sources
- Namespace: `cmd/consul/consul.go`
- Subcommands: `cmd/consul/*.go`
