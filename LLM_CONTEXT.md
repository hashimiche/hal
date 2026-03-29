# HAL 9000 CLI - Project Context for LLM Assistance

## 📌 Project Overview
"HAL 9000" (`hal`) is a Go-based CLI tool built with the Cobra framework. It acts as an orchestrator for local HashiCorp product sandboxes (Vault, Boundary, Consul, Nomad, Terraform). It relies heavily on Docker/KinD to deploy infrastructure and uses native Go API clients to configure them.

## 🛠️ Tech Stack & Dependencies
- **Language:** Go 1.26+
- **CLI Framework:** Cobra (`github.com/spf13/cobra`)
- **Infrastructure Engine:** Docker/Podman (`exec.Command` via `global.DetectEngine()`), KinD.
- **APIs:** HashiCorp Go SDKs (e.g., `github.com/hashicorp/vault/api`).
- **CI/CD:** GitHub Actions (Cross-compiles for macOS/Linux `.tar.gz` and Windows `.zip`), Homebrew Tap compatible.

## 📐 Architectural Rules & Patterns (CRITICAL)

### 1. UI Guidelines
- **Balanced Emoji Usage:** Use emojis to provide clear visual anchors and scanability (⚙️ for processing, ✅ for success, ❌ for errors, ⚠️ for warnings, 🚀/🔗 for key actions/links). Do not overuse them; keep the output clean and professional.
- Provide clear, well-formatted next-step instructions (with examples) at the end of successful deployments.

### 2. The Pre-Flight Health Check Pattern
All subcommands that interact with a product's API MUST use a centralized helper function to ensure the container is alive before executing logic. 
*Example for Vault:* We use `GetHealthyClient()` in `helper.go` to initialize the client, set the root token, and ping `/sys/health`.

### 3. The Teardown & Force Patterns (`-d` and `-f`)
All subcommands must support `--destroy` (`-d`) and `--force` (`-f`).
- **Deploy:** Fail-fast if the required API (like Vault) is offline.
- **Destroy:** Wipe containers first, then gracefully ignore API cleanup steps if the API is unreachable.
- **Force:** Execute the Destroy logic, but do NOT return; continue directly into the Deploy logic for a clean rebuild.

### 4. Docker & System Gotchas (Lessons Learned)
- **Port Mapping:** When exposing privileged ports (like `389` for LDAP) on rootless Docker/Podman engines, map to a higher port on the host (e.g., `-p 1389:389`), but retain the internal port (`389`) in the code when containers talk to each other across the Docker bridge network.
- **Rosetta 2 Panics:** Older Go-based binaries (like `cfssl` inside `osixia/openldap`) crash on Apple Silicon (M-series) Macs under Rosetta. Bypass where possible (e.g., passing `-e LDAP_TLS=false`).
- **Carriage Returns:** When passing multi-line strings (like LDIF files or Scripts) from Go into a Linux container via `exec.Command`, you MUST run `strings.ReplaceAll(text, "\r", "")` first. CRLF endings from code editors will cause silent failures inside Linux configurations.
- **Wait Loops:** Never assume a container is ready the millisecond `docker run` finishes. Always implement a finite retry loop (e.g., 10 retries, 3 seconds apart) for database initialization commands, printing `.` for visual feedback.

## 🚀 Current Implementation Status
* **Core:** Cobra `rootCmd`, `catalogCmd`, Engine detection, Autocompletion enabled.
* **Vault:** Fully implemented. Includes `deploy` (CE/ENT), `destroy`, `k8s` (CSI and Native sync), `jwt` (GitLab CI/CD), `oidc` (Keycloak), `audit` (Loki sockets), and `ldap` (Dynamic, Static, Library pools + Root rotation).
* **Next Up:** Boundary (`cmd/boundary/deploy.go`).

## 🤖 Instructions for the AI
When generating new code for this project:
1. Read this context file thoroughly.
2. Adhere strictly to the established patterns (`GetHealthyClient`, `-d`/`-f` flags, explicit error handling).
3. Follow the UI Guidelines for balanced emoji usage.
4. If a user asks to implement a new HashiCorp product, establish the same lifecycle scaffolding as Vault.