---
name: status
description: Check Boundary and target ecosystem status in hal. Use when the user asks if Boundary, MariaDB target, or SSH target is up.
---

# Boundary Status Workflow

## Intent

Handle hal boundary status requests with a stable lifecycle pattern.

## Primary Command

- hal boundary status

## Validation

- Confirm command output and summarize the resulting lab state.
- If applicable, suggest the next expected command in the lifecycle.

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest force or cleanup path when supported.
