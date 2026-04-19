---
name: destroy
description: Destroy Terraform lab resources in hal. Use for teardown and clean reset.
---

# Terraform Destroy Workflow

## Intent

Handle hal terraform delete requests with a stable lifecycle pattern.

## Primary Command

- hal terraform delete

## Validation

- Confirm command output and summarize the resulting lab state.
- If applicable, suggest the next expected command in the lifecycle.

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest update or cleanup path when supported.
