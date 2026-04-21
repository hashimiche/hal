# HAL Boundary Observability Feature Spec

## Command Family
- Base: `hal boundary obs`
- Purpose: manage Boundary observability artifacts without redeploying Boundary.
- Default behavior: `hal boundary obs` runs `hal boundary obs status`.

## Lifecycle Commands
- `hal boundary obs create`: create/refresh Boundary Prometheus target artifact.
- `hal boundary obs update`: reconcile current Boundary observability artifacts (same behavior as create).
- `hal boundary obs delete`: remove Boundary observability artifacts.
- `hal boundary obs status`: show current Boundary observability artifact state.

## Notes
- Boundary currently has target registration support. Dashboard artifact may be optional if no official dashboard is mapped.

## Prerequisites
- Boundary container is running (`hal-boundary`).
- Observability stack is ready (Prometheus + Grafana from `hal obs create`).

## Sources
- Command wiring: `cmd/boundary/obs.go`
- Shared artifact helpers: `internal/global/obs.go`
