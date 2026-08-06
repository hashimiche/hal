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
brew tap hashimiche/tap && brew trust hashimiche/tap && brew install hal

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
| [KinD](https://kind.sigs.k8s.io/docs/user/quick-start/) + [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) + [helm](https://helm.sh/docs/intro/install/) | `hal vault k8s` |
| [Multipass](https://canonical.com/multipass/install) | `hal nomad`, `hal boundary ssh` |
| [Ollama](https://ollama.com) | `hal plus` — local AI model backend |

> **Engine detection:** HAL probes `docker info` first, then `podman info`, and uses whichever responds. No alias is required — both engines work natively.

---

## Installation

### Homebrew (macOS and Linux)

```bash
brew tap hashimiche/tap
brew trust hashimiche/tap   # Homebrew 6.0+ requires trusting non-official taps
brew install hal
```

> **Homebrew 6.0+ (June 2026):** non-official taps must be explicitly trusted before install. `brew trust hashimiche/tap` grants whole-tap trust. To trust only the formula instead, run `brew trust --formula hashimiche/tap/hal`, or skip trust entirely by installing the fully-qualified name: `brew install hashimiche/tap/hal`.

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
hal vault create                          # provision Vault with defaults (dev mode)
hal vault create --vault-tag 2.0          # pin the image tag
hal vault create --vault-image myregistry.local/vault --vault-tag 2.0  # custom image
hal vault create --edition ent            # use Vault Enterprise image (still dev mode)
hal vault create --edition ent-hsm --mode prod  # Enterprise HSM build + local SoftHSM2 runtime
hal vault create --join-consul            # tether to the local Consul instance
hal vault update                          # reconcile config changes
hal vault status                          # health + seal/init state
hal vault delete                          # remove Vault container and volumes
```

**Production-mode Vault Enterprise** (`--mode prod`) — a single-node, persistent,
TLS-enabled Enterprise instance, the Vault analog to `hal terraform create`:

```bash
export VAULT_LICENSE='...'                # or VAULT_LICENSE_PATH=/path/vault.hclic
hal vault create --mode prod              # real `server -config`, integrated Raft, HTTPS, auto init+unseal
```

- Boots `vault server -config` with **integrated Raft** storage on a persistent
  volume (survives restarts — unlike dev mode's in-memory storage).
- Serves **HTTPS at `https://vault.localhost:8200`** with a HAL-forged self-signed
  cert under `~/.hal/vault-prod/certs/` (dev mode stays plaintext HTTP).
- Auto-runs `operator init` (default `--key-shares 1 --key-threshold 1`) and
  unseals. The generated **unseal key + root token are saved to
  `~/.hal/vault-prod/init.json` (mode `0600`)** — retrieve them anytime with
  `hal vault status` or `hal creds status`.
- `--mode prod` implies `--edition ent`; requires a valid `VAULT_LICENSE`.
- `--edition ent-hsm --mode prod` builds `hal-vault-softhsm:latest`, boots the
  Enterprise HSM binary with PKCS#11 support, and makes HSM-backed PKI the default.
- `hal vault delete` removes the container, Raft volume, **and** `~/.hal/vault-prod/`.

**Feature subcommands** — `enable` / `update` / `disable`

```bash
# OIDC auth method (deploys Authentik as the IdP)
hal vault oidc enable
hal vault oidc enable --authentik-tag 2026.2.3  # pin the image tag
hal vault oidc enable --scim                     # also configure SCIM (Vault Enterprise only)
hal vault oidc update
hal vault oidc disable

# JWT auth method (deploys GitLab CE as the OIDC provider)
hal vault jwt enable --gitlab-tag 18.10.1-ce.0
hal vault jwt enable --gitlab-port 8929   # if host port 8080 is taken (shared with terraform vcs-workflow)

# AAP JWT/OIDC org-mapped auth setup (starts AAP container + configures OIDC)
hal vault aap enable
hal vault aap update
hal vault aap disable

# Database secrets engine (MariaDB backend — only supported backend today)
hal vault database enable --backend mariadb --vault-mariadb-tag 11.4
hal vault database enable --backend oracle \
  --oracle-plugin-path /path/to/vault-plugin-database-oracle   # Enterprise only — see docs/vault-oracle-plugin-build.md

# Database secrets engine + KinD + VSO (VaultDynamicSecret — credentials live-rotate in a real app)
hal vault database enable --k8s                                 # MariaDB + KinD + VSO, 15s TTL rotation demo
hal vault database enable --backend oracle --oracle-plugin-path /path/to/vault-plugin-database-oracle --k8s
hal vault database update --k8s                                 # reconcile (re-creates KinD cluster if needed)
hal vault database disable --k8s                                # tear down DB + KinD cluster

# LDAP auth with pinned image versions
hal vault ldap enable --openldap-version 1.5.0 --phpldapadmin-version 0.9.0

# Kubernetes auth + Vault Secrets Operator (KinD cluster)
hal vault k8s enable \
  --kind-node-image kindest/node:v1.31.1 \
  --vso-chart-version 0.8.1 \
  --vault-k8s-web-backend-image httpd --vault-k8s-web-backend-tag 2.4-alpine \
  --vault-k8s-web-proxy-image nginx --vault-k8s-web-proxy-tag alpine

hal vault k8s update
hal vault k8s disable

# PKI secrets engine (Root CA + Intermediate CA)
hal vault pki enable --k8s              # also deploy cert-manager + web demo on KinD
hal vault pki enable --acme             # also deploy Caddy ACME demo on KinD
hal vault pki enable                    # auto-selects SoftHSM2 managed keys on an ent-hsm deployment
hal vault pki enable --hsm              # assert that HSM-backed CAs are available
hal vault pki enable --no-hsm           # force software-backed CAs on an HSM deployment
hal vault pki update --k8s
hal vault pki update --acme
hal vault pki update --acme --acme-cert-ttl 2m  # shorten cert TTL for live renewal demo
hal vault pki disable

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
hal boundary create --boundary-tag 0.15.2
hal boundary status
hal boundary delete

# SSH target VM (Multipass)
hal boundary ssh enable --ubuntu-image 22.04 --cpus 1 --mem 512M
hal boundary ssh update
hal boundary ssh disable

# MariaDB target
hal boundary mariadb enable --boundary-mariadb-tag 11.4
hal boundary mariadb enable --boundary-mariadb-tag 11.4 --with-vault    # link Vault dynamic creds
hal boundary mariadb disable

# Observability
hal boundary obs create
hal boundary obs delete
```

---

### Consul (`hal consul`)

```bash
hal consul create --consul-tag 1.15.0
hal consul status
hal consul delete

hal consul obs create
hal consul obs status
hal consul obs delete
```

---

### AAP + Vault OIDC/SSH (`hal vault aap`)

```bash
hal vault create
hal vault aap enable
hal vault aap update
hal vault aap disable
```

---

### Nomad (`hal nomad`)

```bash
hal nomad create --ubuntu-image 22.04 --nomad-version 1.11.3 --cpus 2 --mem 2G
hal nomad status
hal nomad delete

hal nomad obs create
hal nomad obs delete
```

---

### Terraform Enterprise (`hal terraform` / `hal tf` / `hal tfe`)

**Primary TFE instance**

```bash
hal terraform create \
  --tfe-tag 1.2.0 \
  --tfe-pg-tag 16-alpine \
  --tfe-redis-tag 7-alpine \
  --tfe-minio-tag latest \
  --minio-api-port 19000 \
  --minio-console-port 19001 \
  --tfe-proxy-tag alpine

hal terraform status
hal terraform update
hal terraform delete
```

**Twin TFE instance** — reuses the primary ecosystem (PostgreSQL, Redis, MinIO)

```bash
hal terraform create --target twin --twin-tag 1.2.0
hal terraform status --target twin
hal terraform update --target twin
hal terraform delete --target twin
```

**Feature subcommands**

```bash
# VCS-driven workspace workflow (local GitLab integration)
hal terraform vcs-workflow enable
hal terraform vcs-workflow enable --gitlab-image gitlab/gitlab-ce --gitlab-tag 18.10.1-ce.0
hal terraform vcs-workflow enable --gitlab-port 8929   # if host port 8080 is taken
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

# SAML SSO via Authentik IdP
hal terraform saml enable               # deploy Authentik + configure TFE SAML
hal terraform saml enable --target twin # configure SAML for the twin TFE instance
hal terraform saml update               # re-provision (new cert, config drift)
hal terraform saml status               # show IdP + TFE SAML health
hal terraform saml disable              # tear down SAML; stop Authentik if unused
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

Deploys a PLG stack (Prometheus, Loki, Grafana, Promtail) on `hal-net` with pre-wired datasources and a Vault audit log scraper.

```bash
hal obs create                  # deploy with default image tags
hal obs create \
  --loki-tag 3.7 \
  --grafana-tag main \
  --prometheus-tag main \
  --promtail-tag 3.6
hal obs status
hal obs update                  # full stack reconcile (tear down + redeploy)
hal obs delete
```

**Stack flags**

| Flag | Default | Purpose |
|---|---|---|
| `--loki-tag` | `3.7` | Tag for the Loki image |
| `--loki-image` | `grafana/loki` | Loki image name |
| `--grafana-tag` | `main` | Tag for the Grafana image |
| `--grafana-image` | `grafana/grafana` | Grafana image name |
| `--prometheus-tag` | `main` | Tag for the Prometheus image |
| `--prometheus-image` | `prom/prometheus` | Prometheus image name |
| `--promtail-tag` | `3.6` | Tag for the Promtail image |
| `--promtail-image` | `grafana/promtail` | Promtail image name |
| `--prom-config-path` | _(generated)_ | Path to a hand-crafted `prometheus.yml`; skips the generated config entirely |

#### Adding a custom metrics endpoint

Two ways to register a custom Prometheus scrape job — both work on `create` and `update` without touching or restarting the rest of the stack:

**Option A — inline flags** (`--job-name`): HAL writes the job block into `prometheus.yml` and creates a target file placeholder at `~/.hal/obs/targets/<job-name>.json`. Edit that file to point at your real endpoint.

```bash
# Register a job, then edit the target file with the real host:port
hal obs update \
  --job-name my-app \
  --metrics-path /metrics \
  --metrics-token eyJ...   # optional bearer token

# Target file written to: ~/.hal/obs/targets/my-app.json
# Default content: [{"targets":["host:port"],"labels":{"job":"my-app"}}]
```

**Option B — JSON scrape config file** (`--scrape-config-path`): Provide a JSON file with a full job definition. HAL merges it verbatim into `prometheus.yml`, preserving any field Prometheus supports (scheme, TLS config, http_headers, static_configs, etc.).

```bash
# my-job.json — bare job object:
# {
#   "job_name": "hcpt",
#   "metrics_path": "/v1/sys/metrics",
#   "params": {"format": ["prometheus"]},
#   "static_configs": [{"targets": ["hcpt.example.com:8200"]}]
# }

hal obs update --scrape-config-path ./my-job.json

# Also accepted: {"scrape_configs": [{...}]} wrapper format
```

After either option, Prometheus receives a live `SIGHUP` reload — no container restart needed.

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

### HAL Plus (`hal plus`)

HAL Plus runs the local web UI container, keeps HAL MCP on the same network, and now handles the common Ollama model UX on the host.

```bash
hal plus create                      # default preset: gemma4
hal plus create --model qwen3.5     # build and use the qwen3.5 preset
hal plus create --model gemma4 --keep-alive 5m
hal plus create --model qwen3.5 --model-config ./Modelfile
hal plus status
hal plus delete
```

Notes:

- `--model` accepts either a curated preset (`gemma4`, `qwen3.5`) or an existing host Ollama model name.
- For known presets, HAL creates a HAL-managed Ollama model with balanced defaults instead of requiring a manual `ollama pull` step.
- `--model-config` lets you point HAL at a custom Ollama `Modelfile`; HAL will build a managed model on the host and then wire HAL Plus to use it.
- `--keep-alive` controls how long Ollama keeps the model loaded after requests so idle memory can fall back down.
- **Qwen3.5** (`--model qwen3.5`) is the recommended preset for HAL Plus — it has a 32k context window and produces concise, grounded answers. HAL Plus uses `think: false` for operational answers to keep latency low.
- **Gemma4** (`--model gemma4`) is a lighter alternative with faster cold starts, suited for machines with less VRAM.
- Ollama must be running on the host (`ollama serve`) before `hal plus create` is called. HAL Plus connects to it from inside the container via `host.containers.internal:11434`.

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
| Vault (dev) | http://vault.localhost:8200 |
| Vault Enterprise (`--mode prod`) | https://vault.localhost:8200 |
| Consul | http://consul.localhost:8500 |
| Boundary | http://boundary.localhost:9200 |
| Terraform Enterprise | https://tfe.localhost:8443 |
| Terraform Enterprise Admin | https://tfe.localhost:8444 |
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
- **`hal delete`** (global teardown) removes all HAL-managed containers, volumes, VMs, and the `hal-net` Docker network. There is a confirmation prompt but the action is not reversible. If `hal-net` cannot be removed (non-HAL containers still attached), the command exits with an error listing the blockers.
- **TFE requires a valid license.** `hal terraform create` expects a Terraform Enterprise license to be in place. The stack will start but TFE itself will not activate without one.
- **Vault Enterprise prod mode requires a license.** `hal vault create --mode prod` needs `VAULT_LICENSE` (or `VAULT_LICENSE_PATH`) and will not boot without one. It serves HTTPS with a self-signed cert — accept the browser warning, or set `VAULT_CACERT=~/.hal/vault-prod/certs/cert.pem` for the CLI. The saved unseal key + root token in `~/.hal/vault-prod/init.json` are the **only** copy; losing that file leaves a sealed, unrecoverable Vault. Feature integrations that embed URLs into Vault (OIDC callbacks, PKI AIA/CRL, the OS plugin register) are currently validated against **dev mode**; enabling them against a prod (TLS) instance may need manual URL adjustment.
- **CSI mode for `hal vault k8s`** requires a Vault Enterprise binary. HAL will detect the edition at runtime and fall back to native mode automatically.
- **Image and tag overrides are opt-in.** Every `create` / `enable` command exposes `--<component>-image` (registry + name) and `--<component>-tag` (version) flags independently. Use them to pull from a private mirror, pin a specific version, or test a custom build.
- **`--network-subnet`** (global flag) pins the subnet when `hal-net` is created for the first time (e.g. `hal --network-subnet 10.89.3.0/24 tf create --enable`). Useful on Rancher Desktop or any engine that assigns an unexpected default subnet that conflicts with static proxy IPs.

---

## Contributing & Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for branch naming, the doc-sync rule, and
conventions before opening a pull request.

```bash
# From the repo root
go build -o hal main.go   # build the binary
go build ./...            # verify all packages compile
go test ./...             # run the full test suite
```

Before changing command behavior or UX patterns, read these files in order:

1. `docs/cli-lifecycle-model.md` — authoritative lifecycle verb model
2. `docs/vault-oracle-plugin-build.md` — building the Oracle database plugin from source (arm64/amd64)
3. `.github/copilot-instructions.md` — concise policy and architecture notes
4. `LLM_CONTEXT.md` — LLM-oriented command guidance

Keep all three in sync when adding or renaming commands.
