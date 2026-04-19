---
name: destroy
description: Destroy the local Boundary environment in hal. Use when the user asks to remove Boundary resources or reset the control plane.
---

# Boundary Destroy Workflow

## Intent

Handle hal boundary delete requests with a stable lifecycle pattern.

## Primary Command

- hal boundary delete

## Validation

- Confirm command output and summarize the resulting lab state.
- If applicable, suggest the next expected command in the lifecycle.

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest update or cleanup path when supported.
