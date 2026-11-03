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
- HSM-backed PKI is auto-selected on an `ent-hsm` deployment; `--hsm` asserts
  that mode is available and `--no-hsm` opts out.

## Prerequisites
- HAL CLI is available in your local environment.
- `hal vault create` has been run and Vault is healthy at `http://127.0.0.1:8200`.
- For `--k8s`: `kind`, `kubectl`, and `helm` must be installed and in PATH.
- For HSM-backed CAs: deploy first with
  `hal vault create --edition ent-hsm --mode prod`.

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

Private keys are Vault-internal by default. On a detected Enterprise HSM build,
they are generated through a managed key in the SoftHSM2 PKCS#11 token instead.

### Flags
```text
--root-mount string        Vault mount path for Root CA (default "pki-root")
--int-mount string         Vault mount path for Intermediate CA (default "pki-int")
--root-ttl string          Max TTL for Root CA (default "43800h" = 5y)
--int-ttl string           Max TTL for Intermediate CA (default "17520h" = 2y)
--allowed-domains string   Allowed domains for hal-role (default "hal.local,cluster.local,svc.cluster.local")
--max-cert-ttl string      Maximum TTL for issued leaf certs (default "24h")
--k8s                      Also deploy cert-manager + web demo on KinD (see below)
--force                    With update --k8s/--acme: also rebuild Root CA and Intermediate CA from scratch
--kind-node-image string   KinD node image used only when creating a new cluster (default "kindest/node:v1.36.1")
--cert-manager-version     Jetstack cert-manager Helm chart version (empty = latest)
--vault-pki-web-backend-image string   Demo backend container image name (default "nginx")
--vault-pki-web-backend-tag string     Demo backend container image tag (default "alpine")
--acme                     Deploy Vault ACME endpoint + Caddy demo on KinD (enable/update only)
--acme-cert-ttl string     TTL for certs issued to Caddy via ACME (default "5m")
--vault-pki-caddy-image string   Caddy container image name (default "caddy")
--vault-pki-caddy-tag string     Caddy container image tag (default "alpine")
--hsm                      Require HSM-backed CAs; error if Vault is not an HSM build
--no-hsm                   Force software-backed CAs even on an HSM build
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
| 1 | Reuse or create the shared KinD cluster on `hal-net` via `ensureHALKindCluster()` (NodePort 30082 → host 8089) |
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

If the HSM layer is active, disable also deletes the managed key
`sys/managed-keys/pkcs11/hal-kms-root` after the mounts are gone and wipes the
SoftHSM token data from the `hal-vault-data` volume. The running HSM runtime is
left untouched; it belongs to the Vault create/delete lifecycle.

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
- If the HSM layer is active: deletes the managed key and wipes SoftHSM token data; the runtime container/image remain in place

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
| 3 | Reuse or create the shared KinD cluster on `hal-net` via `ensureHALKindCluster()` (all port mappings declared upfront via shared `writeHALKindConfig()`) |
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

## HSM auto-detection and overrides

Deploy the HSM-capable runtime with:

```bash
hal vault create --edition ent-hsm --mode prod
```

The edition selects the Enterprise HSM source binary, builds
`hal-vault-softhsm:latest`, and boots Vault with `kms_library` configured from
the first start. PKI detects the running `+ent.hsm` version and uses HSM-backed
CAs by default. `--hsm` is a hard assertion; `--no-hsm` forces software CAs.

When HSM mode is selected, the CA chain uses managed keys instead of
Vault-internal keys:

| Step | What happens |
|------|--------------|
| 1 | Confirm prod mode, the HAL SoftHSM runtime, and Enterprise license reachability |
| 2 | Initialise SoftHSM2 token under `/vault/data/softhsm/tokens`; reuse it when the configured label exists |
| 3 | Register Vault managed key `sys/managed-keys/pkcs11/hal-kms-root` (mechanism `0x0001` CKM_RSA_PKCS, 4096-bit, `allow_generate_key`) |
| 4 | Mount `pki-root`/`pki-int` with `allowed_managed_keys`, generate Root CA via `/root/generate/kms` and Intermediate CA via `/intermediate/generate/kms` |

All private-key operations for both CAs are delegated to the PKCS#11 token — the keys are generated on first use inside SoftHSM and never leave it.

**Update/teardown semantics:** an HSM-backed `update` deletes and re-registers the
managed key while reusing the token. `update --no-hsm` and `disable` delete the
managed key and wipe token data. Neither command replaces the container or
removes the runtime image; `hal vault delete` owns image reclamation.

```bash
# HSM-backed CA chain (Vault Enterprise HSM build, prod mode)
hal vault create --edition ent-hsm --mode prod
hal vault pki enable                  # auto-detected HSM mode
hal vault pki enable --hsm            # same path, with a hard assertion
hal vault pki enable --no-hsm         # explicit software-key opt-out

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
- HSM mode writes SoftHSM token data into the `hal-vault-data` volume and registers managed key `sys/managed-keys/pkcs11/hal-kms-root`.
- `disable` deletes the managed key and token data but leaves the create-owned runtime intact.

---

## Command summary
```text
hal vault pki [status|enable|disable|update]

Flags (mode selection and enable/update):
  --root-mount string         Vault mount path for Root CA (default "pki-root")
  --int-mount string          Vault mount path for Intermediate CA (default "pki-int")
  --root-ttl string           Root CA max TTL (default "43800h" = 5y)
  --int-ttl string            Intermediate CA max TTL (default "17520h" = 2y)
  --allowed-domains string    Allowed domains for hal-role
  --max-cert-ttl string       Max TTL for leaf certs (default "24h")
  --k8s                       Deploy cert-manager + web demo on KinD (with --pqc, select its own NGINX lab)
  --force                     With update --k8s/--acme: also rebuild Root CA and Intermediate CA
  --kind-node-image string    KinD node image (default "kindest/node:v1.36.1")
  --cert-manager-version      Jetstack chart version (empty = latest)
  --vault-pki-web-backend-image string   Demo backend image name (default "nginx")
  --vault-pki-web-backend-tag string     Demo backend image tag (default "alpine")
  --acme                      Deploy Vault ACME endpoint + Caddy demo on KinD
  --acme-cert-ttl string      TTL for ACME certs (default "5m", sets both config/acme max_ttl and role TTL)
  --vault-pki-caddy-image string   Caddy image name (default "caddy")
  --vault-pki-caddy-tag string     Caddy image tag (default "alpine")
  --hsm                       Require HSM-backed CAs; error if Vault is not an HSM build
  --no-hsm                    Force software-backed CAs on an HSM build
  --softhsm-* flags           Token label / PINs / key labels / managed-key name
  --pqc                       Use isolated ML-DSA-65 mutual TLS and ML-KEM-768 lab
  --pqc-cert-ttl string       PQC workload certificate TTL (default "10s", range 10s–24h)
  --pqc-image string          Stock NGINX image with njs and PQC TLS support (pinned digest by default)
  --pqc-cluster-domain string Kubernetes Service DNS domain (default "cluster.local")

Global flags: --debug, --dry-run, --network-subnet, --verbose
```

## Post-quantum Kubernetes mutual TLS

Tracked in [issue #110](https://github.com/hashimiche/hal/issues/110).

Both pods use the **official NGINX image**, pinned by digest. There is no custom
container build, C workload, or locally compiled OpenSSL. HAL mounts configuration,
a static dashboard and a small njs evidence/policy module into stock NGINX.

```text
Browser -- conventional ECDSA HTTPS --> frontend NGINX
frontend NGINX -- ML-DSA-65 mTLS + MLKEM768, TLS 1.3 --> backend NGINX
```

### Prerequisites

Create Vault through the normal HAL product workflow, selecting a supported
Enterprise image tag and supplying a valid license:

```sh
hal vault create --edition ent --vault-tag <supported-tag>
hal vault pki enable --pqc
```

Vault must be unsealed, licensed Enterprise **2.2.0 or later** with ML-DSA-65
PKI API support. HAL checks version and license before provisioning. Enable
creates its own isolated PKI mounts and checks their live API schemas before
CA generation; no diagnostic mount or conventional PKI setup is required.
If provisioning fails, newly created mounts are removed; existing owned mounts
are preserved. The feature never upgrades or replaces a running Vault.
HSM availability is not evidence of PQC support; ML-DSA CA keys are generated
internally in Vault independently of SoftHSM.

Install kind, kubectl, Docker or Podman, and host OpenSSL with ML-DSA X.509
support, which HAL uses to validate issued certificates before deployment.
HAL supplies the pinned stock NGINX image, including OpenSSL and njs;
these do not need to be installed separately on the host. Tested runtime
versions and limitations are recorded in the findings below.

`--pqc` implies the Kubernetes deployment. Explicit `--k8s` remains accepted
for compatibility; status and the default action remain read-only.

### Deploy and operate

```sh
go build -o hal main.go
hal vault pki enable --pqc --dry-run
hal vault pki enable --pqc
hal vault pki status --pqc
hal vault pki update --pqc
hal vault pki update --pqc --pqc-cert-ttl 10s # ten-second certificate demo
hal vault pki disable --pqc
```

The demo HTML and JavaScript are embedded in the HAL binary. After editing
`cmd/vault/pki-pqc/`, rebuild with `go build -o hal main.go`, then run
`./hal vault pki update --pqc` to deploy the updated assets. Recreating the lab
with an older binary deploys its older embedded page.

No lifecycle action means read-only status. Enable preserves a completed
deployment; update reconciles roles, issues new leaves, applies configuration and
restarts workloads so TLS contexts reload. Root and intermediate CAs are retained.
Interrupted enable can be repeated. Conventional PKI, ACME, HSM, mount-tuning and
force flags are rejected with PQC before mutation. Dry-run creates no keys,
state or infrastructure.

`--pqc-image` overrides the official distribution image. The tested default
digest, also defined in `cmd/vault/defaults.go`, is:

```text
docker.io/library/nginx@sha256:1dd3048a04f4b76ebd706c1bbb9df7d9d53b4f8253b32ce14467088c9b5ada0f
```

Overrides need the NGINX SSL variables, OpenSSL algorithms and bundled HTTP njs
module used by this lab. Rerun the opt-in integration suite when changing the
image. HAL pulls or reuses the image locally for compatibility checks.
Kubernetes pulls the selected image directly from its registry. Overrides must
be accessible from the cluster as well as the host; no compiler toolchain is
needed.
`--kind-node-image` controls new cluster nodes. `--pqc-cluster-domain` must match
an existing cluster's CoreDNS Kubernetes zone; new HAL clusters use `cluster.local`.

New shared KinD clusters map host 8093 to NodePort 30085. Existing clusters are
preserved. If their original configuration lacks that mapping, run:

```sh
kubectl --context kind-kind -n hal-pqc port-forward --address 127.0.0.1 service/frontend 8093:8443
```

Open <https://localhost:8093> or <https://pqc.localhost:8093>, then click
**Test connection**. The frontend verifies the backend's
chain and full Service DNS name and presents its own ML-DSA client chain.
Both endpoints require TLS 1.3, `mldsa65` and the standalone `MLKEM768` group;
session reuse is disabled so evidence belongs to a full handshake. The workload
hop has no X25519 component. Browser HTTPS remains conventional.

### Short-lived workload certificates

`--pqc-cert-ttl` sets workload role and leaf TTLs (default `10s`, range `10s`
to `24h`). Both workloads automatically request a fresh Vault-issued identity
at about half the remaining lifetime: normally every five seconds. NGINX/njs
stores each certificate/key pair atomically in shared memory and selects it for
new TLS handshakes. No pod restart, Kubernetes Secret propagation, cert-manager
or manual update is needed for rotation. The roles disable certificate storage
to avoid accumulating thousands of short-lived leaves in Vault.

Click **Test connection**, wait about five seconds, and click it again. The
page compares the verified frontend serial with the previous successful test,
shows **Certificate rotated**, and updates the observed expiry and countdown.
The countdown describes that test's certificate; it does not poll the current
identity. Backend expiry remains unavailable from the proxy. The separate browser
HTTPS certificate retains its existing lifetime.

A periodic task checks every second and retries failed issuance without replacing
the last good identity. Expired identities fail TLS verification; readiness also
fails when no current identity remains. Restore Vault connectivity/authorization
to recover automatically. Logs report rotation serials and failures without keys,
tokens or Vault response bodies. Ten-second TTLs are intended for local demos.

Startup leaf files are offline diagnostic snapshots, not the identities served
by the pods. HAL verifies each snapshot at one second before its signed expiry
so startup latency cannot consume its ten-second lifetime. Signature, chain, EKU
and backend DNS checks remain enforced. Root and intermediate validity are also
checked against the current time. Live workload TLS always checks current expiry.

### Browser trust

HAL creates a separate self-signed ECDSA P-256 certificate for `pqc.localhost`
and `localhost`, at `~/.hal/pqc-pki/browser.pem`. It never installs a host trust
anchor. Inspect and explicitly accept a browser exception or import that
certificate if desired. Remove manually imported trust yourself when finished.
Update preserves this certificate while over one day of validity remains;
otherwise it rotates it and trust must be re-established.

Without changing host trust:

```sh
curl --cacert ~/.hal/pqc-pki/browser.pem https://localhost:8093/
curl --cacert ~/.hal/pqc-pki/browser.pem -X POST https://localhost:8093/test
```

JavaScript cannot directly inspect browser TLS parameters. After a test NGINX
reports protocol, cipher and group from the actual browser request; the panel
labels certificate settings as configuration.

### Ownership and trust model

`hal-pqc-root` and `hal-pqc-int` hold internal ML-DSA-65 CAs. Frontend and backend
roles issue separate clientAuth-only and serverAuth-only leaves, respectively.
The backend has DNS SANs for `backend`, `backend.hal-pqc`, `backend.hal-pqc.svc`
and the full Service DNS name. DigitalSignature is the only key usage.

Namespace `hal-pqc` owns Deployments/Services `frontend` and `backend`, their
per-workload issuance-token Secrets, the `browser` Secret, the `trust` ConfigMap and two configuration
ConfigMaps. Each pod receives a token restricted to its own issuance role and
self-renewal, and obtains its own leaf key/presentation chain from Vault. The
frontend also receives the conventional browser identity and both trust the root.
These periodic orphan tokens have one-hour periods, omit the default policy, and
renew automatically. Local accessors allow disable to revoke them, including
tokens created by interrupted updates. Updates create new scoped tokens; previous
unused tokens expire after their last period. If workloads remain stopped beyond
the one-hour token period, run `update --pqc` to provide fresh issuance credentials. HAL verifies production Vault HTTPS
through a loopback-only proxy using the Vault CA and server name.
No CA keys or Vault administrative credentials enter either pod, and no Vault
Kubernetes auth mount is needed. Pods run without service-account tokens or
privilege escalation, with a read-only root filesystem and bounded writable /tmp.

Local state lives under `~/.hal/pqc-pki` (0700 directory, 0600 files) with an owner
marker. Secrets are applied through stdin with server-side apply, avoiding
credentials in arguments and last-applied annotations. Mount descriptions,
namespace labels and local markers prevent adoption of unowned resources.
Disable removes lab-owned mounts, issuance tokens/policies, namespace and state; it preserves shared
KinD and distribution images. Other lab teardown guards recognize `hal-pqc`.
Product/global cleanup handles final cluster/state cleanup. Legacy custom image
caches from the earlier implementation are removable by global cleanup.

Vault administrators and Kubernetes Secret readers can impersonate workloads.
The dedicated CA and constrained issuance roles delimit trust; this lab does
not implement separate application authorization or revocation checking. Its
NGINX/njs periodic task renews the default ten-second workload identities.

### What custom code remains, and why

[backend.conf](../../cmd/vault/pki-pqc/backend.conf) delegates authentication and key exchange to
NGINX/OpenSSL. [frontend.conf](../../cmd/vault/pki-pqc/frontend.conf) delegates verified HTTPS
proxying and browser HTTPS to NGINX.

[rotation.js](../../cmd/vault/pki-pqc/rotation.js) handles scoped issuance, token
renewal and atomic identity publication; NGINX/OpenSSL still performs TLS.
[evidence.js](../../cmd/vault/pki-pqc/evidence.js) formats connection evidence as
JSON safely, decodes algorithm OIDs/SAN/EKU fields from **NGINX's verified peer
certificate**, and rejects a peer lacking explicit clientAuth EKU. NGINX's
normal purpose check rejects wrong EKU but treats an absent EKU as unrestricted;
the small additional policy preserves the lab's stricter requirement. It does
not implement certificate signatures, trust-chain verification or TLS, read
identity from headers, or treat parsing as verification.

### Evidence semantics

The demo page shows a Browser → Frontend → Backend connection diagram and the
frontend certificate's observed remaining lifetime. **Test connection** verifies
mutual ML-DSA-65 authentication and ML-KEM-768 key exchange, updates the diagram,
and reports certificate rotation when the observed serial changes. The countdown
and moving markers reflect the last successful test, not live traffic. Failed
tests clear the displayed verification state. Browser HTTPS and workload mTLS
remain separate. Expand **Raw connection evidence / JSON** for the original
response, including unavailable metadata reported as `null`.

| Reported field | Source |
| --- | --- |
| subject, issuer, serial and validity | NGINX verified-client SSL variables |
| public key/signature algorithms, SANs, EKUs | Decoded from `$ssl_client_raw_cert`, only after `$ssl_client_verify == SUCCESS` |
| chain_verified | Actual NGINX/OpenSSL verification result for this client connection |
| trust_anchor | `null`: verified-chain anchor is not exposed by these bindings |
| protocol, cipher_suite, key_exchange_group | `$ssl_protocol`, `$ssl_cipher`, `$ssl_curve` |
| peer_handshake_signature_scheme | `$ssl_client_sigalg`, which calls `SSL_get0_peer_signature_name()` |
| local_handshake_signature_scheme | `$ssl_sigalg`, which reports this connection's server handshake signature |
| frontend_verification | Successful NGINX HTTPS proxy with chain/DNS verification enabled; wrong-hostname/untrusted-root tests prove enforcement |
| frontend_observed_backend | `null`: HTTP proxy bindings do not expose upstream certificate/session metadata |
| browser_connection | NGINX SSL variables for the browser's actual `/test` request |

Backend observations establish both ML-DSA handshake signatures and the standalone
`MLKEM768` group **on the same connection**. Frontend upstream metadata is not
fabricated or copied from certificate files. The full chain anchor is not guessed
from configured trust. Readiness/status and cipher-suite names alone never prove
post-quantum authentication or key establishment.

### Troubleshooting

Use `kubectl --context kind-kind -n hal-pqc logs deploy/frontend` and
`logs deploy/backend`. A proxy verification failure returns 502 with guidance;
check chain, DNS SAN, EKU, expiry and runtime algorithms. Never disable verification.
An incompatible image or missing njs module must fail startup; it is not a reason
to relabel conventional TLS. Existing stopped clusters must be started explicitly;
never recreate one while other lab namespaces exist.

The HTTP readiness endpoint checks that an unexpired local workload identity
is available. Readiness is not authentication evidence.

### Interoperability findings

The prior 2026-10-20 run on macOS arm64 used Vault `2.2.0+ent.hsm`,
NGINX 1.31.3, OpenSSL 3.5.7 and bundled njs 1.0.0 from the official image
pinned in `cmd/vault/defaults.go`; the host client used OpenSSL 3.6.3.
Vault-issued ML-DSA-65 mutual authentication and X25519MLKEM768 negotiation
succeeded between two pods on the existing Kubernetes v1.31.1 cluster.
The browser hop used a separate conventional ECDSA identity.
These are historical observations, not fresh validation of subsequent changes.

The stock NGINX tests passed valid authentication and rejected missing,
untrusted, expired, wrong-EKU and absent-EKU clients; incorrect backend hostname,
untrusted backend root and classical-only groups were also rejected. Negative
requests did not reach the authenticated backend handler. NGINX may finish TLS
and return HTTP 400 for a rejected certificate rather than send a TLS alert.
Interactive browser automation, new-cluster provisioning and destructive global
cleanup were not verified by that run. That historical implementation had no renewal controller or revocation checks.
The current implementation adds automatic identity rotation.

### Fresh deployment validation

A subsequent 2026-10-20 run used Vault `2.2.0+ent` in HAL dev mode and a newly
created Kubernetes v1.36.1 KinD cluster on Podman/macOS arm64. Issuance and offline
verification succeeded. The initial rollout exposed stale container-runtime
aliases caused by importing the host image archive. HAL now lets Kubernetes
pull the pinned official image directly; the affected node alias was repaired
without replacing Vault or KinD, and enable completed successfully.

A certificate-verified POST through the new cluster's direct host port 8093
confirmed the frontend's verified HTTPS proxy, both `mldsa65` handshake
signatures and `X25519MLKEM768` on the same backend connection. The full CLI and
stock NGINX positive/negative suite passed against the fresh dev-mode Vault.
All four required Go checks passed. The fixed code has not subsequently been
tested by recreating another cluster from scratch. Interactive browser automation
and destructive cleanup remain unverified by these checks.

### Automatic rotation validation (2026-10-22)

The existing production Vault HTTPS / KinD deployment was upgraded in place.
Both NGINX workloads became ready with ten-second roles and automatic issuance.
Twelve consecutive certificate-verified requests over roughly twelve seconds
succeeded across three observed frontend serials, changing about every five
seconds. Each response confirmed both `mldsa65` signatures, `X25519MLKEM768`,
verified client identity and frontend chain/hostname verification. Runtime logs
also confirmed recurring backend issuance. No CA rebuild was required.

Go package tests cover token scope, credential file permissions, deployment
rendering and revocation. Node tests cover rotation cadence, matched key/cert
snapshots, issuance failure/recovery, expired readiness, token renewal and
concurrent invocation guards; page tests cover first, changed, unchanged and
failed connection states. Interactive browser validation and destructive live
teardown were not performed for this change.

### Standalone ML-KEM-768 validation (2026-10-28)

The current configuration pins `MLKEM768` at both workload TLS endpoints and
requires that exact observed group in the backend policy and browser evidence.
The pinned stock NGINX image completed a temporary two-container frontend →
backend mTLS request using locally issued ML-DSA-65 certificates. The response
confirmed chain and hostname verification, both `mldsa65` handshake signatures,
and `MLKEM768` from the backend's `$ssl_curve`. The actual backend config rejected
clients offering only `X25519MLKEM768` or `X25519`. The Go vault and MCP package
tests and the page test passed. The Vault-issued, two-pod integration suite has
not been rerun for this change.

### Contributor integration validation

The owning Vault package exposes one opt-in test, alongside its normal unit tests:

```sh
HAL_PQC_INTEGRATION=1 go test ./cmd/vault -run '^TestPQCIntegration$' -v -count=1
```

It requires a running licensed Vault Enterprise 2.2.0+, host OpenSSL with
ML-DSA X.509 support, curl and a container engine (`HAL_PQC_TEST_ENGINE`, default
`podman`). It uses `VAULT_ADDR`, `VAULT_TOKEN` and `VAULT_CACERT`, falling back
to HAL production state. `HAL_PQC_TEST_IMAGE` overrides the normal pinned image.
Artifacts and private keys are generated in the Go test temporary directory,
never in the checkout. The test neither creates nor tears down KinD.
It creates process-specific diagnostic Vault mounts, refuses existing mounts,
and removes only successfully created diagnostic mounts in reverse order on exit.
No CA keys are exported. Local ML-DSA issuance is used only for untrusted
negative fixtures. The suite checks actual CLI handshakes and the stock NGINX
backend and frontend proxy, including explicit clientAuth EKU policy.

On 2026-10-27, this suite passed against a licensed Vault `2.2.0+ent` production
deployment on code revision `7723af1`, using macOS arm64, host OpenSSL 3.6.3,
Podman client 5.8.0 / Linux arm64 engine 5.5.2 and the pinned NGINX 1.31.3
image with OpenSSL 3.5.7 and njs 1.0.0. All positive, negative and frontend
proxy cases passed. The diagnostic Vault mounts and NGINX containers were gone
after the run. This suite does not create a KinD cluster or exercise the HAL
Kubernetes deployment lifecycle. The 2026-10-20 and 2026-10-22 deployment
results above remain historical. No result is recorded for other operating
systems, Vault or Kubernetes versions, container engines or image overrides.
