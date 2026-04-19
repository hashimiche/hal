---
name: k8s
description: Deploy, verify, and troubleshoot the Vault Kubernetes auth lab in hal. Use this skill when the user asks to enable k8s auth, test pod authentication, wire Vault Secrets Operator to a local cluster, use native Kubernetes auth or JWT mode, or reset the KinD demo. Triggers include "enable k8s auth", "vault kubernetes", "vault secrets operator", "kind", "pod identity", and "hal vault k8s".
---

# Hal Vault Kubernetes Configurator

This skill covers the KinD + Vault Secrets Operator lab implemented by `hal vault k8s`.

## Lab Assumptions

- Vault runs locally at `http://127.0.0.1:8200`
- Root token defaults to `root`
- The lab uses KinD, `kubectl`, and `helm`
- Native mode mounts `kubernetes/`
- Some flows may also use `jwt-k8s/` depending on the command options and lab mode

## What The Command Actually Sets Up

- KinD cluster attached to `hal-net`
- `vso` and `app1` namespaces
- KV mount: `kv-k8s/`
- Policy: `app1-read`
- Vault auth role: `auth/kubernetes/role/app1-role`
- Vault Secrets Operator Helm release in namespace `vso`
- Demo application in namespace `app1`
- nginx reverse proxy endpoint at `http://web.localhost:8088` (no port-forward workflow)
- Demo website that renders the active secret value for native sync or CSI projection mode

## Demo Modes

- Native mode (default): `VaultStaticSecret` syncs `mysecret` to a Kubernetes secret, app pods read it as `HAL_SECRET`, and rollout is automated so secret edits in Vault are reflected by refreshed web pods.
- CSI mode (`--csi`, Enterprise): `CSISecrets` projects the Vault secret via `csi.vso.hashicorp.com`; app pods read the mounted file and render local `index.html`.

## Workflow

### Step 1: Choose the lifecycle action

Use smart status mode if needed:

    hal vault k8s

Then use one of these:

    hal vault k8s enable
    hal vault k8s enable --csi
    hal vault k8s --force
    hal vault k8s disable

### Step 2: Verify the resulting state

If Vault MCP is available, inspect:

1. `auth/kubernetes/config`
2. `auth/kubernetes/role/app1-role`
3. `kv-k8s/data/app1`

Also verify the cluster side with CLI:

    kubectl get ns
    kubectl get pods -n vso
    kubectl get pods -n app1
    helm list -n vso

Mode-specific checks:

    kubectl get vaultstaticsecret vso-mysecret -n app1
    kubectl get csisecrets hal-csi-secrets -n app1

### Step 3: Present structured results

**Tier 1 — Success Summary**
Provide a brief confirmation that KinD, VSO, and Vault auth are configured, including the active demo endpoint `http://web.localhost:8088`.

**Tier 2 — Configuration Details Table**

| Component | Value | Description |
|-----------|-------|-------------|
| Auth Path | `auth/kubernetes/` | The mount point |
| Vault Role | `app1-role` | Maps the `app1-sa` service account to Vault policies |
| Policy | `app1-read` | Grants read access to the demo secret |
| KV Mount | `kv-k8s/` | Stores the example application secret |

**Tier 3 — Actionable Testing Commands**

    export VAULT_ADDR='http://127.0.0.1:8200'
    export VAULT_TOKEN='root'

    vault read auth/kubernetes/config
    vault read auth/kubernetes/role/app1-role
    vault kv get kv-k8s/app1

## Handling Edge Cases

1. **Missing local dependencies:** If `kind`, `kubectl`, or `helm` is not installed, say so explicitly.
2. **Token reviewer errors:** Explain that Vault must be able to reach the KinD API endpoint and validate service account tokens.
3. **CSI requested on OSS Vault:** Explain that the code downgrades to standard native sync if Vault Enterprise is not detected.
4. **User wants to tune roles after deployment:** Provide exact `vault write auth/kubernetes/role/...` commands.