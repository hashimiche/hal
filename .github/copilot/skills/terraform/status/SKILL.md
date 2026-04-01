---
name: status
description: Inspect Terraform lab status in hal. Use to verify current state before applying lifecycle changes.
---

# Terraform Status Workflow

## Intent

Handle hal terraform status requests with a stable lifecycle pattern.

## Primary Command

- hal terraform status

## Validation

- Confirm command output and summarize the resulting lab state.
- If applicable, suggest the next expected command in the lifecycle.

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest force or cleanup path when supported.
