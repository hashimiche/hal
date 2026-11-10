summary: Pods on a KinD cluster read a Vault secret through the Vault Secrets Operator, synced to a Kubernetes Secret or projected by the CSI driver.
read: hal vault k8s | hal vault status | hal creds status
actions: hal vault k8s enable | hal vault k8s enable --csi | hal vault k8s update | hal vault k8s disable
docs:
- Vault Kubernetes auth: https://developer.hashicorp.com/vault/docs/auth/kubernetes#configuring-kubernetes
- Vault Secrets Operator, static secrets: https://developer.hashicorp.com/vault/docs/deploy/kubernetes/vso/sources/vault#vaultstaticsecret-custom-resource
- Vault Secrets Operator CSI driver: https://developer.hashicorp.com/vault/docs/deploy/kubernetes/vso/csi#secrets-flow-traditional-vso-vs-csi-driver

# Vault and Kubernetes with the Vault Secrets Operator

## The mechanism

A pod needs a secret from Vault, and it must not carry a stored credential to get
it. Two pieces solve this.

The first is the Kubernetes auth method. Kubernetes gives every pod a signed token
for its service account. Vault does not trust that token on sight: it asks the
Kubernetes API, through the TokenReview call, whether the token is valid and whose
it is. Then it compares the service account name and namespace with the bindings of
a role. If they match, Vault returns a token that carries the role's policies. For
Vault to make that call, it needs a credential of its own that is allowed to review
tokens. This lesson calls it the reviewer.

The second is the Vault Secrets Operator (VSO), a controller that runs in the
cluster and does the login for the workloads. It reads custom resources. One says
where Vault is, one says how to log in, and one says which secret to copy to which
Kubernetes Secret. The application then sees an ordinary Kubernetes Secret and
never talks to Vault. VSO also has a CSI driver mode. The secret is projected into
the pod as a file and is never stored as a Kubernetes Secret.

## How this lab stages it

- **There is one KinD cluster, named `kind`, and the Kubernetes scenarios share
  it.** `hal vault k8s`, `hal vault database --k8s` and the `hal vault pki`
  variants that use Kubernetes each get their own namespace on it. HAL declares the
  host ports of all of them when it creates the cluster, so any combination can run
  together. Its node is attached to the `hal-net` network, which is how the Vault
  container, outside the cluster, reaches the Kubernetes API on port 6443.
- **HAL only reuses a cluster it created.** If a cluster named `kind` already
  exists and HAL did not create it, `enable` stops with an error and does not
  touch it.
- **On the Vault side**, HAL mounts a KV v2 engine at `kv-k8s` with one secret,
  `app1`, holding a demo value under the key `mysecret`. The policy `app1-read`
  reads that path. The auth method is mounted at `auth/kubernetes`, with the role
  `app1-role`. The role accepts the service account `app1-sa` in the namespace
  `app1`, with the audience `vault`, and it hands out tokens that live one hour.
- **The reviewer is a service account**, `vault-reviewer` in the `default`
  namespace, bound to the cluster role `system:auth-delegator`. HAL asks for a token
  for it that lasts 87600 hours (ten years) and gives it to Vault, together with the
  cluster's CA and API address. Issuer validation is turned off on the auth method.
  These are lab shortcuts that keep the demo running, not a production setup.
- **VSO is a Helm release** named `vault-secrets-operator`, from HashiCorp's chart
  repository, in the namespace `vso`. The chart version follows the latest unless
  `--vso-chart-version` pins it.
- **The workload is a small web page.** In the namespace `app1`, HAL applies a
  VaultConnection that points at the `hal-vault` container on `hal-net`, and a
  VaultAuth that logs in with the `kubernetes` mount, the role `app1-role` and the
  service account `app1-sa`. The page is served by `hal-web-backend` (two replicas)
  behind an nginx proxy, and it shows the secret. The proxy is a NodePort that HAL
  maps to `http://web.localhost:8088`, so no port-forward is needed.
- **Native mode is the default.** A VaultStaticSecret named `vso-mysecret` copies
  `kv-k8s/app1` into the Kubernetes Secret `hal-web-secret`, checks Vault again
  every 15 seconds, and is set to restart the backend deployment when the value
  changes. The backend receives the value as an environment variable.
- **CSI mode is `--csi`.** A CSISecrets resource, `hal-csi-secrets`, lists the
  secret, and the backend mounts it through the driver `csi.vso.hashicorp.com` as a
  file under `/mnt/secrets`. No Kubernetes Secret is created. HAL requires Vault
  Enterprise for it. On Community Edition it prints a warning and deploys the
  native mode instead.

## What to expect

- **`kind`, `kubectl` and `helm` must be installed and on the PATH.** The command
  checks them first, so even the read-only status stops with an error without them.
- **`enable` needs a running, healthy Vault**, and it stops with an error
  otherwise. If the container engine looks short of capacity for the scenario, HAL
  asks for a yes before it goes on.
- **The first run builds the cluster and pulls images**, so it takes a while. The
  node image and the demo images can be overridden with flags, and `hal vault k8s
  --help` lists them.
- **The Vault address in the manifests is plain `http`.** That fits dev mode. Prod
  mode serves Vault over TLS, and this scenario's code does not adapt to it, so do
  not expect it to work against a prod-mode Vault. It was not run that way.
- **A change of the secret shows differently in each mode.** In native mode the page
  follows the Vault value after the next 15 second check, because the backend is
  restarted in a rolling update. In CSI mode the pod renders the page once, at
  startup, from the file the driver provides.
- **`--jwt` is not part of this lesson.** The flag is labelled experimental, and
  `enable` never configures a `jwt-k8s` auth method. The flag only changes which
  mount the cleanup looks at, and the status reports a JWT mode if such a mount
  exists. The Kubernetes auth of this lab is the `kubernetes` mount.
- **The other Kubernetes scenarios have their own login mounts**, so disabling this
  one does not remove their logins. They are separate lessons.
- **Disable keeps a cluster that is still needed.** When another Kubernetes scenario
  is active on the cluster, HAL leaves the cluster and removes only the namespace
  `app1`.

## Commands

- `hal vault k8s` with no action is read-only. It reports the cluster, the VSO
  release, the Vault auth, the demo endpoint and whether the demo is native or CSI,
  then names the next step. `hal vault status` lists `kubernetes` among the active
  auth methods and `kv-k8s` among the secrets engines.
- `enable` builds everything above. `enable --csi` asks for the CSI mode.
- `update` removes what `disable` removes, then runs `enable` again. Pass `--csi`
  again if the CSI mode is wanted, because the mode is not remembered.
- `disable` removes the Vault side (the identities created by logins on the
  `kubernetes` mount, the `kv-k8s` engine, the auth method and the policy
  `app1-read`). It then deletes the KinD cluster, but only if no other Kubernetes
  scenario is active and only a cluster that HAL created. If Vault is not reachable
  at that moment, HAL skips the Vault side and says so, and the cluster is still
  handled.

The demo page needs no login. The Vault login is a lab secret, so it is not written
in this file: `hal creds status` shows the credentials of what is running.
