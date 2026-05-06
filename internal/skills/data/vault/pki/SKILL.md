---
name: pki
description: Deploy, verify, and troubleshoot the Vault PKI CA chain and demo scenarios in hal. Use this skill when the user asks to set up a Root CA or Intermediate CA, issue leaf certificates, deploy cert-manager K8s integration, run the Vault ACME + Caddy live auto-renewal demo, or reset PKI engines. Triggers include "vault pki", "Root CA", "Intermediate CA", "cert-manager", "ACME", "Caddy renewal", "PKI certificate", and "hal vault pki".
---

# Hal Vault PKI Configurator

This skill covers the two-tier PKI CA chain and both K8s/ACME demo modes implemented by `hal vault pki`.

## Lab Assumptions

- Vault runs locally at `http://vault.localhost:8200`
- Root token defaults to `root`
- PKI engines: `pki-root` (Root CA, 5y) and `pki-int` (Intermediate CA, 2y)
- The lab uses KinD, `kubectl`, and `helm` for the `--k8s` and `--acme` modes
- Prefer `hal` for lifecycle actions and `vault read/write` for post-deploy inspection

## What The Command Actually Sets Up

### Base PKI (always)

- Root CA: `pki-root/` (RSA-4096, 5y max TTL)
- Intermediate CA: `pki-int/` (RSA-4096, signed by Root CA, 2y max TTL)
- Role: `pki-int/roles/hal-role` (domains: `hal.local`, `cluster.local`, `svc.cluster.local`, max TTL 24h)
- Policy: `hal-pki-issuer`
- ACME endpoint enabled: `pki-int/config/acme` (for Caddy / ACME clients)
- Role: `pki-int/roles/acme-demo` (TTL = `--acme-cert-ttl`, default `5m`, `allow_any_name`)

### `--k8s` mode (adds cert-manager + nginx demo)

- Shared KinD cluster (`kind`) with ports 30080→8088, 30082→8089, 30083→8090
- Jetstack cert-manager (Helm, OCI chart, `webhook.hostNetwork=true` for KinD)
- Dedicated Vault Kubernetes auth mount: `kubernetes-pki/`
- ClusterIssuer: `vault-pki-issuer` → `pki-int/sign/hal-role`
- Certificate: `hal-web-pki-cert` in namespace `pki-demo`
- nginx web pod with TLS cert mounted at `/tls`, page shows `openssl x509 -noout -text`
- Access: `https://pki.localhost:8089`

### `--acme` mode (adds Caddy + ACME demo)

- Shared KinD cluster (same cluster, all 3 port mappings declared upfront)
- Caddy pod in namespace `pki-acme-demo` — no cert-manager, no CRDs
- Caddy uses Vault ACME directory: `http://vault.localhost:8200/v1/pki-int/roles/acme-demo/acme/directory`
- `config/acme max_ttl` set to `--acme-cert-ttl` (Vault 2.x requirement — role TTL alone is insufficient)
- Live web page at `https://acme.localhost:8090` shows countdown + renewal badge
- `update --acme` always restarts Caddy deployment to clear cert cache and force a fresh ACME exchange
- Access: `https://acme.localhost:8090`

## Workflow

### Step 1: Choose the lifecycle action

Use smart status mode if needed:

    hal vault pki

Then use the correct lifecycle command:

    # Base PKI only
    hal vault pki enable

    # With cert-manager K8s demo
    hal vault pki enable --k8s

    # With Vault ACME + Caddy live renewal demo
    hal vault pki enable --acme
    hal vault pki enable --acme --acme-cert-ttl 5m

    # Both demos at once
    hal vault pki enable --k8s --acme

    # Reconcile ACME layer (PKI engines preserved, Caddy restarted)
    hal vault pki update --acme --acme-cert-ttl 2m

    # Full rebuild from scratch
    hal vault pki update --k8s --force
    hal vault pki update --acme --force

    # Teardown
    hal vault pki disable

### Step 2: Verify resulting state

If Vault MCP is available, inspect:

1. `sys/mounts` — check `pki-root/` and `pki-int/` are mounted
2. `pki-int/cert/ca` — confirm Intermediate CA is installed
3. `pki-int/roles/hal-role` — confirm role config
4. `pki-int/config/acme` — confirm ACME enabled and `max_ttl`
5. `pki-int/roles/acme-demo` — confirm TTL matches `--acme-cert-ttl`

If MCP is unavailable, use:

    export VAULT_ADDR='http://vault.localhost:8200'
    export VAULT_TOKEN='root'

    vault secrets list
    vault read pki-int/roles/hal-role
    vault read pki-int/config/acme
    vault read pki-int/roles/acme-demo

For K8s state:

    kubectl get clusterissuer vault-pki-issuer
    kubectl get certificate hal-web-pki-cert -n pki-demo
    kubectl get pods -n pki-demo
    kubectl get pods -n pki-acme-demo

### Step 3: Present structured results

**Tier 1 — Success Summary**
Confirm CA chain is built and which demo modes are active.

**Tier 2 — Configuration Details Table**

| Component | Value | Description |
|-----------|-------|-------------|
| Root CA | `pki-root/` | RSA-4096, 5y max TTL |
| Intermediate CA | `pki-int/` | Signed by Root CA, 2y max TTL |
| Role | `pki-int/roles/hal-role` | Leaf cert issuance, max TTL 24h |
| ACME dir | `pki-int/roles/acme-demo/acme/directory` | Vault built-in ACME, TTL = `--acme-cert-ttl` |
| K8s demo | `https://pki.localhost:8089` | cert-manager + nginx (if `--k8s`) |
| ACME demo | `https://acme.localhost:8090` | Caddy auto-renewal (if `--acme`) |

**Tier 3 — Actionable Testing Commands**

    # Issue a leaf cert directly from Vault
    vault write pki-int/issue/hal-role \
      common_name="test.hal.local" \
      ttl="24h"

    # Inspect the Caddy cert via kubectl exec
    kubectl exec -n pki-acme-demo deploy/hal-caddy-acme -- \
      ls /data/caddy/certificates/

    # Inspect the cert-manager certificate
    kubectl get secret hal-web-pki-tls -n pki-demo \
      -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -noout -text

## Handling Edge Cases

1. **Vault is not running:** Instruct the user to run `hal vault create` first.
2. **`kind`/`kubectl`/`helm` missing:** Required for `--k8s` and `--acme`; install them first.
3. **cert-manager webhook not ready:** The code retries up to 10× with 10s sleep between attempts. If it times out, check `kubectl get pods -n cert-manager`.
4. **Caddy ACME exchange fails:** Check `kubectl logs -n pki-acme-demo deploy/hal-caddy-acme`. Common cause: Vault ACME `config/acme max_ttl` not set (fixed in current code) or DNS resolution of `vault.localhost` from inside the pod.
5. **TTL still shows 12h after update:** Vault 2.x `config/acme max_ttl` defaults to `2160h` and caps all ACME certs on the mount regardless of role TTL. `update --acme` re-syncs both. Use `vault read pki-int/config/acme` to verify.
6. **Old cert TTL after update:** `update --acme` automatically does `kubectl rollout restart` to clear the emptyDir cert cache. If the pod still shows the old cert, run the restart manually.
7. **`allow_any_name` requirement:** The `acme-demo` role must have `allow_any_name=true` because Caddy requests a cert for `acme.localhost` which does not match the `hal.local` domain list. This is already set by the code.
