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

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest force or cleanup path when supported.
