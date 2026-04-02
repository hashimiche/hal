---
name: deploy
description: Deploy local Nomad in hal. Use for starting Nomad server and client resources.
---

# Nomad Deploy Workflow

## Intent

Handle hal nomad deploy requests with a stable lifecycle pattern.

## Primary Command

- hal nomad deploy

## Validation

- Confirm command output and summarize the resulting lab state.
- If applicable, suggest the next expected command in the lifecycle.
- If observability is running, call out that target/dashboard artifacts are handled automatically.

## Observability Notes

- Nomad deploy now calls shared observability artifact registration.
- Prometheus target file is refreshed automatically when obs is active.
- Official Nomad dashboard is imported automatically into Grafana folder HAL.
- If Nomad is already running and obs comes later, use `hal nomad deploy --configure-obs` to backfill only monitoring artifacts.
- `--configure-obs` expects the obs stack to already be running; otherwise it should stop and ask the user to run `hal obs deploy` first.

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest force or cleanup path when supported.
