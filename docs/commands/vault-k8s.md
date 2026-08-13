# HAL Vault K8s Command Spec

## Command
- `hal vault k8s`

## Purpose
Deploy KinD and Vault Secrets Operator scenario for Kubernetes integration labs.

## Related
- Parent namespace: [vault.md](vault.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Deprecated: older HAL docs may reference `hal vault k8s --force`. That flag has been removed from the CLI. Use `hal vault k8s update`.
- Command flags from `hal vault k8s --help`:
```text
--csi                        Use the VSO CSI Driver (Requires Vault Enterprise)
-u, --update                     Reconcile cluster and VSO configuration
-h, --help                       help for k8s
--jwt                        Use the advanced jwt-k8s OIDC architecture (experimental)
--kind-node-image string     KinD node image used when creating the cluster (default "kindest/node:v1.36.1")
--vso-chart-version string   Helm chart version for hashicorp/vault-secrets-operator (empty uses latest)
--web-backend-image string   Demo backend container image (default "httpd:2.4-alpine")
--web-proxy-image string     Demo reverse proxy container image (default "nginx:alpine")
```
- Global flags: `--debug`, `--dry-run`, `--verbose`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.
- The shared KinD cluster is attached to `hal-net` via `ensureHALKindCluster()`: experimental kind network env vars plus an explicit `network connect` fallback so Vault can reach the Kubernetes API.
- `kubernetes_host` and VSO `VaultConnection` addresses use the node's `hal-net` IP only (not a concatenated inspect of every attached NIC).

## Troubleshooting
- Status line `Active (Network: not on hal-net)` means the node is on the default `kind` network. Re-run `hal vault k8s enable` or `hal vault k8s update` to attach it; a cluster recreate is not required.
- Token reviewer / VSO connection failures are usually the same network miss: Vault on `hal-net` cannot reach a node that only exists on `kind`.

## Example
```bash
hal vault k8s enable
```
