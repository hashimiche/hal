# `hal vault pki` Command Spec

## Command
- `hal vault pki [status|enable|disable|update]`

## Purpose
Manage Vault PKI secrets engines for a two-tier CA hierarchy (Root CA → Intermediate CA). Optionally (`--k8s`) deploys cert-manager to a KinD cluster so pods can receive Vault-issued TLS certificates via a `ClusterIssuer`.

## Position in hal vault
PKI is a Vault feature, not a standalone product. It lives under `hal vault pki` alongside `hal vault k8s`, `hal vault jwt`, `hal vault ldap`, etc.

## Related
- Vault must be running: `hal vault create`
- KinD cluster (shared with or independent of `hal vault k8s`): used only when `--k8s` is passed
- `--acme` flag: enables Vault's built-in ACME endpoint, deploys Caddy on KinD, live cert-renewal web page
- `--hsm` flag: backs the CA keys with a SoftHSM2 PKCS#11 token via a Vault managed key (requires Vault Enterprise HSM build, prod mode)

## Prerequisites
- HAL CLI is available in your local environment.
- `hal vault create` has been run and Vault is healthy at `http://127.0.0.1:8200`.
- For `--k8s`: `kind`, `kubectl`, and `helm` must be installed and in PATH.

---

## Lifecycle: `hal vault pki enable`

Enables two Vault PKI secrets engines and builds a two-tier CA chain:

| Step | What happens |
|------|-------------|
| 1 | Mount `pki-root` (max TTL 43800h = 5 years) |
| 2 | Generate Root CA (RSA-4096, internal key) |
| 3 | Configure CRL/issuing URLs for `pki-root` |
| 4 | Mount `pki-int` (max TTL 17520h = 2 years) |
| 5 | Generate Intermediate CSR (RSA-4096, internal key) |
| 6 | Sign CSR with Root CA |
| 7 | Install signed cert on `pki-int` |
| 8 | Configure CRL/issuing URLs for `pki-int` |
| 9 | Create role `hal-role` on `pki-int` (domains: `hal.local`, `cluster.local`, `svc.cluster.local`, max TTL 24h) |
| 10 | Write policy `hal-pki-issuer` (allows signing/issuing via `pki-int/sign/hal-role` and `pki-int/issue/hal-role`) |
| 11 | Enable Vault ACME endpoint (`pki-int/config/acme`), set `max_ttl` = `acme-cert-ttl`, create role `acme-demo` (`allow_any_name`, `ttl`/`max_ttl` = `acme-cert-ttl`) |

Private keys are **Vault-internal** — they never appear on disk.

### Flags
```text
--root-mount string        Vault mount path for Root CA (default "pki-root")
--int-mount string         Vault mount path for Intermediate CA (default "pki-int")
--root-ttl string          Max TTL for Root CA (default "43800h" = 5y)
--int-ttl string           Max TTL for Intermediate CA (default "17520h" = 2y)
--allowed-domains string   Allowed domains for hal-role (default "hal.local,cluster.local,svc.cluster.local")
--max-cert-ttl string      Maximum TTL for issued leaf certs (default "24h")
--k8s                      Also deploy cert-manager + web demo on KinD (see below)
--force                    With update --k8s: also rebuild Root CA and Intermediate CA from scratch
--kind-node-image string   KinD node image used only when creating a new cluster (default "kindest/node:v1.31.1")
--cert-manager-version     Jetstack cert-manager Helm chart version (empty = latest)
--web-backend-image        Demo backend container image (default "nginx:alpine")
--acme                     Deploy Vault ACME endpoint + Caddy demo on KinD (enable/update only)
--acme-cert-ttl string     TTL for certs issued to Caddy via ACME (default "5m")
--caddy-image string       Caddy container image (default "caddy:alpine")
--hsm                      Use SoftHSM2 PKCS#11 managed keys for both CAs (Vault Enterprise HSM build)
--vault-hsm-tag string     vault-enterprise tag for the HSM build (default "2.0.3-ent.hsm", must end in "-ent.hsm")
--softhsm-base-image       Base image for the SoftHSM runtime build (default "debian", must be glibc-based)
--softhsm-base-tag         Base image tag for the SoftHSM runtime build (default "12-slim")
--softhsm-token-label      SoftHSM2 token label (default "hal-hsm-token")
--softhsm-pin              SoftHSM2 user PIN (default "1234")
--softhsm-so-pin           SoftHSM2 SO PIN (default "0000")
--softhsm-managed-key      Vault managed-key name (default "hal-kms-root")
--softhsm-key-label        PKCS#11 key label for the CA signing key (default "hal-pki-root-key")
--softhsm-hmac-label       PKCS#11 key label for the HMAC key (default "hal-pki-root-hmac-key")
```

### Examples
```bash
# PKI engines only
hal vault pki enable

# PKI engines + cert-manager + web demo
hal vault pki enable --k8s

# Custom cert-manager version
hal vault pki enable --k8s --cert-manager-version 1.12.3
```

---

## `--k8s` flag (enable / update)

When `--k8s` is passed to `enable` or `update`, the following additional steps run after the PKI CA setup:

| Step | What happens |
|------|-------------|
| 1 | Reuse existing KinD cluster or create a new one (NodePort 30082 → host port 80) |
| 2 | Deploy cert-manager via Jetstack OCI chart (`oci://quay.io/jetstack/charts/cert-manager`) with CRDs, `webhook.hostNetwork=true`, and `webhook.securePort=10260` |
| 3 | Enable dedicated Vault Kubernetes auth at `kubernetes-pki/` (always fresh, never shared with `kubernetes/`) |
| 4 | Configure `vault-reviewer` SA and `cert-manager-vault` SA |
| 5 | Generate a K8s SA-bound token (8760h) and store it as secret `vault-k8s-token` in `cert-manager` namespace |
| 6 | Create Vault role `cert-manager-role` on `kubernetes-pki/` (policy: `hal-pki-issuer`) |
| 7 | Apply `ClusterIssuer vault-pki-issuer` pointing to `<vault-ip>:8200` |
| 8 | Apply `Certificate hal-web-pki-cert` in namespace `pki-demo` (24h TTL, renewBefore 1h) |
| 9 | Deploy `hal-web-pki` (`nginx:alpine`) that mounts the TLS secret at `/tls` and renders cert details |
| 10 | Expose via `NodePort 30082` |

### Auth mount isolation

The `kubernetes-pki/` auth mount is always separate from the `kubernetes/` mount used by `hal vault k8s`. This means:
- `hal vault pki disable` can cleanly remove `kubernetes-pki/` without affecting `hal vault k8s`.
- If both features are enabled simultaneously, the KinD cluster is shared but each feature controls only its own auth mount.

---

## Lifecycle: `hal vault pki update`

`update` behavior depends on which flags are present:

| Command | CA engines | cert-manager | Caddy/ACME |
|---|---|---|---|
| `update` | ♻️ Rebuilt from scratch | — | — |
| `update --k8s` | ✅ Preserved as-is | ♻️ Reconciled | — |
| `update --acme` | ✅ Preserved as-is | — | ♻️ Role TTL synced + pod restarted |
| `update --k8s --force` | ♻️ Rebuilt from scratch | ♻️ Reconciled | — |
| `update --acme --force` | ♻️ Rebuilt from scratch | — | ♻️ Full rebuild |

- **`update`** (no flags): unmounts `pki-root`/`pki-int` and fully rebuilds the CA chain. Use when you need fresh root/intermediate keys.
- **`update --k8s`**: leaves the PKI engines untouched — only reconciles the cert-manager Helm release, re-configures the `kubernetes-pki/` auth mount, and re-applies all K8s resources. Fails fast if `pki-int` is not mounted (run `enable` first).
- **`update --k8s --force`**: full teardown and rebuild — CAs, cert-manager, and all K8s resources from scratch.

```bash
hal vault pki update                       # rebuild CAs only
hal vault pki update --k8s                 # reconcile cert-manager, preserve CAs
hal vault pki update --k8s --force         # rebuild everything
hal vault pki update --acme                # re-sync role TTL, restart Caddy
hal vault pki update --acme --acme-cert-ttl 2m   # change TTL to 2m
hal vault pki update --acme --force        # full CA rebuild + redeploy Caddy
```

---

## Lifecycle: `hal vault pki disable`

Unmounts `pki-root` and `pki-int`, removes the `hal-pki-issuer` policy, disables the `kubernetes-pki/` auth mount, and — if cert-manager was previously deployed — uninstalls it and removes the `pki-demo` namespace.

KinD cluster is only deleted if `hal vault k8s` is not active (checks for the `vault-secrets-operator` Helm release in the `vso` namespace).

```bash
hal vault pki disable
```

### What disable always does
- Unmounts `pki-root` and `pki-int` from Vault
- Disables `kubernetes-pki/` auth mount
- Deletes `hal-pki-issuer` policy
- Checks for cert-manager — if found, also deletes `ClusterIssuer`, `pki-demo` namespace, `cert-manager` Helm release, and `cert-manager` namespace
- Conditionally deletes KinD cluster (preserved if `hal vault k8s` is still active)

---

## Lifecycle: `hal vault pki status`

Checks (shown even without flags):
- Whether `pki-root` and `pki-int` are mounted in Vault
- Whether Root CA certificate is present
- Whether Intermediate CA is signed and installed
- Whether the `hal-role` role exists on `pki-int`
- KinD cluster state
- cert-manager deployment, `ClusterIssuer vault-pki-issuer` readiness, `Certificate hal-web-pki-cert`, and web pod state

```bash
hal vault pki status
# or simply:
hal vault pki
```

---

## Manual cert operations (after enable)

**Issue a leaf certificate:**
```bash
vault write pki-int/issue/hal-role \
  common_name="test.hal.local" \
  ttl="24h"
```

**Read Root CA certificate:**
```bash
vault read -field=certificate pki-root/cert/ca
```

**Read Intermediate CA certificate:**
```bash
vault read -field=certificate pki-int/cert/ca
```

**List issued certificates:**
```bash
vault list pki-int/certs
```

**Access the cert-manager web demo (after --k8s):**
```bash
# → https://pki.localhost:8089  (no port-forward needed)
```

**Inspect the issued TLS certificate:**
```bash
kubectl describe certificate hal-web-pki-cert -n pki-demo
kubectl get secret hal-web-pki-tls -n pki-demo \
  -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -noout -text
```

---

## `--acme` flag (enable / update)

When `--acme` is passed, the following additional steps run after PKI CA setup:

| Step | What happens |
|------|--------------|
| 1 | Vault ACME endpoint enabled (`pki-int/config/acme`, `max_ttl` = `acme-cert-ttl`) |
| 2 | Role `acme-demo` created/updated: `allow_any_name=true`, `ttl`/`max_ttl` = `acme-cert-ttl` |
| 3 | Reuse existing KinD cluster or create a new one (all 3 port mappings declared upfront via shared `writeHALKindConfig()`) |
| 4 | `fetch-ca` init container downloads Vault Root CA PEM from `pki-int/ca/pem` |
| 5 | `build-page` init container writes live countdown HTML page to `/srv` |
| 6 | Caddy main container: `apk add openssl`, `caddy start`, then watches `/data/caddy/certificates` for cert changes, writes `cert-info.txt` + `cert-pem.txt` to `/srv` |
| 7 | Caddy uses global `acme_ca` + `acme_ca_root` Caddyfile directives to force Vault ACME (overrides Caddy's local CA fallback for `.localhost` domains) |
| 8 | `kubectl rollout restart` forces fresh pod on every `update --acme` (clears cert cache, forces new ACME exchange) |
| 9 | Wait up to 90s for Caddy pod Ready (ACME exchange on first start) |

### Web page
The page at `https://acme.localhost:8090` shows:
- Live countdown to cert expiry (updates every 500ms)
- Progress bar (blue → amber → red as cert ages)
- `🔄 Renewed!` badge flashes for 8s when Caddy auto-renews (serial change detection)
- Full `openssl x509 -noout -text` output
- Raw PEM

### TTL behaviour (Vault 2.x)
Vault's ACME layer has a `config/acme max_ttl` that defaults to `2160h` and overrides the role TTL. Both `config/acme max_ttl` **and** the role `ttl`/`max_ttl` must be set to the desired value — `--acme-cert-ttl` controls both.

```bash
# default 5m TTL
hal vault pki enable --acme

# custom TTL
hal vault pki enable --acme --acme-cert-ttl 2m

# change TTL on running demo (restarts Caddy to force fresh cert)
hal vault pki update --acme --acme-cert-ttl 3m
```

### Access
```
https://acme.localhost:8090
```

### Inspect
```bash
kubectl exec -n pki-acme-demo deploy/hal-caddy-acme -- ls /data/caddy/certificates/
kubectl logs -n pki-acme-demo deploy/hal-caddy-acme
```

---

## `--hsm` flag (enable / update)

**Requires the Vault Enterprise HSM build** (image tag ending in `-ent.hsm` — the PKCS#11 subsystem is compiled in only for these tags) **running in prod mode** (`hal vault create --mode prod`; dev mode never loads the `kms_library` config block).

When `--hsm` is passed, the CA chain is built with HSM-backed keys instead of Vault-internal keys:

| Step | What happens |
|------|--------------|
| 1 | Build local image `hal-vault-softhsm:latest` (debian:12-slim + SoftHSM2 + Vault binary from `hashicorp/vault-enterprise:<--vault-hsm-tag>`) — skipped if present unless `update` |
| 2 | Rewrite `~/.hal/vault-prod/vault.hcl` with a `kms_library "pkcs11"` block pointing at `libsofthsm2.so`, restart `hal-vault` on the new image, unseal with cached keys |
| 3 | Initialise SoftHSM2 token (`softhsm2-util --init-token`) under `/vault/data/softhsm/tokens` (persists in the `hal-vault-data` volume); reuse existing token if the label already exists |
| 4 | Register Vault managed key `sys/managed-keys/pkcs11/hal-kms-root` (mechanism `0x0001` CKM_RSA_PKCS, 4096-bit, `allow_generate_key`) |
| 5 | Mount `pki-root`/`pki-int` with `allowed_managed_keys`, generate Root CA via `/root/generate/kms` and Intermediate CA via `/intermediate/generate/kms` |

All private-key operations for both CAs are delegated to the PKCS#11 token — the keys are generated on first use inside SoftHSM and never leave it.

```bash
# HSM-backed CA chain (Vault Enterprise HSM build, prod mode)
hal vault create --mode prod --vault-tag 2.0.3-ent.hsm
hal vault pki enable --hsm

# Inspect the managed key afterwards
vault read sys/managed-keys/pkcs11/hal-kms-root
```

---

## Side Effects
- Mounts two PKI secrets engines in Vault (`pki-root`, `pki-int` by default).
- Writes policy `hal-pki-issuer` to Vault.
- `--k8s`: enables dedicated `kubernetes-pki/` auth mount, deploys cert-manager, creates cluster-scoped `ClusterIssuer`.
- `--k8s`: stores a K8s SA-bound token as a Kubernetes secret in `cert-manager` namespace.
- `--acme`: enables `pki-int/config/acme`, creates `acme-demo` role, deploys Caddy pod in `pki-acme-demo` namespace.
- `--hsm`: builds local image `hal-vault-softhsm:latest`, replaces the `hal-vault` container with it, rewrites `~/.hal/vault-prod/vault.hcl` (adds `kms_library` block), writes SoftHSM token data into the `hal-vault-data` volume, registers managed key `sys/managed-keys/pkcs11/hal-kms-root`.
- `disable` tears down all of the above automatically, **except** the `--hsm` layer: the managed key, SoftHSM token data, and `hal-vault-softhsm` image/container are currently left in place.

---

## Command summary
```text
hal vault pki [status|enable|disable|update]

Flags (enable/update):
  --root-mount string         Vault mount path for Root CA (default "pki-root")
  --int-mount string          Vault mount path for Intermediate CA (default "pki-int")
  --root-ttl string           Root CA max TTL (default "43800h" = 5y)
  --int-ttl string            Intermediate CA max TTL (default "17520h" = 2y)
  --allowed-domains string    Allowed domains for hal-role
  --max-cert-ttl string       Max TTL for leaf certs (default "24h")
  --k8s                       Deploy cert-manager + web demo on KinD
  --force                     With update --k8s: also rebuild Root CA and Intermediate CA
  --kind-node-image string    KinD node image (default "kindest/node:v1.31.1")
  --cert-manager-version      Jetstack chart version (empty = latest)
  --web-backend-image         Demo backend image (default "nginx:alpine")
  --acme                      Deploy Vault ACME endpoint + Caddy demo on KinD
  --acme-cert-ttl string      TTL for ACME certs (default "5m", sets both config/acme max_ttl and role TTL)
  --caddy-image string        Caddy container image (default "caddy:alpine")
  --hsm                       SoftHSM2 PKCS#11 managed keys for both CAs (Vault Enterprise HSM build, prod mode)
  --vault-hsm-tag string      vault-enterprise tag for the HSM build (default "2.0.3-ent.hsm")
  --softhsm-* flags           Token label / PINs / key labels / managed-key name / base image overrides

Global flags: --debug, --dry-run
```
 Vault PKI teardown complete.
💡 Next Step: hal vault pki enable
❌ Vault mount 'pki-int' not found. Run 'hal vault pki enable' first.
   Use 'hal vault pki update --k8s --force' to rebuild everything from scratch.   