---
name: token
description: Manage Terraform token workflow in hal. Use when the user asks about token generation, rotation, or inspection.
---

# Terraform Token Workflow

## Intent

Handle hal terraform token requests with a stable lifecycle pattern.

## Primary Command

- hal terraform token

## Validation

- Confirm command output and summarize the resulting lab state.
- If applicable, suggest the next expected command in the lifecycle.

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest force or cleanup path when supported.
