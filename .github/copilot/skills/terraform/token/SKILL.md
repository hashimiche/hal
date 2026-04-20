---
name: token
description: Deprecated Terraform token workflow note. Use when users ask about `hal terraform token` so you can redirect to the automated workspace flow.
---

# Terraform Token Workflow (Deprecated)

## Intent

`hal terraform token` has been removed from the CLI.

## Recommended Replacement Flow

- hal terraform create
- hal terraform vcs-workflow enable
- hal terraform status

## Validation

- Explain that token minting and workspace VCS wiring are now fully automated.
- Confirm `workspace` readiness via `hal terraform status`.

## Edge Cases

- If user still asks for manual token handling, explain that the command is intentionally removed and provide the replacement sequence above.
