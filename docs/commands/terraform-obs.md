# HAL Terraform Observability Feature Spec

## Command Family
- Base: `hal terraform obs`
- Purpose: manage Terraform Enterprise observability artifacts without redeploying Terraform Enterprise.
- Default behavior: `hal terraform obs` runs `hal terraform obs status`.

## Lifecycle Commands

### `hal terraform obs create`
- Creates or refreshes Terraform observability artifacts.
- Artifacts:
  - Prometheus target file: `~/.hal/obs/targets/terraform.json`
  - Grafana dashboard artifact: `~/.hal/obs/dashboards/terraform.json`
- Requires obs stack readiness (Prometheus + Grafana) and running Terraform target scope.
- Supports `--target primary|twin|both` (default `primary`).

### `hal terraform obs update`
- Alias behavior of `create` for reconciliation.
- Same prerequisites and target behavior as `create`.

### `hal terraform obs delete`
- Removes Terraform observability artifacts for selected scope.
- Supports `--target primary|twin|both` (default `primary`).
- Scope behavior:
  - `primary`: removes primary TFE target entry from `terraform.json`.
  - `twin`: removes twin TFE target entry from `terraform.json`.
  - `both`: removes `terraform.json` entirely.

### `hal terraform obs status`
- Reports whether Terraform observability artifacts are configured for the selected scope.
- Supports `--target primary|twin|both` (default `primary`).

## Targeting Rules
- `--target primary` maps to `hal-tfe:9090`.
- `--target twin` maps to `<twin-core-container>:9090` (default container: `hal-tfe-bis`).
- `--target both` manages both endpoints.

## Lifecycle Note
- Terraform observability artifacts are managed via:
  - `hal terraform obs create|update|delete|status`

## Source Wiring
- Command: `cmd/terraform/obs.go`
- Shared helpers: `internal/global/obs.go`
