# 🔴 HAL - Hashicorp Academy Labs

**HAL** is a fast, local orchestrator for HashiCorp product sandboxes. Built in Go, it instantly spins up complex, containerized lab environments (Vault, Boundary, Consul, Nomad, Terraform, and Observability) right on your local machine using Docker, KinD, and Multipass.

No more writing massive `docker-compose` files just to test an OIDC integration or a Boundary target. Ask HAL to do it for you.

## 🚀 Installation

### macOS & Linux (Homebrew)
The easiest way to install HAL is via our custom Homebrew tap:

```bash
brew tap hashimiche/tap
brew install hal
```

### Windows & Manual Installation
Download the pre-compiled binaries from the [Releases page](https://github.com/hashimiche/hal/releases).

---

## 🧠 The "Smart Status" Architecture

HAL is built around a **Read-Only First** philosophy. If you ever forget what is running, just type the name of the product. HAL will dynamically scan your Docker containers, Multipass VMs, and local APIs to give you a real-time dashboard and suggest your next move.

```text
$ hal boundary mariadb

🔍 Checking Boundary MariaDB Target Status...

  ❌ MariaDB Target : Not deployed

💡 Next Step:
   hal boundary mariadb --enable
```

---

## 🛠️ Core Commands

HAL operates on a strict **2-Tier Architecture**:
1. **Tier 1 (Core Products):** Use explicit verbs (`deploy`, `destroy`, `status`) to spin up the heavy infrastructure.
2. **Tier 2 (Features & Targets):** Use flags (`-e`, `-d`, `-f`) to attach integrations to the running products.

### 🔐 Vault
Deploy a fully initialized and unsealed Vault instance.
* `hal vault deploy` (Starts Core Vault)
* `hal vault oidc -e` (Spins up Keycloak and configures Vault OIDC)
* `hal vault k8s -e` (Spins up a KinD cluster and configures Kubernetes Auth)
* `hal vault destroy` (Nukes everything)

### 🚪 Boundary
Deploy a Boundary Control Plane and attach mock targets.
* `hal boundary deploy` (Starts Controller + Postgres Backend)
* `hal boundary mariadb -e` (Spins up a dummy MariaDB target)
* `hal boundary ssh -e` (Spins up an Ubuntu Multipass VM target)

### 🌍 Consul & Nomad
Deploy a local Control Plane and tether Nomad to it.
* `hal consul deploy` (Starts standalone Consul server)
* `hal nomad deploy --join-consul` (Spins up Nomad VM and tethers it to Consul)

### ☁️ Terraform
Deploy a mock Terraform Enterprise (FDO) environment.
* `hal terraform deploy` (Starts mock S3, Redis, and Postgres DB)

### 📊 Observability (PLG Stack)
Deploy a full telemetry stack to monitor your HashiCorp tools.
* `hal obs deploy` (Starts Prometheus, Loki, Grafana, and Promtail)

---

## 📋 Prerequisites
Depending on the labs you intend to run, HAL requires the following engines:
* **Docker** or **Podman** (Required for almost everything)
* **KinD** (Required for `hal vault k8s`)
* **Multipass** (Required for `hal nomad` and `hal boundary ssh`)

## 🤝 Contributing
Pull requests are welcome! If you want to add a new HashiCorp product or feature, please read the `LLM_CONTEXT.md` file to understand the CLI's state-machine architecture and UI guidelines.