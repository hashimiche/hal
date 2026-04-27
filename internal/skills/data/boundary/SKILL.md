---
name: boundary
description: Route Boundary requests to the correct hal boundary skill for control plane deploy, target setup, access checks, and cleanup.
---

# Boundary Skill Router

Use this router when the user asks for Boundary workflows in hal but has not selected a specific subcommand yet.

## Routing Rules

- Route create/deploy requests to the create skill.
- Route teardown or cleanup requests to the delete skill.
- Route health checks to the status skill.
- Route feature-specific requests to mariadb, ssh, or setup.
- Route observability requests to the obs skill (`hal boundary obs create|update|delete|status`).

## Lab Assumptions

- Prefer hal commands first.
- For post-deploy operations, provide exact CLI commands.
- If Boundary is already running and obs comes later, use `hal boundary obs create` to backfill monitoring artifacts.
