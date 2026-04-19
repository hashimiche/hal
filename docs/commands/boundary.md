# HAL Boundary Command Spec

## Base Command
- Command: `hal boundary`
- Purpose: manage local Boundary deployments
- Default behavior: runs `hal boundary status`

## Subcommands
- `hal boundary create`
  - Deploy Boundary control plane components
  - Spec: [boundary-deploy.md](boundary-deploy.md)

- `hal boundary status`
  - Show Boundary and target status
  - Spec: [boundary-status.md](boundary-status.md)

- `hal boundary delete`
  - Destroy Boundary and associated target resources
  - Spec: [boundary-destroy.md](boundary-destroy.md)

- `hal boundary mariadb`
  - Deploy a MariaDB target for Boundary
  - Spec: [boundary-mariadb.md](boundary-mariadb.md)

- `hal boundary ssh`
  - Deploy a Multipass Ubuntu SSH target VM for Boundary
  - Spec: [boundary-ssh.md](boundary-ssh.md)

## Sources
- Namespace: `cmd/boundary/boundary.go`
- Subcommands: `cmd/boundary/*.go`
