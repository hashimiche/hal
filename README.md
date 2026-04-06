<div align="center">
  <img src="hal_logo.png" alt="hal logo" width="200" height="200">
</div>

# HAL - HashiCorp Academy Labs

HAL is a local DevOps lab orchestrator for HashiCorp products.

It helps you stand up realistic local environments for Vault, Boundary, Consul, Nomad, Terraform Enterprise (FDO), and observability without hand-writing large compose/manifests for every demo.

## What HAL Does Well

- Fast local product labs with sensible defaults.
- Read-only first status UX (`hal <product>` defaults to status for most product commands).
- Product + feature lifecycle patterns:
  - Core products: `deploy`, `status`, `destroy`
  - Feature flows: `--enable`, `--disable`, `--force`
- Built-in integration scenarios (OIDC, JWT, K8s auth/VSO, Boundary targets, TFE workspace automation).

## Installation

### macOS and Linux (Homebrew)

```bash
brew tap hashimiche/tap
brew install hal
```

### Manual

Download binaries from the Releases page:

- https://github.com/hashimiche/hal/releases

## Prerequisites

Install the tools required by the labs you want to run:

- Docker or Podman (required for most flows)
- KinD + kubectl + helm (required for `hal vault k8s`)
- Multipass (required for `hal nomad` and `hal boundary ssh`)

## Quick Start

```bash
# Global snapshot
hal status

# MCP bridge bootstrap (stdio)
hal mcp create
hal mcp status

# Capacity advisor views
hal capacity
hal capacity --active
hal capacity --pending

# Bring up Vault core
hal vault deploy
hal vault status

# Bring up Terraform Enterprise local stack
hal terraform deploy
hal terraform workspace --enable
hal terraform status
```

## Reproducible Version Pins

Use explicit image/version flags when you want deterministic labs across machines or over time.

```bash
# Vault core with explicit version + helper image
hal vault deploy --version 1.21 --helper-image alpine:3.22

# Vault LDAP demo with pinned OpenLDAP + phpLDAPadmin tags
hal vault ldap --enable --openldap-version 1.5.0 --phpldapadmin-version 0.9.0

# Vault K8s demo with pinned KinD node image, VSO chart, backend/proxy images
hal vault k8s --enable \
  --kind-node-image kindest/node:v1.31.1 \
  --vso-chart-version 0.8.1 \
  --web-backend-image httpd:2.4-alpine \
  --web-proxy-image nginx:alpine

# Terraform Enterprise stack with pinned supporting images
hal terraform deploy \
  --version 1.2.0 \
  --pg-version 16 \
  --redis-version 7 \
  --minio-version latest \
  --proxy-nginx-version alpine

# Nomad VM with pinned Ubuntu image channel
hal nomad deploy --ubuntu-image 22.04 --version 1.11.3

# Boundary SSH target VM with pinned image and VM sizing
hal boundary ssh --enable --ubuntu-image 22.04 --cpus 1 --mem 512M
```

💡 Tip: check available flags with `hal <product> <command> --help`.

## Core Command Map

This section is intentionally a curated quick map. Keep it focused on common workflows.
For the full, exact command surface, use `hal --help` and `hal <product> --help`.

### Global

- `hal status`
- `hal capacity` (current usage view)
- `hal capacity --active` or `hal capacity --deployed` (active heavy deployment detail view)
- `hal capacity --pending` (pending heavy deployment impact view)

### MCP

- `hal mcp create` (generate MCP client config scaffold at `~/.hal/mcp/hal-mcp.json`)
- `hal mcp up` (run HAL MCP server over stdio)
- `hal mcp status` (show local MCP readiness)
- `hal mcp down` (stop background daemon if a PID file exists; stdio mode is normally on-demand)

### Vault

- `hal vault deploy`
- `hal vault status`
- `hal vault destroy`
- `hal vault oidc --enable`
- `hal vault jwt --enable`
- `hal vault k8s --enable`
- `hal vault k8s --enable --csi` (Vault Enterprise)

### Boundary

- `hal boundary deploy`
- `hal boundary status`
- `hal boundary destroy`
- `hal boundary mariadb --enable`
- `hal boundary ssh --enable`

### Consul

- `hal consul deploy`
- `hal consul status`
- `hal consul destroy`

### Nomad

- `hal nomad deploy`
- `hal nomad status`
- `hal nomad destroy`
- `hal nomad job`

### Terraform Enterprise (FDO)

- `hal terraform deploy`
- `hal terraform workspace --enable`
- `hal terraform status`
- `hal terraform destroy`

Notes:

- `hal terraform token` is removed. Workspace and token wiring are handled by the automated `workspace --enable` flow.
- TFE local endpoint is `https://tfe.localhost:8443`.

### Observability (PLG)

- `hal obs deploy`
- `hal obs status`
- `hal obs destroy`

## Vault K8s Demo Modes

`hal vault k8s` deploys a KinD + Vault Secrets Operator lab with a direct endpoint:

- http://web.localhost:8088

No `kubectl port-forward` is required in the standard flow.

### Native mode (default)

- Uses `VaultStaticSecret`.
- Syncs secret data to Kubernetes Secret.
- Injects `HAL_SECRET` as env var into backend pods.
- Backend renders local `index.html` at startup.

### CSI mode (`--csi`, Enterprise)

- Uses `CSISecrets` + `csi.vso.hashicorp.com`.
- Projects secret data as an ephemeral CSI-mounted file.
- Backend renders local `index.html` from projected data.
- If Vault Enterprise is not detected, HAL falls back to native mode.

## Useful Endpoints

- Vault: http://vault.localhost:8200
- Consul: http://consul.localhost:8500
- Boundary: http://boundary.localhost:9200
- Terraform Enterprise: https://tfe.localhost:8443
- Grafana: http://grafana.localhost:3000
- Prometheus: http://prometheus.localhost:9090
- Loki: http://loki.localhost:3100/ready
- Vault K8s demo: http://web.localhost:8088

## Development

From repo root:

```bash
go build -o hal main.go
go build ./...
go test ./...
```

## Contributing

Contributions are welcome.

If you are changing command behavior or UX patterns, read `LLM_CONTEXT.md` and `.github/copilot-instructions.md` first so updates stay aligned with current architecture and operator guidance.