---
name: tpm
description: Deploy and test Vault Enterprise TPM auth inside a Multipass VM, either through Vault Proxy or the Vault CLI. Use when the user asks for TPM auth, tpm_cert, vault login -method=tpm, or hal vault tpm.
---

# Hal Vault TPM

One command, `hal vault tpm`, with two demos on one Multipass VM (`hal-vault-tpm`), one `swtpm` socket, and `auth/tpm/`. Spec: `docs/commands/vault-tpm.md`.

- `hal vault tpm enable --proxy` — Vault Proxy auto-auth.
- `hal vault tpm enable --cli` — `vault tpm attest` then `vault login -method=tpm`.

`hal vault tpm` prints both statuses. `enable`, `update`, and `disable` need `--proxy`, `--cli`, or both. Enabling one does not remove the other. Disabling one removes the VM only when the other demo is gone. `hal vault delete` always removes the VM.

## Lab Assumptions

- Vault Enterprise 2.2 or newer, prod mode (`hal vault create --mode prod`)
- Auth mount: `auth/tpm/`, role `<app>-app` (default `demo-app`)
- Software TPM socket: `/run/swtpm/swtpm.sock`
- Proxy socket: `/run/vault-proxy/demo.sock`
- CLI attest state: `/var/lib/vault-cli/demo/tpm`
- Demo secret: `secret/data/tpm-demo`

## Proxy test

```bash
hal vault tpm enable --proxy
multipass shell hal-vault-tpm
sudo -u demo VAULT_ADDR=unix:///run/vault-proxy/demo.sock vault kv get secret/tpm-demo
```

## CLI test

```bash
hal vault tpm enable --cli
sudo -u demo env -u VAULT_TOKEN \
  VAULT_ADDR=https://127.0.0.1:8200 \
  VAULT_CACERT=/var/lib/vault-agent/demo/tls/vault-ca.pem \
  vault login -method=tpm \
  -tpm-state-dir=/var/lib/vault-cli/demo/tpm \
  -tpm-device-path=/run/swtpm/swtpm.sock \
  role_name=demo-app
```
