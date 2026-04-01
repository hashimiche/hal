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

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest force or cleanup path when supported.
