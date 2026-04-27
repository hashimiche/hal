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
