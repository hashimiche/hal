# HAL PKI Command Spec

## Command
- `hal pki`

## Purpose
Manage Vault PKI secrets engines for a two-tier CA hierarchy (Root CA → Intermediate CA) and optionally deploy cert-manager to a KinD cluster so pods can receive Vault-issued TLS certificates.

## Related
- Vault must be running: `hal vault create`
- KinD cluster (shared with or independent of `hal vault k8s enable`): used by `hal pki k8s enable`

## Prerequisites
- HAL CLI is available in your local environment.
- `hal vault create` has been run and Vault is healthy at `http://127.0.0.1:8200`.
- For `hal pki k8s enable`: `kind`, `kubectl`, and `helm` must be installed and in PATH.

---

## Lifecycle: `hal pki create`

Enables two Vault PKI secrets engines and builds a two-tier CA chain:

| Step | What happens |
|------|-------------|
| 1 | Mount `pki-root` (max TTL 87600h / 10 years) |
| 2 | Generate Root CA (RSA-4096, internal key) |
| 3 | Configure CRL/OCSP URLs for `pki-root` |
| 4 | Mount `pki-int` (max TTL 43800h / 5 years) |
| 5 | Generate Intermediate CSR (RSA-4096, internal key) |
| 6 | Sign CSR with Root CA |
| 7 | Install signed cert on `pki-int` |
| 8 | Configure CRL/OCSP URLs for `pki-int` |
| 9 | Create role `hal-role` on `pki-int` for `hal.local`, `cluster.local`, `svc.cluster.local` |
| 10 | Write policy `hal-pki-issuer` (allows signing via `pki-int/sign/hal-role`) |

### Flags
```text
--root-mount string        Vault mount path for Root CA (default "pki-root")
--int-mount string         Vault mount path for Intermediate CA (default "pki-int")
--root-ttl string          Max TTL for Root CA (default "87600h")
--int-ttl string           Max TTL for Intermediate CA (default "43800h")
--allowed-domains string   Allowed domains for hal-role (default "hal.local,cluster.local,svc.cluster.local")
--max-cert-ttl string      Maximum TTL for issued leaf certs (default "72h")
```

### Example
```bash
hal pki create
```

### Commands to get certificates after setup (root token)

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

---

## Lifecycle: `hal pki update`

Same as `hal pki create` but first unmounts `pki-root` and `pki-int`, then rebuilds from scratch. Use this to regenerate all CAs.

```bash
hal pki update
```

---

## Lifecycle: `hal pki delete`

Unmounts `pki-root` and `pki-int` and removes the `hal-pki-issuer` policy.

```bash
hal pki delete
```

### Flags
```text
--root-mount string   Vault mount path for Root CA (default "pki-root")
--int-mount string    Vault mount path for Intermediate CA (default "pki-int")
```

---

## Lifecycle: `hal pki status`

Checks:
- Whether `pki-root` and `pki-int` are mounted
- Whether the Root CA cert is present
- Whether the Intermediate CA is signed and installed
- Whether the `hal-role` role exists on `pki-int`
- KinD cluster state, cert-manager deployment, ClusterIssuer, Certificate CR, and web pod

```bash
hal pki status
```

---

## Feature: `hal pki k8s [status|enable|disable]`

Deploy cert-manager to KinD and configure it to talk to `pki-int` so pods can receive Vault-issued TLS certificates. Deploys a demo web pod that renders the issued certificate.

### What `enable` does

| Step | What happens |
|------|-------------|
| 1 | Reuse existing KinD cluster or create a new one (port 30082→host 8089) |
| 2 | Deploy cert-manager via `helm upgrade --install jetstack/cert-manager` with CRDs |
| 3 | Create Vault token (policy: `hal-pki-issuer`, TTL 8760h) |
| 4 | Store token as K8s secret `vault-token` in `cert-manager` namespace |
| 5 | Apply `ClusterIssuer vault-pki-issuer` pointing to `http://<vault-ip>:8200` |
| 6 | Apply `Certificate hal-web-pki-cert` in namespace `pki-demo` (DNS: `hal-web-pki.hal.local`) |
| 7 | Deploy `hal-web-pki` (`httpd:2.4-alpine`) that mounts the TLS secret at `/tls` and renders cert details |
| 8 | Expose via `NodePort 30082` / `Service hal-web-pki` |

### What `disable` does
- Deletes namespace `pki-demo`
- Deletes `ClusterIssuer vault-pki-issuer`
- Deletes `vault-token` secret from `cert-manager`
- Uninstalls cert-manager Helm release
- Deletes `cert-manager` namespace
- **Does NOT delete the KinD cluster** (it may be shared with `hal vault k8s enable`)

### Flags
```text
--kind-node-image string          KinD node image (default "kindest/node:v1.31.1")
--cert-manager-version string     cert-manager Helm chart version (empty = latest)
--web-backend-image string        Demo backend image (default "httpd:2.4-alpine")
--int-mount string                Vault Intermediate CA mount (default "pki-int")
```

### Examples
```bash
# Full enable
hal pki k8s enable

# Access the web pod (shows the TLS cert in the browser)
kubectl port-forward -n pki-demo svc/hal-web-pki 8089:80
# → http://localhost:8089

# Inspect the issued certificate
kubectl describe certificate hal-web-pki-cert -n pki-demo
kubectl get secret hal-web-pki-tls -n pki-demo \
  -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -noout -text

# Tear down
hal pki k8s disable
```

---

## Side Effects
- Mounts two PKI secrets engines in Vault (`pki-root`, `pki-int` by default).
- Writes policy `hal-pki-issuer` to Vault.
- `k8s enable` deploys cert-manager and creates cluster-scoped resources (`ClusterIssuer`).
- `k8s enable` stores a Vault token as a K8s secret in the `cert-manager` namespace.
- `k8s disable` does **not** touch the KinD cluster itself.

---

## Command flags from `hal pki --help`
```text
Available Commands:
  create      Enable Vault PKI engines, generate Root CA and signed Intermediate CA
  update      Reconcile Vault PKI engines (unmount existing CAs and recreate)
  delete      Disable and remove Vault PKI engines and associated policy
  status      Check the status of Vault PKI engines, cert-manager, and the K8s demo
  k8s         Deploy cert-manager + Vault PKI web demo on KinD
```

Global flags: `--debug`, `--dry-run`
