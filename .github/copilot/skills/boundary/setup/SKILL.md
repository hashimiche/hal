---
name: setup
description: Run the Boundary setup helper workflow in hal. Use when the user explicitly asks for boundary setup orchestration.
---

# Boundary Setup Workflow

## Intent

Handle hal boundary setup requests with a stable lifecycle pattern.

## Primary Command

- hal boundary setup

## Validation

- Confirm command output and summarize the resulting lab state.
- If applicable, suggest the next expected command in the lifecycle.

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest update or cleanup path when supported.
