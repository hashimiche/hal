# HAL Vault TPM Command Spec

## Command
- `hal vault tpm [status|enable|disable|update]`

Two demos share the `hal-vault-tpm` VM, `swtpm`, and `auth/tpm/`.

- `--proxy` — Vault Proxy auto-auth. The proxy attests, then serves the Vault API on a Unix socket with that token filled in.
- `--cli` — enroll the TPM, attest a client certificate, and log in with `vault login -method=tpm`.

`hal vault tpm` with no flags shows both. Pass `--proxy` or `--cli` to show one. `enable`, `update`, and `disable` require at least one of those flags. Enabling one does not remove the other.

## Purpose
Stand up a Multipass Ubuntu VM that runs `swtpm` and enroll that TPM with Vault Enterprise.

## Related
- Parent namespace: [vault.md](vault.md)

## Prerequisites
- Multipass is installed and running (https://multipass.run/install)
- Vault Enterprise 2.2 or newer is running in prod mode (`hal vault create --mode prod`)
- The guest must be able to reach the host on port 8200. The VM forwards `127.0.0.1:8200` to that port so the demos can use the prod certificate, which includes `127.0.0.1`

## Flags
```text
      --proxy                 Vault Proxy auto-auth on the guest Unix socket
      --cli                   Attest with the Vault CLI and log in with -method=tpm
      --app string            Linux user that owns the proxy socket and runs vault login (default "demo")
      --ubuntu-image string   Ubuntu image for the Multipass VM (default "24.04")
      --vm-cpus string        Number of CPUs for the VM (default "1")
      --vm-mem string         Amount of RAM for the VM (default "1G")
  -h, --help                  help for tpm
```
- Global flags: `--debug`, `--dry-run`, `--verbose`
- Hidden compatibility flags: `--enable`, `--disable`, `--update`

## Side Effects
- Creates or reuses a Multipass VM named `hal-vault-tpm`
- Installs `swtpm` and a Vault CLI build matching the running server
- Enables the `tpm` auth method, role `demo-app` (or `<app>-app`), policy `tpm-demo`, and KV secret `secret/data/tpm-demo`
- Enrolls the software TPM at `identity/tpm`
- `--proxy` starts `vault proxy` with TPM auto-auth. The API is at `/run/vault-proxy/demo.sock`
- `--cli` writes attest state to `/var/lib/vault-cli/demo/tpm`

## Lifecycle Actions

### Status (Default)
```bash
hal vault tpm
hal vault tpm status --proxy
hal vault tpm status --cli
```
Displays whether the VM exists, whether `auth/tpm/` is mounted, and whether the selected demo is ready.

### Enable
```bash
hal vault tpm enable --proxy
hal vault tpm enable --cli
```
1. Launches Ubuntu 24.04 via Multipass, or reuses `hal-vault-tpm` if the other demo already created it
2. Starts `swtpm` on `/run/swtpm/swtpm.sock` and forwards guest `127.0.0.1:8200` to the host Vault
3. Enables TPM auth, writes the role, policy, and demo secret
4. Enrolls the guest TPM
5. `--proxy` starts Vault Proxy and reads `secret/tpm-demo` through the proxy socket
6. `--cli` runs `vault tpm attest`, then `vault login -method=tpm`, and reads `secret/tpm-demo` with that token

Pass both flags to enable both demos in one command.

### Disable
```bash
hal vault tpm disable --proxy
hal vault tpm disable --cli
```
- `--proxy` stops `vault-proxy` and removes its unit and config. When the CLI demo is not attested, it also disables `auth/tpm`, deletes policy `tpm-demo`, deletes the demo secret and enrolled `identity/tpm` records, and deletes the `hal-vault-tpm` VM.
- `--cli` removes `/var/lib/vault-cli/<app>/tpm`. When Vault Proxy is not running, it tears the lab down the same way.
- When the other demo is still in place, the VM and the auth method stay.

`hal vault delete` always removes the VM.

### Update
```bash
hal vault tpm update --proxy
hal vault tpm update --cli
```
Re-runs enable for the selected demo on the existing VM. Updating one does not remove the other.

## Example Workflow
```bash
export VAULT_LICENSE_PATH=/path/to/vault.hclic
# Docker Hub has no hashicorp/vault-enterprise:2.2.0-rc1-ent tag.
# Build the local image in Troubleshooting below, then:
hal vault create --mode prod --edition ent \
  --vault-image localhost/vault-enterprise --vault-tag 2.2.0-rc1-ent

hal vault tpm enable --proxy

multipass shell hal-vault-tpm
sudo -u demo VAULT_ADDR=unix:///run/vault-proxy/demo.sock vault kv get secret/tpm-demo
```

The proxy forces its TPM auto-auth token onto the request. No token is copied by hand.

```bash
hal vault tpm enable --cli

sudo -u demo env -u VAULT_TOKEN \
  VAULT_ADDR=https://127.0.0.1:8200 \
  VAULT_CACERT=/var/lib/vault-agent/demo/tls/vault-ca.pem \
  vault login -method=tpm \
  -tpm-state-dir=/var/lib/vault-cli/demo/tpm \
  -tpm-device-path=/run/swtpm/swtpm.sock \
  role_name=demo-app
sudo -u demo env -u VAULT_TOKEN \
  VAULT_ADDR=https://127.0.0.1:8200 \
  VAULT_CACERT=/var/lib/vault-agent/demo/tls/vault-ca.pem \
  vault kv get secret/tpm-demo
```

## How It Works
Vault Enterprise 2.2 TPM auth issues a certificate for a key that stays inside the TPM. Vault Proxy's `tpm_cert` block runs that attestation against `auth/tpm`, renews the certificate, and `auto_auth` method `tpm` logs in with it. `api_proxy.use_auto_auth_token = "force"` injects the token on the Unix listener.

The TPM is `swtpm` inside the guest. Its command socket is `/run/swtpm/swtpm.sock`. Vault's client opens a Unix socket as a TPM simulator, so Multipass does not have to attach a virtual TPM device.

## Notes
- The role allows the enrolled TPM id (`tpm_ids`) and grants policy `tpm-demo` for one hour.
- `device_path` `/run/swtpm/swtpm.sock` is the simulator socket Vault's TPM client dials.

## Troubleshooting

### Multipass Not Found
Install from https://multipass.run/install

### Vault is not Enterprise 2.2, or it is still in dev mode

`hashicorp/vault-enterprise:2.2.0-rc1-ent` is not on Docker Hub. The matching binaries are at https://releases.hashicorp.com/vault/2.2.0-rc1+ent/. `hashicorp/vault:2.2.0-rc1` is the community build and does not include TPM auth.

Build a local image from the community image plus the Enterprise binary. `TARGETARCH` is `arm64` or `amd64`, matching the engine VM (`podman info --format '{{.Host.Arch}}'`).

```bash
mkdir -p /tmp/vault-ent && cd /tmp/vault-ent
cat > Dockerfile <<'EOF'
FROM hashicorp/vault:2.2.0-rc1
USER root
ARG TARGETARCH
RUN apk add --no-cache unzip wget \
 && wget -O /tmp/vault.zip "https://releases.hashicorp.com/vault/2.2.0-rc1+ent/vault_2.2.0-rc1+ent_linux_${TARGETARCH}.zip" \
 && unzip -o /tmp/vault.zip -d /bin \
 && chmod 0755 /bin/vault \
 && rm /tmp/vault.zip
USER vault
EOF
podman build --build-arg TARGETARCH=arm64 -t localhost/vault-enterprise:2.2.0-rc1-ent .

hal vault delete
export VAULT_LICENSE_PATH=/path/to/vault.hclic
hal vault create --mode prod --edition ent \
  --vault-image localhost/vault-enterprise --vault-tag 2.2.0-rc1-ent
```

Prod mode serves `https://vault.localhost:8200` and writes a generated root token. It does not use `http://` or the dev token `root`.

### The VM cannot reach port 8200
Prod Vault must be published on the host. From the VM, the default route's gateway is the host. Enable fails before enrollment if that port does not accept a connection.

### Proxy never serves the secret
```bash
multipass exec hal-vault-tpm -- journalctl -u vault-proxy -n 40 --no-pager
multipass exec hal-vault-tpm -- systemctl status swtpm vault-forward vault-proxy
```
