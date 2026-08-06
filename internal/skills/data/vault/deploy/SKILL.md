---
name: deploy
description: Deploy local Vault in hal. Use when the user asks to start Vault, initialize the local control plane, or recover from Vault being offline.
---

# Vault Deploy Workflow

## Intent

Handle hal vault create requests with a stable lifecycle pattern.

## Primary Command

- hal vault create

## Edition And Token Baseline

- Default deploy is Vault CE in dev mode.
- Dev mode root token is `root`.
- Enterprise deploy requires `hal vault create --edition ent` and `VAULT_LICENSE` exported (this is still dev mode, just the Enterprise image).
- Enterprise HSM deploy requires `hal vault create --edition ent-hsm --mode prod`.
  Create builds `hal-vault-softhsm:latest` from the selected Enterprise HSM
  source image and a glibc SoftHSM2 base image.

## Production Mode (`--mode prod`)

- `hal vault create --mode prod` stands up a single-node **production** Vault Enterprise: real `vault server -config` with integrated Raft storage on a persistent volume, TLS at `https://vault.localhost:8200` (self-signed cert), and automatic init + unseal.
- Implies `--edition ent`; requires `VAULT_LICENSE` / `VAULT_LICENSE_PATH`. `--edition ce --mode prod` is rejected.
- Init material (root token + unseal key) is saved to `~/.hal/vault-prod/init.json` (mode `0600`). Tell the user to retrieve it with `hal vault status` or `hal creds status`, set `VAULT_CACERT=~/.hal/vault-prod/certs/cert.pem` for the CLI, and that this file is the only copy of the unseal key.
- Prod-only flags: `--key-shares` (default 1), `--key-threshold` (default 1), `--node-id`.
- `--edition ent-hsm` is prod-only. Its default source tag is `2.0.3-ent.hsm`;
  custom tags must end in `.hsm`. `--vault-image` selects the source repository,
  while `--softhsm-base-image` / `--softhsm-base-tag` select the runtime base.
- Update does not persist create flags; repeat `--mode prod --edition ent-hsm`
  and any image overrides when reconciling an HSM deployment.
- Feature integrations that embed URLs into Vault (OIDC callbacks, PKI AIA/CRL, OS plugin register) are validated against dev mode today; prefer dev mode when demoing those specific features.

## Validation

- Confirm Vault is reachable after deployment.
- Summarize UI/API endpoints (`http://vault.localhost:8200`) and next steps.
- Vault deployment no longer auto-registers observability artifacts.

## Observability Notes

- Use explicit lifecycle commands for Vault observability artifacts:
	- `hal vault obs create`
	- `hal vault obs update`
	- `hal vault obs delete`
	- `hal vault obs status`
- `hal vault obs create` expects the obs stack to already be running; otherwise it should ask the user to run `hal obs create` first.

## Edge Cases

- If ports are already in use, explain likely conflicts and next actions.
- If an old broken deployment exists, suggest update (`hal vault update`) or delete (`hal vault delete`) path.
- If Enterprise is requested without `VAULT_LICENSE`, explain the failure and provide the exact export + redeploy command.
- If `ent-hsm` is requested without prod mode or with a non-HSM tag, provide
  `hal vault create --edition ent-hsm --mode prod` as the corrective command.
