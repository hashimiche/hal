# HAL CLI - Project Context for LLM Assistance

## 📌 Project Overview
Hashicorp Academy Labs, `hal`, is a Go-based CLI tool built with the Cobra framework. It acts as an orchestrator for local HashiCorp product sandboxes (Vault, Boundary, Consul, Nomad, Terraform, plus Observability). It relies heavily on Docker/KinD/Multipass to deploy infrastructure and uses native Go API clients to configure them.

## 🛠️ Tech Stack & Dependencies
- **Language:** Go 1.26+
- **CLI Framework:** Cobra (`github.com/spf13/cobra`)
- **Infrastructure Engine:** Docker/Podman (`exec.Command` via `global.DetectEngine()`), KinD, Multipass.
- **APIs:** HashiCorp Go SDKs (e.g., `github.com/hashicorp/vault/api`).
- **CI/CD:** GitHub Actions (Cross-compiles for macOS/Linux `.tar.gz` and Windows `.zip`), Homebrew Tap compatible.

## 📐 Architectural Rules & Patterns (CRITICAL)

### 1. The 2-Tier Command Architecture
We strictly separate Infrastructure (Core Products) from Configuration (Features) to prevent CLI flag collisions.
* **Tier 1 (Core Products):** Use explicit VERB subcommands (`deploy`, `destroy`, `status`). Do not use short flags here.
    * *Example:* `hal vault deploy`, `hal boundary destroy`
* **Tier 2 (Features/Integrations):** Use NOUN subcommands with lifecycle flags (`-e/--enable`, `-d/--disable`, `-f/--force`).
    * *Example:* `hal vault oidc -e`, `hal boundary mariadb -d`

### 2. Smart Status Default (Read-Only First)
If a user runs a Tier 1 or Tier 2 command without explicit action flags, the CLI MUST default to a read-only **Smart Status** dashboard.
* Do not blindly dump the Cobra `--help` menu.
* Inspect the Docker engine and product API to determine the current state (Up/Down/Degraded).
* Always conclude the status output with a `💡 Next Step:` block that provides the exact copy-pasteable command the user needs to advance or fix their state.

### 3. The "Known Universe" Teardown Pattern
When implementing a `destroy.go` command for a product, use the "Nuke from Orbit" approach. 
* Define a package-level array of all containers and volumes associated with that product (e.g., `var vaultEcosystem = []string{"hal-vault", "hal-keycloak"}`).
* Loop through this array and execute `rm -f`. 
* Bypass the product's API for teardowns to ensure speed and prevent hanging on crashed containers.

### 4. UI & Output Guidelines
* **Balanced Emoji Usage:** Use emojis as visual anchors (⚙️ for processing, ✅ for success, ❌ for errors, ⚠️ for warnings, 🚀/🔗 for key actions/links, ⚪ for offline/undeployed states).
* **Suppress Engine Noise:** When querying Docker container states (e.g., `docker inspect`), use `exec.Command(...).Output()` instead of `.CombinedOutput()` to prevent Docker's internal stderr messages ("No such object") from bleeding into the beautiful CLI UI.

### 5. Docker & System Gotchas (Lessons Learned)
* **The Bolted Pipe (Volumes):** Docker physically prevents deleting a volume while its attached container is running. For feature `--disable` commands, you cannot `docker volume rm`. Instead, `exec` into the container and `rm -f` the data inside the volume to achieve a clean slate.
* **Port Mapping:** When exposing privileged ports (like `389` for LDAP) on rootless Docker/Podman engines, map to a higher port on the host (e.g., `-p 1389:389`), but retain the internal port (`389`) in the code when containers talk to each other across the Docker bridge network.
* **Rosetta 2 Panics:** Older Go-based binaries (like `cfssl` inside `osixia/openldap`) crash on Apple Silicon (M-series) Macs under Rosetta. Bypass where possible.
* **Carriage Returns:** When passing multi-line strings (like LDIF files) from Go into a Linux container via `exec.Command`, you MUST run `strings.ReplaceAll(text, "\r", "")` first. CRLF endings cause silent failures.

## 🚀 Current Implementation Status
* **Core:** Cobra `rootCmd`, `catalogCmd`, Engine detection, Autocompletion enabled.
* **Vault:** Fully refactored to Smart Status. Includes `deploy`, `destroy`, `k8s`, `jwt`, `oidc`, `audit`, and `ldap`.
* **Boundary:** Fully refactored. Core Control Plane + `mariadb` and `ssh` targets.
* **Terraform:** Fully refactored. Local FDO deployment with S3/Redis/PG mocking.
* **Nomad & Consul:** Refactored to Smart Status. Multipass VM and Docker standalone respectively. Includes `--join-consul` tethering.
* **Observability:** PLG Stack (Prometheus, Loki, Grafana, Promtail) refactored.

## 🤖 Instructions for the AI
When generating new code for this project:
1.  Read this context file thoroughly.
2.  Adhere strictly to the 2-Tier Architecture and Smart Status UX patterns.
3.  Ensure state checking uses `.Output()` to suppress `stderr` spam.
4.  If a user asks to implement a new HashiCorp product or feature, establish the exact same lifecycle scaffolding as the existing suite.