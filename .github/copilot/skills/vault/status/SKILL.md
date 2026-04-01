---
name: status
description: Check local Vault ecosystem status in hal. Use when the user asks whether Vault and its auth or secrets integrations are healthy.
---

# Vault Status Workflow

## Intent

Handle hal vault status requests with a stable lifecycle pattern.

## Primary Command

- hal vault status

## Validation

- Summarize service health and configured integrations.
- Recommend the next lifecycle command based on state.

## Edge Cases

- If Vault is unreachable, suggest deploy or restart path.
- If environment is partially degraded, suggest targeted reset command.
