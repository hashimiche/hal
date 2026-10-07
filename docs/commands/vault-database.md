# `hal vault database` Command Spec

## Command
- `hal vault database [status|enable|disable|update]`
- Alias: `hal vault db`

## Purpose
Deploy a database backend (MariaDB or Oracle) and configure Vault's dynamic database secrets engine so Vault mints short-lived, Just-In-Time (JIT) database credentials on demand. Optionally (`--k8s`) extends the workflow into a full KinD + Vault Secrets Operator demo where a live web app receives and rotates those credentials automatically via `VaultDynamicSecret`.

> **Shared MariaDB:** `hal-vault-mariadb` is a shared service counted per consumer (ADR 0004), like Authentik and GitLab. This command registers as `vault-database`; the Agentic IAM lab registers as `vault-agentic-iam`, with its own Vault mount and broker user. `enable` reuses the container when it is running, and `disable` removes it only when no other consumer remains. See [Shared MariaDB](#shared-mariadb).

> **Auth mount isolation:** the `--k8s` flag uses a dedicated `kubernetes-db/` Vault auth mount — never the shared `kubernetes/` mount used by `hal vault k8s`. This means `disable` is safe regardless of which other `--k8s` features are running, and the cluster is preserved if `app1`, `pki-demo`, or `pki-acme-demo` namespaces are still active.

## Related
- Parent namespace: [vault.md](vault.md)
- Shared KinD cluster: `hal vault k8s`, `hal vault pki --k8s`

## Prerequisites
- `hal vault create` has been run and Vault is healthy at `http://127.0.0.1:8200`.
- For `--backend oracle`: Vault Enterprise + `--oracle-plugin-path` binary.
- For `--k8s`: `kind`, `kubectl`, and `helm` must be installed and in PATH.

---

## Lifecycle: `hal vault database enable`

Deploys the selected database backend and wires Vault's database secrets engine:

| Step | What happens |
|------|-------------|
| 1 | Start the database container on `hal-net`. MariaDB: reuse `hal-vault-mariadb` if it is already running, then register the `vault-database` consumer |
| 2 | Wait for the database to be ready to accept connections |
| 3 | MariaDB: revoke and unmount an earlier `database/`, so its JIT users are dropped from a reused container |
| 4 | Create a least-privileged broker account (`vaultadmin` for MariaDB, reset if it already exists; `vault` user for Oracle) |
| 5 | Enable Vault database secrets engine at `database/` |
| 6 | Configure the database connection (`database/config/<container-name>`) |
| 7 | Rotate the broker account password — Vault takes exclusive ownership, nobody knows it |
| 8 | Create a dynamic role with SQL creation/revocation statements (`database/roles/<role>`) |
| 9 | Generate a test JIT credential to verify the full chain works |
| 10 | Print the credential and a ready-to-paste `mysql` login command |

### Flags
```text
-b, --backend string               Database backend (mariadb, oracle; pgsql planned) (default "mariadb")
    --vault-mariadb-image string   MariaDB container image name, ignored when the shared container is reused (default "mariadb")
    --vault-mariadb-tag string     MariaDB container image tag, ignored when the shared container is reused (default "11.8")
    --username-prefix string       Prefix for generated usernames e.g. "myapp" → "myapp-AbCdEfGhIj" (default "v")
    --oracle-image string          Oracle Database Free image (default "gvenzl/oracle-free")
    --oracle-tag string            Oracle Database Free tag (default "slim")
    --oracle-plugin-path string    Path to vault-plugin-database-oracle binary (required for oracle)
    --oracle-plugin-version string Version string to register with Vault (default "0.14.1+ent")
    --k8s                          Also deploy KinD + VSO with VaultDynamicSecret (see --k8s section below)
```

### Examples
```bash
# MariaDB (default)
hal vault database enable

# MariaDB with pinned image
hal vault database enable --vault-mariadb-tag 11.8

# Oracle (Enterprise only)
hal vault database enable --backend oracle \
  --oracle-plugin-path /path/to/vault-plugin-database-oracle

# MariaDB + KinD + VSO live rotation demo
hal vault database enable --k8s
```

---

## Lifecycle: `hal vault database update`

Tears down the existing database environment and re-enables it from scratch. Equivalent to `disable` followed by `enable`. Use when the database container or Vault configuration has drifted. With MariaDB, the container is recreated only when no other consumer holds it; otherwise it is reused and only this command's mount and broker are reset.

```bash
hal vault database update
hal vault database update --k8s   # also reconciles the KinD cluster (recreates if port map is stale)
```

---

## Lifecycle: `hal vault database disable`

Revokes all active Vault database leases, unmounts the `database/` engine, and removes the database container. With MariaDB, it also drops the `vaultadmin` broker and deregisters `vault-database`; `hal-vault-mariadb` is removed only when no other consumer remains.

```bash
hal vault database disable
hal vault database disable --k8s   # also destroys the KinD cluster and cleans up kubernetes auth + policy
```

---

## Lifecycle: `hal vault database status`

Checks (shown by default when no lifecycle action is given):
- Whether `database/` is mounted in Vault
- MariaDB container running (with its shared-service consumers) + Vault config exists
- Oracle runtime image built, plugin present, container running, Vault config exists

```bash
hal vault database status
# or simply:
hal vault database
```

---

## Demonstrating JIT credentials (without --k8s)

After `enable`, Vault can issue fresh database credentials on demand. Each call creates a new user in the database with a TTL of 2 minutes — the user is automatically deleted when the lease expires.

**Request a JIT credential:**
```bash
VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN=root \
  vault read database/creds/dba-role
```

**Log in to MariaDB with the JIT credential:**
```bash
mysql -h mariadb.localhost -P 3306 -u <username> -p<password>
```

**Watch credentials rotate in real time (terminal):**
```bash
watch -n 5 'VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN=root vault read database/creds/dba-role'
```

**Check active leases:**
```bash
VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN=root \
  vault list sys/leases/lookup/database/creds/dba-role
```

**Revoke all database leases manually:**
```bash
VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN=root \
  vault lease revoke -force -prefix database/creds/
```

---

## `--k8s` flag — VSO Dynamic Database Credential Demo

When `--k8s` is passed to `enable` or `update`, the following additional steps run after the database secrets engine is fully configured:

| Step | What happens |
|------|-------------|
| 1 | Boot a KinD cluster (or reuse an existing one) — port `30084` → host port `8091` |
| 2 | Create namespace `db-app` and service account `db-app-sa` |
| 3 | Configure `vault-reviewer` SA for Kubernetes TokenReview (shared, reused if present) |
| 4 | Enable Vault Kubernetes auth at **`kubernetes-db/`** (dedicated mount, never touches `kubernetes/`), configure with KinD CA + reviewer token |
| 5 | Write policy `db-app-read` (read-only on `database/creds/<role>`) |
| 6 | Create Vault auth role `db-app-role` bound to `db-app-sa` in `db-app` namespace |
| 7 | Install Vault Secrets Operator via Helm in namespace `vso` |
| 8 | Wait for `VaultDynamicSecret` CRD to be established and VSO controller pods to be Ready |
| 9 | Apply `VaultConnection`, `VaultAuth`, and `VaultDynamicSecret` manifests |
| 10 | Deploy 2-replica httpd app + nginx reverse proxy |
| 11 | Expose demo at `http://db.localhost:8091` (no `kubectl port-forward` needed) |

### How the rotation works

```
VSO controller
  └─ reads VaultDynamicSecret (mount: database, path: creds/dba-role)
       └─ calls Vault → Vault runs CREATE USER 'v-xxxx' in MariaDB
            └─ writes username + password into K8s Secret "hal-db-creds"
                 └─ triggers rolling restart of hal-db-app deployment
                      └─ new pods start with DB_USERNAME / DB_PASSWORD env vars
                           └─ renders live credentials on http://db.localhost:8091
```

At TTL expiry (~15 seconds), VSO requests a **new** credential. Vault creates a new user, rotates the Secret, and the old user is automatically deleted from MariaDB.

### Why `default_ttl = max_ttl = 15s`

Setting both to the same value makes the lease **non-renewable**. When VSO tries to renew at 67% of TTL (~10s), Vault refuses — so VSO is forced to call `database/creds/dba-role` fresh each time, creating a genuinely new username and password (not just resetting the same lease clock). This makes the rotation visible in real time during a demo.

### --k8s flags
```text
--db-kind-node-image string    KinD node image (default "kindest/node:v1.36.1")
--db-vso-chart-version string  Helm chart version for vault-secrets-operator (empty uses latest)
--db-app-image string          Demo app container image name (default "httpd")
--db-app-tag string            Demo app container image tag (default "2.4-alpine")
--db-proxy-image string        Demo proxy container image name (default "nginx")
--db-proxy-tag string          Demo proxy container image tag (default "alpine")
```

### Accessing the demo

Open in a browser — no port-forward needed:
```
http://db.localhost:8091
```

The page shows:
- Current `username` (green) and `password` (red) minted by Vault
- The database label and role name
- A **LOGIN COMMAND** block with the complete `mysql` (or `sqlplus`) command pre-filled with the current live credentials
- A **Copy login command** button — click it, paste into a terminal to log in as the current JIT user

### Watching credential rotation in the terminal

```bash
# Watch the K8s Secret update — username changes every ~10s
watch -n 2 'kubectl get secret hal-db-creds -n db-app \
  -o jsonpath="{.data.username}" | base64 -d; echo'

# Or stream with timestamps
while true; do
  echo "$(date +%T)  $(kubectl get secret hal-db-creds -n db-app \
    -o jsonpath='{.data.username}' | base64 -d)"
  sleep 3
done

# Watch pod rolling restarts triggered by VSO
kubectl get pods -n db-app -w

# Watch the VaultDynamicSecret status and last renewal time
kubectl get vaultdynamicsecret hal-db-dynamic -n db-app -w
```

### Logging in to MariaDB with a JIT credential

1. Open `http://db.localhost:8091` in a browser
2. Click **Copy login command**
3. Paste into a terminal:
```bash
mysql -h mariadb.localhost -P 3306 -u v-AbCdEfGhIj -pW4-bSgidzAWMEk2UkHbI
```
4. You are logged in as that short-lived user
5. Reload the page — within ~10–15 seconds the username and password will change to a new JIT user; the previous user no longer exists in MariaDB

### Inspecting the VSO resources

```bash
# Check VaultDynamicSecret sync status and current lease
kubectl get vaultdynamicsecret hal-db-dynamic -n db-app -o yaml

# Check the current credential secret
kubectl get secret hal-db-creds -n db-app \
  -o jsonpath='{.data}' | python3 -m json.tool

# Decode username and password directly
kubectl get secret hal-db-creds -n db-app \
  -o jsonpath='{.data.username}' | base64 -d; echo
kubectl get secret hal-db-creds -n db-app \
  -o jsonpath='{.data.password}' | base64 -d; echo

# Check VSO controller logs
kubectl logs -n vso -l app.kubernetes.io/name=vault-secrets-operator --tail=40

# Check app pod logs
kubectl logs -n db-app -l app=hal-db-app --tail=20
```

### Shared KinD cluster

The `--k8s` flag shares the same KinD cluster used by `hal vault k8s` and `hal vault pki --k8s`. Bring-up goes through `ensureHALKindCluster()` so the node is on `hal-net`. Port mappings declared in the shared `writeHALKindConfig()`:

| Host port | KinD NodePort | Used by |
|-----------|--------------|---------|
| `8088` | `30080` | `hal vault k8s` — VSO static secret demo |
| `8089` | `30082` | `hal vault pki --k8s` — cert-manager demo |
| `8090` | `30083` | `hal vault pki --acme` — Caddy ACME demo |
| `8091` | `30084` | `hal vault database --k8s` — dynamic DB credential demo |

> **Important:** If a KinD cluster already exists from a previous `hal vault k8s enable` run (without port 30084 mapped), run `hal vault database update --k8s` to recreate it with the full port map. KinD bakes port mappings in at cluster creation time.

---

## Shared MariaDB

`hal-vault-mariadb` is registered under the `vault-mariadb` key of `~/.hal/shared-services.json`. Each consumer owns its own Vault mount and broker user, so disabling one never breaks another.

| Consumer | Command | Vault mount | Broker user |
|----------|---------|-------------|-------------|
| `vault-database` | `hal vault database` (MariaDB) | `database/` | `vaultadmin` |
| `vault-agentic-iam` | `hal vault agentic-iam` (planned) | its own | its own |

- Root keeps the fixed lab password for the container's whole life, so a reused container can always be administered. Vault's `rotate-root` only ever rotates a consumer's broker.
- A container that is stopped is restarted, not recreated, so the other consumers' data survives. A fresh container resets the consumer list.
- `hal boundary mariadb --with-vault` is not a consumer: it creates nothing in MariaDB and brokers `database/creds/dba-role`, so it needs `hal vault database` itself.
- `hal vault delete` removes the container and clears the `vault-mariadb` consumers.

---

## Side Effects

- Starts `hal-vault-mariadb` (MariaDB, reused when already running) or `hal-vault-oracle-db` (Oracle) container on `hal-net`
- Mounts `database/` secrets engine in Vault
- Writes `database/config/<container-name>` and `database/roles/<role-name>`
- Rotates the broker account password — Vault holds exclusive ownership, it is not recoverable from outside Vault
- `--k8s`: enables dedicated `kubernetes-db/` Vault auth mount, writes `db-app-read` policy and `db-app-role`
- `--k8s`: installs `vault-secrets-operator` Helm release in namespace `vso`
- `--k8s`: creates namespace `db-app`, service account `db-app-sa`, and all VSO CRDs
- `disable` tears down all of the above, except `hal-vault-mariadb` while another consumer still uses it

---

## Command summary

```text
hal vault database [status|enable|disable|update]
Alias: hal vault db

Flags:
  -b, --backend string               Database backend (mariadb, oracle; pgsql planned) (default "mariadb")
      --vault-mariadb-image string   MariaDB container image name, ignored on reuse (default "mariadb")
      --vault-mariadb-tag string     MariaDB container image tag, ignored on reuse (default "11.8")
      --username-prefix string       Dynamic username prefix (default "v")
      --oracle-image string          Oracle Free image (default "gvenzl/oracle-free")
      --oracle-tag string            Oracle Free tag (default "slim")
      --oracle-plugin-path string    Path to vault-plugin-database-oracle binary
      --oracle-plugin-version string Plugin version to register (default "0.14.1+ent")
      --k8s                          Deploy KinD + VSO with VaultDynamicSecret rotation demo
      --db-kind-node-image string    KinD node image for --k8s (default "kindest/node:v1.36.1")
      --db-vso-chart-version string  VSO Helm chart version for --k8s (empty = latest)
      --db-app-image string          Demo app image for --k8s (default "httpd")
      --db-app-tag string            Demo app tag for --k8s (default "2.4-alpine")
      --db-proxy-image string        Demo proxy image for --k8s (default "nginx")
      --db-proxy-tag string          Demo proxy tag for --k8s (default "alpine")

Global flags: --debug, --dry-run, --verbose
```
