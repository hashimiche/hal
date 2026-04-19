---
name: deploy
description: Deploy the local Boundary control plane in hal. Use when the user asks to start Boundary, bootstrap controller and worker, or initialize control plane dependencies.
---

# Boundary Deploy Workflow

## Intent

Handle hal boundary create requests with a stable lifecycle pattern.

## Primary Command

- hal boundary create

## Validation

- Confirm command output and summarize the resulting lab state.
- If applicable, suggest the next expected command in the lifecycle.

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest update or cleanup path when supported.
