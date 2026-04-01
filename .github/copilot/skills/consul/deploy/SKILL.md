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

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest force or cleanup path when supported.
