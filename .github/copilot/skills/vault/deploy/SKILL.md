---
name: deploy
description: Deploy local Vault in hal. Use when the user asks to start Vault, initialize the local control plane, or recover from Vault being offline.
---

# Vault Deploy Workflow

## Intent

Handle hal vault deploy requests with a stable lifecycle pattern.

## Primary Command

- hal vault deploy

## Validation

- Confirm Vault is reachable after deployment.
- Summarize UI/API endpoints and next steps.
- If Grafana/Prometheus are already running, mention that Vault metrics target registration and dashboard import happen automatically.

## Observability Notes

- Deploy path now runs shared observability artifact registration.
- Prometheus target file is written when obs is active.
- Vault dashboard is downloaded/imported automatically into Grafana folder HAL (no manual import step needed).

## Edge Cases

- If ports are already in use, explain likely conflicts and next actions.
- If an old broken deployment exists, suggest force or teardown path.
