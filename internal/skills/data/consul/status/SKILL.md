---
name: status
description: Inspect local Consul status in hal. Use to confirm health and connectivity.
---

# Consul Status Workflow

## Intent

Handle hal consul status requests with a stable lifecycle pattern.

## Primary Command

- hal consul status

## Validation

- Confirm command output and summarize the resulting lab state.
- If applicable, suggest the next expected command in the lifecycle.

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest update or cleanup path when supported.
