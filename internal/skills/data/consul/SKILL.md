---
name: consul
description: Route Consul requests to deploy, verify, or destroy the local Consul control plane in hal.
---

# Consul Skill Router

Use this router when the user asks for Consul workflows in hal but has not selected a specific subcommand yet.

## Routing Rules

- Route create/deploy requests to the create skill.
- Route teardown requests to the delete skill.
- Route health checks to the status skill.
- Route observability requests to the obs skill (`hal consul obs create|update|delete|status`).

## Lab Assumptions

- Prefer hal commands first.
- For post-deploy operations, provide exact CLI commands.
