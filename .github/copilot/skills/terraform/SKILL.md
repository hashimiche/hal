---
name: terraform
description: Route Terraform lab requests to deploy, destroy, inspect status, and manage workspace automation in hal.
---

# Terraform Skill Router

Use this router when the user asks for Terraform workflows in hal but has not selected a specific subcommand yet.

## Routing Rules

- Route deploy requests to the deploy skill.
- Route status checks to the status skill.
- Route teardown requests to the destroy skill.
- Route workspace wiring requests to `hal terraform workspace -e`.

## Lab Assumptions

- Prefer hal commands first.
- For post-deploy operations, provide exact CLI commands.
- Default local TFE endpoint is `https://tfe.localhost:8443`.
- Terraform workspace bootstrap flow is `hal terraform workspace -e` after deploy.
- Default local lab password baseline for current Terraform/GitLab flows is `hal9000FTW` unless the user overrides flags.
