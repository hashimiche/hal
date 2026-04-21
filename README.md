<div align="center">
  <img src="hal_logo.png" alt="hal logo" width="200" height="200">
</div>

# HAL — HashiCorp Academy Labs

[![Release](https://img.shields.io/github/v/release/hashimiche/hal)](https://github.com/hashimiche/hal/releases)
[![Go Build](https://img.shields.io/github/actions/workflow/status/hashimiche/hal/release.yml?label=build)](https://github.com/hashimiche/hal/actions)
[![Go Version](https://img.shields.io/github/go-mod/go-version/hashimiche/hal)](go.mod)
[![Go Report Card](https://goreportcard.com/badge/github.com/hashimiche/hal)](https://goreportcard.com/report/github.com/hashimiche/hal)
[![Last Commit](https://img.shields.io/github/last-commit/hashimiche/hal)](https://github.com/hashimiche/hal/commits/main)
![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey)
![Powered by Cobra](https://img.shields.io/badge/powered%20by-Cobra-blueviolet)

HAL is a local lab orchestrator for HashiCorp products. It replaces the hand-written Docker Compose and Kubernetes manifests you'd otherwise need to stand up realistic Vault, Boundary, Consul, Nomad, or Terraform Enterprise environments — so you can focus on learning and demoing, not plumbing.

---

## TL;DR

```bash
brew tap hashimiche/tap && brew install hal

hal status          # see what's running
hal vault create    # spin up Vault
hal vault status    # confirm it's healthy
```

---

## Prerequisites

Install the tooling required by the labs you want to run before installing HAL.

| Requirement | Used by |
|---|---|
| Docker **or** Podman | Almost every `hal` flow |
| KinD + kubectl + helm | `hal vault k8s` |
| Multipass | `hal nomad`, `hal boundary ssh` |

> **Engine detection:** HAL probes `docker info` first, then `podman info`, and uses whichever responds. No alias is required — both engines work natively.

---

## Installation

### Homebrew (macOS and Linux)

```bash
brew tap hashimiche/tap
brew install hal
```

### Manual binary

Download the latest release from the [Releases page](https://github.com/hashimiche/hal/releases) and place the binary on your `$PATH`.

---

## Usage

### Global flags

| Flag | Effect |
|---|---|
| `--help`, `-h` | Show help for any command |
| `--version`, `-v` | Print the HAL version |
| `--dry-run` | Print what would run without executing |
| `--debug` | Enable verbose debug output |

> For the full flag surface of any subcommand, run `hal <product> <command> --help`.

---

### Global commands

#### `hal status` — environment snapshot

```bash
hal status
```

Shows the health of every product HAL knows about in one pass.

#### `hal capacity` — resource advisor

```bash
hal capacity             # full view
hal capacity --active    # running containers/VMs only
hal capacity --pending   # resources queued but not yet started
```

#### `hal catalog` — available products

```bash
hal catalog
```

Lists every product namespace HAL can manage.

#### `hal delete` — global teardown

```bash
hal delete
```

Tears down all HAL-managed resources. **Destructive — prompts for confirmation.**

---

### Vault (`hal vault`)

**Product lifecycle**

```bash
hal vault create                          # provision Vault with defaults
hal vault create --version 2.0            # pin the image version
hal vault create --edition ent            # use Vault Enterprise image
hal vault create --join-consul            # tether to the local Consul instance
hal vault update                          # reconcile config changes
hal vault status                          # health + seal/init state
hal vault delete                          # remove Vault container and volumes
```

**Feature subcommands** — `enable` / `update` / `disable`

```bash
# OIDC auth method (deploys Keycloak as the provider)
hal vault oidc enable --keycloak-version 24.0.4
hal vault oidc update
hal vault oidc disable

# JWT auth method (deploys GitLab CE as the OIDC provider)
hal vault jwt enable --gitlab-version 18.10.1-ce.0

# Database secrets engine (MariaDB backend — only supported backend today)
hal vault database enable --backend mariadb --mariadb-version 11.4

# LDAP auth with pinned image versions
hal vault ldap enable --openldap-version 1.5.0 --phpldapadmin-version 0.9.0

# Kubernetes auth + Vault Secrets Operator (KinD cluster)
hal vault k8s enable \
  --kind-node-image kindest/node:v1.31.1 \
  --vso-chart-version 0.8.1 \
  --web-backend-image httpd:2.4-alpine \
  --web-proxy-image nginx:alpine

hal vault k8s update
hal vault k8s disable

# Audit logging (file-based by default)
hal vault audit enable
hal vault audit enable --loki    # also wire into the Promtail/Loki shared volume
```

**Observability** (opt-in, CRUD lifecycle)

```bash
hal vault obs create
hal vault obs status
hal vault obs update
hal vault obs delete
```

**Vault K8s demo modes**

The demo app is reachable at http://web.localhost:8088 — no `kubectl port-forward` required.

| Mode | Flag | How it works |
|---|---|---|
| Native (default) | _(none)_ | `VaultStaticSecret` syncs to a Kubernetes Secret; injects `HAL_SECRET` env var |
| CSI | `--csi` | Projects secret data as an ephemeral CSI-mounted file (requires Vault Enterprise; falls back to native if not detected) |

---

### Boundary (`hal boundary`)

```bash
hal boundary create --version 0.15.2
hal boundary status
hal boundary delete

# SSH target VM (Multipass)
hal boundary ssh enable --ubuntu-image 22.04 --cpus 1 --mem 512M
hal boundary ssh update
hal boundary ssh disable

# MariaDB target
hal boundary mariadb enable --mariadb-version 11.4
hal boundary mariadb enable --mariadb-version 11.4 --with-vault    # link Vault dynamic creds
hal boundary mariadb disable

# Observability
hal boundary obs create
hal boundary obs delete
```

---

### Consul (`hal consul`)

```bash
hal consul create --version 1.15.0
hal consul status
hal consul delete

hal consul obs create
hal consul obs status
hal consul obs delete
```

---

### Nomad (`hal nomad`)

```bash
hal nomad create --ubuntu-image 22.04 --version 1.11.3 --cpus 2 --mem 2G
hal nomad status
hal nomad delete

hal nomad obs create
hal nomad obs delete
```

---

### Terraform Enterprise (`hal terraform` / `hal tf`)

**Primary TFE instance**

```bash
hal terraform create \
  --version 1.2.0 \
  --pg-version 16 \
  --redis-version 7 \
  --minio-version latest \
  --minio-api-port 19000 \
  --minio-console-port 19001 \
  --proxy-nginx-version alpine

hal terraform status
hal terraform update
hal terraform delete
```

**Twin TFE instance** — reuses the primary ecosystem (PostgreSQL, Redis, MinIO)

```bash
hal terraform create --target twin --twin-version 1.2.0
hal terraform status --target twin
hal terraform update --target twin
hal terraform delete --target twin
```

**Feature subcommands**

```bash
# VCS-driven workspace workflow (local GitLab integration)
hal terraform vcs-workflow enable --gitlab-version 18.10.1-ce.0
hal terraform vcs-workflow update
hal terraform vcs-workflow disable

# API-driven workspace workflow
hal terraform api-workflow enable   # alias: hal terraform api enable
hal terraform api-workflow disable

# Custom agent pool
hal terraform agent enable --image hashicorp/tfc-agent:latest
hal terraform agent update
hal terraform agent disable

# Workspace automation
hal terraform workspace enable
hal terraform workspace update
hal terraform workspace disable
```

**Observability** (`--target primary | twin | both`)

```bash
hal terraform obs create --target both
hal terraform obs status --target both
hal terraform obs update --target twin
hal terraform obs delete --target primary
```

---

### Standalone observability stack (`hal obs`)

```bash
hal obs create \
  --loki-version 3.7 \
  --grafana-version main \
  --prom-version main \
  --promtail-version 3.6
hal obs status
hal obs update
hal obs delete
```

---

### MCP bridge (`hal mcp`)

HAL exposes an MCP (Model Context Protocol) server so AI assistants can query lab state directly.

```bash
hal mcp create    # generate or replace the MCP config and managed binary
hal mcp serve     # run the MCP server over stdio for an MCP client such as hal-plus
hal mcp status    # inspect MCP config/binary readiness
hal mcp delete    # remove MCP config, managed binary, and stale PID state
```

`hal delete` also removes HAL-managed MCP artifacts as part of global cleanup.

**MCP tools available to the LLM**

| Tool | What it returns |
|---|---|
| `hal_status` | Global status + executed-command metadata |
| `hal_capacity` | Current / active / pending resource views |
| `hal_product_status` | Per-product status (strict args) |
| `hal_help` | Real HAL help output to ground command syntax |
| `hal_snapshot` | Batched snapshot across status, capacity, and product status |
| `hal_status_baseline` | Runtime baseline status routing |
| `hal_plan_deploy` | Intent-driven deploy/setup planning |
| `hal_plan_verify` | Deterministic post-action verification command plan |

---

## Configuration

HAL uses environment variables and Docker/Podman networking — there is no config file to edit under normal use.

| Variable | Default | Purpose |
|---|---|---|
| `VAULT_ADDR` | `http://127.0.0.1:8200` | Vault API address used by `hal vault` commands |
| `VAULT_TOKEN` | `root` | Vault root token for local dev labs |

**Local service endpoints** (after `create`):

| Service | URL |
|---|---|
| Vault | http://vault.localhost:8200 |
| Consul | http://consul.localhost:8500 |
| Boundary | http://boundary.localhost:9200 |
| Terraform Enterprise | https://tfe.localhost:8443 |
| MinIO API | http://127.0.0.1:19000 |
| MinIO Console | http://127.0.0.1:19001 |
| Grafana | http://grafana.localhost:3000 |
| Prometheus | http://prometheus.localhost:9090 |
| Loki | http://loki.localhost:3100/ready |
| Vault K8s demo | http://web.localhost:8088 |

---

## Caveats & Known Limitations

- **macOS-first.** HAL is primarily developed and tested on macOS. Linux support is best-effort. Windows is not supported.
- **Docker or Podman must be running** before any `create` command. HAL auto-detects the engine by probing `docker info` then `podman info` — whichever responds is used. No alias is needed. HAL will error early if neither engine is reachable.
- **`hal vault k8s`** requires KinD, kubectl, and helm to be on your `$PATH`. The KinD cluster is created on demand and removed on `disable`.
- **`hal nomad` and `hal boundary ssh`** require Multipass. The Ubuntu VM is provisioned and torn down as part of the lifecycle.
- **`hal delete`** (global teardown) removes all HAL-managed containers, volumes, and VMs. There is a confirmation prompt but the action is not reversible.
- **TFE requires a valid license.** `hal terraform create` expects a Terraform Enterprise license to be in place. The stack will start but TFE itself will not activate without one.
- **CSI mode for `hal vault k8s`** requires a Vault Enterprise binary. HAL will detect the edition at runtime and fall back to native mode automatically.
- **Version pinning is opt-in.** By default HAL pulls the latest stable image for each product. Use explicit `--version` and image flags for reproducible labs.

---

## Contributing & Development

```bash
# From the repo root
go build -o hal main.go   # build the binary
go build ./...            # verify all packages compile
go test ./...             # run the full test suite
```

Before changing command behavior or UX patterns, read these files in order:

1. `docs/cli-lifecycle-model.md` — authoritative lifecycle verb model
2. `.github/copilot-instructions.md` — concise policy and architecture notes
3. `LLM_CONTEXT.md` — LLM-oriented command guidance

Keep all three in sync when adding or renaming commands.

---

## License

This tool was developed on HashiCorp/IBM equipment and is subject to HashiCorp/IBM's intellectual property policies. No independent open-source license has been applied. All rights reserved unless explicitly stated otherwise by HashiCorp/IBM.