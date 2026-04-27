---
name: nomad-obs
description: Manage Nomad observability artifacts in hal — Prometheus scrape targets and Grafana dashboards. Use when the user asks to wire Nomad metrics into the obs stack or manage Nomad monitoring artifacts.
---

# Nomad Observability Skill

`hal nomad obs` manages Nomad-specific monitoring artifacts attached to the shared HAL obs stack.

## Prerequisites

```
hal obs status
hal obs create    # if not running
```

## Primary Commands

```
hal nomad obs create    # register Prometheus target + import Nomad Grafana dashboard
hal nomad obs update    # re-register targets and re-import dashboard (idempotent)
hal nomad obs delete    # remove Nomad Prometheus target and Grafana dashboard
hal nomad obs status    # show obs artifact registration state for Nomad
```

## Key Behaviours

- Nomad obs onboarding is explicit and opt-in — `hal nomad create` does not auto-register monitoring artifacts.
- Official Nomad dashboard is auto-downloaded and imported into Grafana folder `HAL`.
- `hal nomad obs create` fails with a clear message if the obs stack is not running.

## Lab Assumptions

- Grafana: `http://grafana.localhost:3000`
- Prometheus: `http://prometheus.localhost:9090`
- Nomad cluster runs via Multipass VM (`hal-nomad`)
