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
- Command flags from `hal vault k8s --help`:
```text
--csi                        Use the VSO CSI Driver (Requires Vault Enterprise)
-d, --disable                    Destroy KinD and clean up Vault configurations
-e, --enable                     Deploy KinD and configure Vault Secrets Operator
-f, --force                      Force a clean redeployment of the cluster
-h, --help                       help for k8s
--jwt                        Use the advanced jwt-k8s OIDC architecture (experimental)
--kind-node-image string     KinD node image used when creating the cluster (default "kindest/node:v1.31.1")
--vso-chart-version string   Helm chart version for hashicorp/vault-secrets-operator (empty uses latest)
--web-backend-image string   Demo backend container image (default "httpd:2.4-alpine")
--web-proxy-image string     Demo reverse proxy container image (default "nginx:alpine")
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal vault k8s
```
