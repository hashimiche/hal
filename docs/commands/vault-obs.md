# HAL Vault Observability Feature Spec

## Command Family
- Base: `hal vault obs`
- Purpose: manage Vault observability artifacts without redeploying Vault.
- Default behavior: `hal vault obs` runs `hal vault obs status`.

## Lifecycle Commands
- `hal vault obs create`: create/refresh Vault Prometheus target + dashboard artifact.
- `hal vault obs update`: reconcile current Vault observability artifacts (same behavior as create).
- `hal vault obs delete`: remove Vault observability artifacts.
- `hal vault obs status`: show current Vault observability artifact state.

## Prerequisites
- Vault container is running (`hal-vault`).
- Observability stack is ready (Prometheus + Grafana from `hal obs create`).

## Sources
- Command wiring: `cmd/vault/obs.go`
- Shared artifact helpers: `internal/global/obs.go`
