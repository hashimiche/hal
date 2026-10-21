# HAL Vault TPM CLI Command Spec

## Command
- `hal vault tpm cli`

`hal vault tpm` with no subcommand shows CLI and proxy status. The proxy demo is [vault-tpm-proxy.md](vault-tpm-proxy.md). Both demos share the `hal-vault-tpm` VM, `swtpm`, and `auth/tpm/`.

## Purpose
Stand up a Multipass Ubuntu VM that runs `swtpm`, enroll that TPM with Vault Enterprise, attest a client certificate, and log in with `vault login -method=tpm`.

## Related
- Parent namespace: [vault.md](vault.md)

## Prerequisites
- Multipass is installed and running (https://multipass.run/install)
- Vault Enterprise 2.2 or newer is running in prod mode (`hal vault create --mode prod`)
- The guest must be able to reach the host on port 8200. The VM forwards `127.0.0.1:8200` to that port so the CLI can use the prod certificate, which includes `127.0.0.1`

## Flags
```text
      --app string            Linux user that runs vault login (default "demo")
      --ubuntu-image string   Ubuntu image for the Multipass VM (default "24.04")
      --vm-cpus string        Number of CPUs for the VM (default "1")
      --vm-mem string         Amount of RAM for the VM (default "1G")
  -h, --help                  help for cli
```
- Global flags: `--debug`, `--dry-run`, `--verbose`

## Side Effects
- Creates or reuses a Multipass VM named `hal-vault-tpm`
- Installs `swtpm` and a Vault CLI build matching the running server
- Enables the `tpm` auth method, role `demo-app` (or `<app>-app`), policy `tpm-demo`, and KV secret `secret/data/tpm-demo`
- Enrolls the software TPM at `identity/tpm`
- Writes attest state to `/var/lib/vault-cli/demo/tpm`

## Lifecycle Actions

### Status (Default)
```bash
hal vault tpm cli
```
Displays whether the VM exists, whether `auth/tpm/` is mounted, and whether the CLI attest state is present.

### Enable
```bash
hal vault tpm cli enable
```
1. Launches Ubuntu 24.04 via Multipass, or reuses `hal-vault-tpm` if the proxy demo already created it
2. Starts `swtpm` on `/run/swtpm/swtpm.sock` and forwards guest `127.0.0.1:8200` to the host Vault
3. Enables TPM auth, writes the role, policy, and demo secret
4. Enrolls the guest TPM, runs `vault tpm attest`, then `vault login -method=tpm`
5. Reads `secret/tpm-demo` with the login token before reporting success

### Disable
```bash
hal vault tpm cli disable
```
1. Removes `/var/lib/vault-cli/<app>/tpm`
2. When Vault Proxy is not running, disables `auth/tpm`, deletes policy `tpm-demo`, deletes the demo secret and enrolled `identity/tpm` records, and deletes the `hal-vault-tpm` VM
3. When the proxy is still running, leaves the VM and the auth method in place

`hal vault delete` always removes the VM.

### Update
```bash
hal vault tpm cli update
```
Re-runs enable on the existing VM. It replaces the CLI attest state and does not stop the proxy.

## Example Workflow
```bash
export VAULT_LICENSE_PATH=/path/to/vault.hclic
# Docker Hub has no hashicorp/vault-enterprise:2.2.0-rc1-ent tag.
# Build the local image shown in vault-tpm-proxy.md, then:
hal vault create --mode prod --edition ent \
  --vault-image localhost/vault-enterprise --vault-tag 2.2.0-rc1-ent

hal vault tpm cli enable

multipass shell hal-vault-tpm
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

## Notes
- The TPM is `swtpm` on a Unix socket. Multipass cannot attach a hypervisor TPM.
- `device_path` `/run/swtpm/swtpm.sock` is the simulator socket Vault's TPM client dials.
- The role allows the enrolled TPM id (`tpm_ids`) and grants policy `tpm-demo` for one hour.
