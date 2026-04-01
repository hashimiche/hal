---
name: destroy
description: Destroy local Nomad resources in hal. Use for cleanup and reset.
---

# Nomad Destroy Workflow

## Intent

Handle hal nomad destroy requests with a stable lifecycle pattern.

## Primary Command

- hal nomad destroy

## Validation

- Confirm command output and summarize the resulting lab state.
- If applicable, suggest the next expected command in the lifecycle.

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest force or cleanup path when supported.
