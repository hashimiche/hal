# HAL Nomad Observability Feature Spec

## Command Family
- Base: `hal nomad obs`
- Purpose: manage Nomad observability artifacts without redeploying Nomad.
- Default behavior: `hal nomad obs` runs `hal nomad obs status`.

## Lifecycle Commands
- `hal nomad obs create`: create/refresh Nomad Prometheus target + dashboard artifact.
- `hal nomad obs update`: reconcile current Nomad observability artifacts (same behavior as create).
- `hal nomad obs delete`: remove Nomad observability artifacts.
- `hal nomad obs status`: show current Nomad observability artifact state.

## Prerequisites
- Nomad VM exists (`hal-nomad`).
- Observability stack is ready (Prometheus + Grafana from `hal obs create`).

## Sources
- Command wiring: `cmd/nomad/obs.go`
- Shared artifact helpers: `internal/global/obs.go`
