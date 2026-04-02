---
name: boundary
description: Route Boundary requests to the correct hal boundary skill for control plane deploy, target setup, access checks, and cleanup.
---

# Boundary Skill Router

Use this router when the user asks for Boundary workflows in hal but has not selected a specific subcommand yet.

## Routing Rules

- Route deploy requests to the deploy skill.
- Route teardown or cleanup requests to the destroy skill.
- Route health checks to the status skill.
- Route feature-specific requests to mariadb, ssh, or setup.

## Lab Assumptions

- Prefer hal commands first.
- For post-deploy operations, provide exact CLI commands.
- If Boundary is already running and obs comes later, `hal boundary deploy --configure-obs` backfills only monitoring artifacts.
