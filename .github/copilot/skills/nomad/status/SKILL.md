---
name: status
description: Inspect local Nomad status in hal. Use to verify scheduler and workload readiness.
---

# Nomad Status Workflow

## Intent

Handle hal nomad status requests with a stable lifecycle pattern.

## Primary Command

- hal nomad status

## Validation

- Confirm command output and summarize the resulting lab state.
- If applicable, suggest the next expected command in the lifecycle.

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest force or cleanup path when supported.
