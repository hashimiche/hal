# HAL Nomad Command Spec

## Base Command
- Command: `hal nomad`
- Purpose: manage local Nomad cluster via Multipass
- Default behavior: runs `hal nomad status`

## Subcommands
- `hal nomad create`
  - Deploy local Nomad cluster VM(s)
  - Spec: [nomad-deploy.md](nomad-deploy.md)

- `hal nomad status`
  - Show Nomad cluster health/status
  - Spec: [nomad-status.md](nomad-status.md)

- `hal nomad delete`
  - Destroy Nomad VM resources
  - Spec: [nomad-destroy.md](nomad-destroy.md)

- `hal nomad obs`
  - Manage Nomad observability artifacts
  - Subcommands: `create`, `update`, `delete`, `status`
  - Spec: [nomad-obs.md](nomad-obs.md)

- `hal nomad job`
  - Submit sample workloads/jobs to Nomad
  - Spec: [nomad-job.md](nomad-job.md)

## Sources
- Namespace: `cmd/nomad/nomad.go`
- Subcommands: `cmd/nomad/*.go`
