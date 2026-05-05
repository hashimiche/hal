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
- `--acme` flag (future): ACME HTTP challenge integration — not yet implemented

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

| Command | CA engines | cert-manager |
|---|---|---|
| `update` | ♻️ Rebuilt from scratch | — |
| `update --k8s` | ✅ Preserved as-is | ♻️ Reconciled |
| `update --k8s --force` | ♻️ Rebuilt from scratch | ♻️ Reconciled |

- **`update`** (no flags): unmounts `pki-root`/`pki-int` and fully rebuilds the CA chain. Use when you need fresh root/intermediate keys.
- **`update --k8s`**: leaves the PKI engines untouched — only reconciles the cert-manager Helm release, re-configures the `kubernetes-pki/` auth mount, and re-applies all K8s resources. Fails fast if `pki-int` is not mounted (run `enable` first).
- **`update --k8s --force`**: full teardown and rebuild — CAs, cert-manager, and all K8s resources from scratch.

```bash
hal vault pki update                  # rebuild CAs only
hal vault pki update --k8s            # reconcile cert-manager, preserve CAs
hal vault pki update --k8s --force    # rebuild everything
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

## Future: `--acme` flag

A future `--acme` flag will enable ACME HTTP-01 challenge support through Vault's PKI engine. Not implemented yet — reserved for a future work cycle.

---

## Side Effects
- Mounts two PKI secrets engines in Vault (`pki-root`, `pki-int` by default).
- Writes policy `hal-pki-issuer` to Vault.
- `--k8s`: enables dedicated `kubernetes-pki/` auth mount, deploys cert-manager, creates cluster-scoped `ClusterIssuer`.
- `--k8s`: stores a K8s SA-bound token as a Kubernetes secret in `cert-manager` namespace.
- `disable` tears down all of the above automatically.

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

Global flags: --debug, --dry-run
```
 Vault PKI teardown complete.
💡 Next Step: hal vault pki enable
❌ Vault mount 'pki-int' not found. Run 'hal vault pki enable' first.
   Use 'hal vault pki update --k8s --force' to rebuild everything from scratch.