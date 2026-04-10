# HAL Observability Command Spec

## Base Command
- Command: `hal obs`
- Purpose: manage local PLG observability stack
- Default behavior: runs `hal obs status`

## Subcommands
- `hal obs deploy`
  - Deploy Prometheus, Loki, Grafana, and Promtail
  - Spec: [observability-deploy.md](observability-deploy.md)

- `hal obs status`
  - Show observability component health/status
  - Spec: [observability-status.md](observability-status.md)

- `hal obs destroy`
  - Destroy observability stack and local state
  - Spec: [observability-destroy.md](observability-destroy.md)

## Sources
- Namespace: `cmd/observability/observability.go`
- Subcommands: `cmd/observability/*.go`
