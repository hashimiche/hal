# HAL Consul Observability Feature Spec

## Command Family
- Base: `hal consul obs`
- Purpose: manage Consul observability artifacts without redeploying Consul.
- Default behavior: `hal consul obs` runs `hal consul obs status`.

## Lifecycle Commands
- `hal consul obs create`: create/refresh Consul Prometheus target + dashboard artifact.
- `hal consul obs update`: reconcile current Consul observability artifacts (same behavior as create).
- `hal consul obs delete`: remove Consul observability artifacts.
- `hal consul obs status`: show current Consul observability artifact state.

## Prerequisites
- Consul container is running (`hal-consul`).
- Observability stack is ready (Prometheus + Grafana from `hal obs create`).

## Sources
- Command wiring: `cmd/consul/obs.go`
- Shared artifact helpers: `internal/global/obs.go`
