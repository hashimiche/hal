---
name: terraform
description: Route Terraform lab requests to deploy, destroy, inspect status, and manage workspace automation in hal.
---

# Terraform Skill Router

Use this router when the user asks for Terraform workflows in hal but has not selected a specific subcommand yet.

## Routing Rules

- Route create/deploy requests to the create skill.
- Route status checks to the status skill.
- Route teardown requests to the delete skill.
- Route twin-instance requests to target-based product lifecycle commands (`hal terraform create|update|status|delete --target twin`).
- Route workspace wiring requests to `hal terraform vcs-workflow enable`.
- Route helper-shell, TFX, self-signed cert, or `hal tf api-workflow` requests to the cli skill.
- Route custom agent pool requests to `hal terraform agent enable` and the agent skill.
- Route observability requests to the obs skill (`hal terraform obs create|update|delete|status`).

## Lab Assumptions

- Prefer hal commands first.
- For post-deploy operations, provide exact CLI commands.
- Default local TFE endpoint is `https://tfe.localhost:8443`.
- Terraform workspace bootstrap flow is `hal terraform vcs-workflow enable` after deploy.
- Terraform custom agent flow is `hal terraform agent enable` after deploy.
- Terraform API helper flow is `hal terraform api-workflow enable` (opens shell automatically).
- Terraform helper subcommands are lifecycle-only (`status|enable|disable|update`), so do not suggest `create|delete` for `vcs-workflow` or `agent`.
- Default local lab password baseline for current Terraform/GitLab flows is `hal9000FTW` unless the user overrides flags.
