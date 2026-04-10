# HAL Global and Root Commands Spec

## Scope
This file covers the root `hal` command, persistent flags, and global commands that are not under a product namespace.

## Root Command
- Command: `hal`
- Description: HashiCorp Academy Lab CLI
- Default behavior: prints help when no command is provided

## Persistent Flags
- `--debug`: enable debug logging through global runtime state
- `--dry-run`: simulate execution with no state mutation

## Global Commands
- `hal status`
  - Purpose: global multi-product status summary
  - Behavior: reports deployment state for Vault, Boundary, Consul, Nomad, TFE, and Observability
  - Spec: [global-status.md](global-status.md)

- `hal capacity`
  - Purpose: local runtime capacity view and what-if estimates
  - Behavior: shows live engine usage and heavy scenario projections
  - Spec: [global-capacity.md](global-capacity.md)

- `hal catalog`
  - Purpose: product and feature catalog
  - Behavior: prints command-oriented product overview
  - Spec: [global-catalog.md](global-catalog.md)

- `hal destroy`
  - Purpose: global infrastructure teardown
  - Scope: HAL-managed containers, KinD clusters, Multipass VMs, and observability local state
  - Safety: confirmation prompt by default
  - Override: `--force` skips prompt
  - Supports: `--dry-run`
  - Spec: [global-destroy.md](global-destroy.md)

- `hal version`
  - Purpose: print HAL version
  - Spec: [global-version.md](global-version.md)

- `hal daisy` (hidden)
  - Purpose: cinematic easter-egg variant of global teardown
  - Note: uses the same backend teardown routine
  - Spec: [global-daisy.md](global-daisy.md)

## Sources
- Root wiring: `cmd/root.go`
- Global commands: `cmd/status.go`, `cmd/capacity.go`, `cmd/catalog.go`, `cmd/destroy.go`, `cmd/version.go`, `cmd/daisy.go`
