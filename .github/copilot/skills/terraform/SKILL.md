---
name: terraform
description: Route Terraform lab requests to deploy, destroy, inspect status, and manage token workflows in hal.
---

# Terraform Skill Router

Use this router when the user asks for Terraform workflows in hal but has not selected a specific subcommand yet.

## Routing Rules

- Route deploy requests to the deploy skill.
- Route token requests to the token skill.
- Route status checks to the status skill.
- Route teardown requests to the destroy skill.

## Lab Assumptions

- Prefer hal commands first.
- For post-deploy operations, provide exact CLI commands.
