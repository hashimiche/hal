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
- Enterprise deploy requires `hal vault create --edition ent` and `VAULT_LICENSE` exported.

## Validation

- Confirm Vault is reachable after deployment.
- Summarize UI/API endpoints (`http://vault.localhost:8200`) and next steps.
- If Grafana/Prometheus are already running, mention that Vault metrics target registration and dashboard import happen automatically.

## Observability Notes

- Deploy path now runs shared observability artifact registration.
- Prometheus target file is written when obs is active.
- Vault dashboard is downloaded/imported automatically into Grafana folder HAL (no manual import step needed).
- If Vault is already running and obs comes later, use `hal vault create --configure-obs` to backfill only monitoring artifacts.
- `--configure-obs` expects the obs stack to already be running; otherwise it should stop and ask the user to run `hal obs create` first.

## Edge Cases

- If ports are already in use, explain likely conflicts and next actions.
- If an old broken deployment exists, suggest force or teardown path.
- If Enterprise is requested without `VAULT_LICENSE`, explain the failure and provide the exact export + redeploy command.
