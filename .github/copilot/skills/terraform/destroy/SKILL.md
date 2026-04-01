---
name: destroy
description: Destroy Terraform lab resources in hal. Use for teardown and clean reset.
---

# Terraform Destroy Workflow

## Intent

Handle hal terraform destroy requests with a stable lifecycle pattern.

## Primary Command

- hal terraform destroy

## Validation

- Confirm command output and summarize the resulting lab state.
- If applicable, suggest the next expected command in the lifecycle.

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest force or cleanup path when supported.
