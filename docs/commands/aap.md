# HAL AAP Command Spec

## Base Command
- Command: `hal aap`
- Purpose: manage local AAP runtime
- Default behavior: runs `hal aap status`

## Subcommands
- `hal aap create`
  - Deploy local AAP container runtime
  - Spec: [aap-deploy.md](aap-deploy.md)

- `hal aap status`
  - Show AAP runtime health and status
  - Spec: [aap-status.md](aap-status.md)

- `hal aap update`
  - Reconcile an existing AAP runtime in place
  - Uses same flags as `hal aap create` (without `--update` alias)

- `hal aap delete`
  - Destroy local AAP runtime resources
  - Spec: [aap-destroy.md](aap-destroy.md)

## Sources
- Namespace: `cmd/aap/aap.go`
- Subcommands: `cmd/aap/*.go`
