---
name: deploy
description: Deploy the local Terraform demo environment in hal. Use to provision and initialize Terraform lab resources.
---

# Terraform Deploy Workflow

## Intent

Handle hal terraform deploy requests with a stable lifecycle pattern.

## Primary Command

- hal terraform deploy

## Validation

- Confirm command output and summarize the resulting lab state.
- If applicable, suggest the next expected command in the lifecycle.
- If observability is running, mention automatic target/dashboard artifact updates.

## Observability Notes

- Terraform deploy uses shared observability artifact registration.
- TFE metrics target file is refreshed automatically when obs is active.
- Official Terraform Enterprise dashboard is imported automatically into Grafana folder HAL.
- If TFE is already running and obs comes later, use `hal terraform deploy --configure-obs` to backfill only monitoring artifacts.
- `--configure-obs` expects the obs stack to already be running; otherwise it should stop and ask the user to run `hal obs deploy` first.

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest force or cleanup path when supported.

## Networking Notes

- HAL deploys a small local ingress proxy (`hal-tfe-proxy`) in front of TFE.
- This avoids requiring privileged host port 443 on rootless Podman.
- User-facing URL remains `https://tfe.localhost:8443` in this mode.
- Deploy validation should include both health endpoint and `/app` reachability (to catch redirect loops).
