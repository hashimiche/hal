---
name: destroy
description: Destroy local Vault lab resources in hal. Use when the user asks for cleanup, reset, or complete teardown of the Vault environment.
---

# Vault Destroy Workflow

## Intent

Handle hal vault delete requests with a stable lifecycle pattern.

## Primary Command

- hal vault delete

## Validation

- Confirm containers/resources are removed.
- Summarize what remains and what to run next.

## Edge Cases

- If Vault is partially down, still guide user through cleanup.
- If dependent labs remain, call out the effect on those integrations.
