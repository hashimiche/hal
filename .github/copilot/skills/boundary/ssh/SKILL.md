---
name: ssh
description: Deploy or remove the Boundary SSH target lab in hal. Use when the user asks for Linux VM SSH access through Boundary.
---

# Boundary Ssh Workflow

## Intent

Handle hal boundary ssh requests with a stable lifecycle pattern.

## Primary Command

- hal boundary ssh

## Validation

- Confirm command output and summarize the resulting lab state.
- If applicable, suggest the next expected command in the lifecycle.

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest force or cleanup path when supported.
