---
name: deploy
description: Deploy local Consul in hal. Use for starting the Consul control plane.
---

# Consul Deploy Workflow

## Intent

Handle hal consul deploy requests with a stable lifecycle pattern.

## Primary Command

- hal consul deploy

## Validation

- Confirm command output and summarize the resulting lab state.
- If applicable, suggest the next expected command in the lifecycle.
- If observability is already running, mention that Consul target/dashboard artifacts are handled automatically.

## Observability Notes

- Consul deploy uses shared observability artifact registration.
- When `hal obs deploy` is up, Prometheus target updates are automatic.
- Official Consul dashboard import is automatic in Grafana folder HAL.
- If Consul is already running and obs comes later, use `hal consul deploy --configure-obs` to backfill only monitoring artifacts.
- `--configure-obs` expects the obs stack to already be running; otherwise it should stop and ask the user to run `hal obs deploy` first.

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest force or cleanup path when supported.
