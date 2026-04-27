---
name: capacity
description: Show local container engine capacity and HAL deployment estimates. Use when the user asks about available resources, whether there is headroom for a new stack, or what is currently deployed and how heavy it is.
---

# HAL Capacity Skill

The `hal capacity` command surfaces live engine resource usage and per-stack footprint estimates so you can judge headroom before spinning up a new product stack.

## Primary Commands

```
hal capacity                    # current engine view: CPU, RAM, running containers
hal capacity --active           # active heavy deployment composition with per-stack footprint details
hal capacity --pending          # pending heavy deployment impact estimates
```

## When To Use

- Before `hal terraform create` or any other heavy create — to check if the engine has enough headroom.
- After noticing sluggishness — to see which stacks are consuming the most resources.
- To understand relative cost of stacks before deciding what to tear down.

## Key Behaviours

- Memory pressure calculations exclude cache/buffers (pressure memory, not free-cache-inflated baselines).
- Interactive confirmation prompts on heavy creates only trigger when estimated post-create usage exceeds engine limits (CPU > 100% or RAM > machine RAM).
- Podman on macOS exposes richer machine-runtime telemetry than Docker when available.
- Capacity scenario labels are infra-centric (e.g. shared GitLab runner, KinD/VSO flows), not purely product-centric.

## Lab Assumptions

- `hal status` also surfaces a compact capacity summary alongside product state.
- Use `hal capacity --active` to see exactly which running stacks own the current resource footprint before deciding what to scale down.
