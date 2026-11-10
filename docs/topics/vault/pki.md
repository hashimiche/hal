summary: Vault runs a two-tier certificate authority, and HAL shows it issuing certificates through cert-manager or ACME on the shared KinD cluster.
read: hal vault pki | hal vault status | hal creds status
actions: hal vault pki enable | hal vault pki enable --k8s | hal vault pki enable --acme | hal vault pki update | hal vault pki disable
docs:
- Vault PKI, use a CA hierarchy: https://developer.hashicorp.com/vault/docs/secrets/pki/considerations#use-a-ca-hierarchy
- Vault PKI, enable ACME support: https://developer.hashicorp.com/vault/docs/secrets/pki/acme#enable-acme-support-on-a-vault-pki-mount
- cert-manager, Vault issuer with Kubernetes auth: https://cert-manager.io/docs/configuration/vault/#option-2-vault-authentication-method-use-kubernetes-auth

# Vault PKI, a private certificate authority

## The mechanism

A private certificate authority (CA) lets services prove who they are with
certificates your own organisation signs. The usual shape is a hierarchy. A root CA
does one job, which is to sign intermediate CAs. An intermediate CA signs the leaf
certificates that workloads actually present. If an intermediate is lost, it is
replaced, and the root stays trusted.

Vault's PKI secrets engine plays both parts. Each CA is a mount, and its private key
is generated inside Vault and does not leave it. A role is the template for leaf
certificates: which names are allowed, the longest lifetime, the key type. A client
asks a role for a certificate, either by having Vault generate the key pair or by
sending a certificate signing request (CSR) that Vault signs.

Two ways of asking matter here. A controller such as cert-manager logs in to Vault,
builds the key pair itself and sends only a CSR, so the workload's key never
reaches Vault. ACME is the open protocol that Let's Encrypt made common. The client
proves it controls a name by answering a challenge, and then gets a certificate.
Vault can be the ACME server for a PKI mount.

## How this lab stages it

The base chain with software keys needs only a running Vault, and the code checks no
edition for it. `--k8s` and `--acme` add a demo on top, and both need `kind`,
`kubectl` and `helm`.

- **The base chain is two mounts.** `pki-root` holds the root CA (RSA-4096, common
  name `HAL Root CA`, maximum lease of 43800 hours, five years). `pki-int` holds the
  intermediate (RSA-4096, `HAL Intermediate CA`, 17520 hours, two years). HAL has Vault
  create the intermediate's CSR, has the root sign it, and installs the result.
  `--root-mount`, `--int-mount`, `--root-ttl` and `--int-ttl` change these.
- **The role is `hal-role`**, on `pki-int`, with RSA-2048 keys and a 24 hour
  maximum. It allows subdomains of `hal.local`, `cluster.local` and
  `svc.cluster.local`, but not the bare domains. `--allowed-domains` and
  `--max-cert-ttl` change it. The policy `hal-pki-issuer` lets a holder sign and
  issue through `hal-role` and nothing else.
- **ACME is switched on by the base command**, not by `--acme`. On `pki-int`, HAL
  tunes the response headers the ACME docs ask for, enables ACME, caps its
  certificates at `--acme-cert-ttl` (five minutes by default) and creates the role
  `acme-demo`. That role allows any name, because the Caddy demo asks for
  `acme.localhost`, which is not in the domain list above. This is the software-key
  path. The HSM path, below, tunes the headers but does not enable ACME or create
  that role.
- **The Kubernetes scenarios share one cluster.** HAL has a single KinD cluster,
  named `kind`. `--k8s`, `--acme`, the Vault Kubernetes lab, the database
  `--k8s` demo and the post-quantum lab each get their own namespace on it. HAL declares
  the host ports of all of them when it creates the cluster. It only reuses a cluster
  it created.
- **`--k8s` is cert-manager.** HAL installs cert-manager from the Jetstack chart
  (the latest unless `--cert-manager-version` pins one) in the namespace
  `cert-manager`. The webhook runs on the node's own network, so that the API
  server inside the KinD node can reach it. HAL then mounts a dedicated Kubernetes
  auth method at `kubernetes-pki`, with the role `cert-manager-role`, which
  accepts the service account `cert-manager-vault` of that namespace and hands out
  the `hal-pki-issuer` policy for one hour. The ClusterIssuer `vault-pki-issuer`
  signs through `pki-int/sign/hal-role` and logs in with that role.
- **Vault checks that login the same way as in the Kubernetes lesson.** It asks the
  Kubernetes API to review the token, with a reviewer service account bound to
  `system:auth-delegator`, and issuer validation is turned off. These are lab shortcuts.
- **The demo is a Certificate and a web page.** The Certificate `hal-web-pki-cert` in
  the namespace `pki-demo` asks for a 24 hour certificate, renewed one hour before it
  ends, for `hal-web-pki.hal.local` and the matching service name. An nginx
  deployment, `hal-web-pki`, serves it over TLS and shows the certificate's text and
  PEM. The page is `https://pki.localhost:8089`.
- **`--acme` is Caddy.** In the namespace `pki-acme-demo`, Caddy asks Vault's ACME
  directory for a certificate for `acme.localhost`. Its configuration disables the
  TLS-ALPN challenge, so the HTTP-01 challenge is the one that is used. Vault has to
  reach Caddy to check that challenge, and `.localhost` resolves to loopback inside
  the Vault container. So HAL starts a small CoreDNS container, `hal-acme-dns`, on
  `hal-net` that answers `acme.localhost` with the KinD node's address, points
  Vault's ACME validator at it, and permits that one address as a challenge target.
  An nginx gateway on the node's port 80 passes the requests on to Caddy. HAL also
  turns on automatic tidy on `pki-int` (every two minutes), since five-minute
  certificates leave many expired entries. The page is `https://acme.localhost:8090`,
  with a countdown to expiry and a badge when Caddy renews. By HAL's own message
  Caddy renews when about a third of the lifetime is left.
- **Variant: HSM.** On a Vault Enterprise HSM build created with
  `hal vault create --edition ent-hsm --mode prod`, `enable` detects the build and
  keeps the CA keys in a SoftHSM2 token through a Vault managed key
  (`hal-kms-root` by default). The CAs are generated through the managed key, so
  their private keys stay in the token. `--hsm` insists on this and `--no-hsm`
  gives software keys.
- **Variant: post-quantum.** `--pqc` is a separate lab with its own mounts
  (`hal-pqc-root`, `hal-pqc-int`) and namespace (`hal-pqc`). It needs a licensed
  Vault Enterprise 2.2.0 or later whose PKI engine offers ML-DSA-65. Two NGINX pods
  authenticate each other with ML-DSA-65 certificates and agree on keys with
  ML-KEM-768. The certificates last 10 seconds by default and the pods fetch new
  ones at about half their lifetime. The browser sees an ordinary ECDSA certificate,
  and the page is `https://pqc.localhost:8093`. If the cluster was created without
  that host port, status prints a port-forward command instead. The lab does not use
  cert-manager, and it cannot be combined with `--acme`, `--hsm` or the mount and TTL
  flags. The Vault docs say ACME does not support ML-DSA keys.

## What to expect

- **HAL installs no trust.** The CA is private, and the code has no step that adds
  it to a browser or system trust store, so expect a certificate warning on the
  demo pages until you trust the root yourself.
- **The root is online.** It lives in the same Vault as the intermediate and
  signs the intermediate right away. The Vault docs point out that a hierarchy lets
  the root get stronger protection. The lab does not do that.
- **cert-manager holds a stored credential.** The ClusterIssuer reads a service
  account token that HAL creates for one year and keeps in a Kubernetes Secret,
  `vault-k8s-token`, in the `cert-manager` namespace. cert-manager presents it to
  Vault when it logs in. The value is not written in this file.
- **The first `--k8s` run is slow.** HAL waits up to 60 seconds for the webhook, retries
  the ClusterIssuer and the Certificate up to ten times, ten seconds apart, and then
  waits up to 180 seconds for each of the issuer, the certificate and the
  deployment. If the certificate is not issued in time, HAL prints what it can find
  about the failure.
- **A second `enable` is not refused.** If the mounts already exist, the code tunes
  them and goes on to generate the CAs again. It was not run twice, and `update` is
  the clean way to start over.
- **Plain `update` rebuilds everything, and the demos go.** It runs the full
  teardown, including cert-manager, the demo namespaces and a cluster nothing else
  needs, and then enables only the base chain. Pass `--k8s` or `--acme` again to get a
  demo back. With `--k8s` or `--acme`, `update` alone leaves the CAs in place and
  reconciles only that demo. `--force` makes it rebuild the CAs too. Every ACME
  deployment restarts Caddy, so that its cached certificate is dropped.
- **The `--acme` demo is written for dev mode.** The directory URL in Caddy's
  configuration is plain `http`, and prod mode serves Vault over TLS, so do not expect
  `--acme` to work against a prod-mode Vault. HSM builds only run in prod mode, so that
  rules out the pair. This comes from reading the code and was not run. The `--k8s`
  code does work out the address and the CA bundle for a TLS Vault, but it was not run
  against one either.
- **Not covered here.** ACME external account binding and its directory
  policies, revocation lists, several issuers on one mount, name constraints, and the
  inner workings of the HSM and post-quantum labs beyond what is above.

## Commands

- `hal vault pki` with no action is read-only. It shows whether `pki-root` and
  `pki-int` are mounted, whether the root and intermediate exist and the role is there,
  and, when the cluster is up, the state of cert-manager, the issuer, the
  certificate and the two demo pods. `hal vault status` lists the PKI mounts among the
  secrets engines.
- `enable` builds the base chain. `enable --k8s` adds cert-manager and its demo,
  `enable --acme` adds Caddy and its demo, and both flags can go together. For an
  HSM build, `enable` uses the HSM path unless `--no-hsm` is given.
- `update` removes what `disable` removes and enables again, as the rules above say.
- `disable` unmounts both CAs, removes the `kubernetes-pki` auth method and the policy
  `hal-pki-issuer`, and removes the SoftHSM managed key when one exists. It
  uninstalls cert-manager, deletes the `pki-demo` and `pki-acme-demo`
  namespaces and the `hal-acme-dns` container. It deletes the KinD cluster only if the
  post-quantum lab and the Vault Secrets Operator are not in use and the cluster is
  one HAL created. It needs no flag for any of this. The post-quantum lab has its own
  `disable --pqc`; plain `disable` leaves it in place and keeps the cluster for it.

The post-quantum lab and the HSM variant are not in the actions, because they need a
licensed Enterprise Vault of a particular build. The SoftHSM PINs are lab secrets and
are not written here. For the credentials of what is running, `hal creds status`
shows them.
