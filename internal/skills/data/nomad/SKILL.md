---
name: nomad
description: Route Nomad requests to deploy, inspect, run jobs, and destroy the local Nomad lab in hal.
---

# Nomad Skill Router

Use this router when the user asks for Nomad workflows in hal but has not selected a specific subcommand yet.

## Routing Rules

- Route create/deploy requests to the create skill.
- Route workload requests to the job skill.
- Route health checks to the status skill.
- Route teardown requests to the delete skill.
- Route observability requests to the obs skill (`hal nomad obs create|update|delete|status`).

## Lab Assumptions

- Prefer hal commands first.
- For post-deploy operations, provide exact CLI commands.
