---
name: destroy
description: Destroy local Consul resources in hal. Use for teardown and reset.
---

# Consul Destroy Workflow

## Intent

Handle hal consul destroy requests with a stable lifecycle pattern.

## Primary Command

- hal consul destroy

## Validation

- Confirm command output and summarize the resulting lab state.
- If applicable, suggest the next expected command in the lifecycle.

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest force or cleanup path when supported.
