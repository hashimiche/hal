---
name: job
description: Manage Nomad jobs in hal. Use for running, inspecting, or stopping sample workloads.
---

# Nomad Job Workflow

## Intent

Handle hal nomad job requests with a stable lifecycle pattern.

## Primary Command

- hal nomad job

## Validation

- Confirm command output and summarize the resulting lab state.
- If applicable, suggest the next expected command in the lifecycle.

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest force or cleanup path when supported.
