---
name: boundary-obs
description: Manage Boundary observability artifacts in hal — Prometheus scrape targets and Grafana dashboards. Use when the user asks to wire Boundary metrics into the obs stack or manage Boundary monitoring artifacts.
---

# Boundary Observability Skill

`hal boundary obs` manages Boundary-specific monitoring artifacts attached to the shared HAL obs stack.

## Prerequisites

```
hal obs status
hal obs create    # if not running
```

## Primary Commands

```
hal boundary obs create    # register Prometheus target + import Boundary Grafana dashboard
hal boundary obs update    # re-register targets and re-import dashboard (idempotent)
hal boundary obs delete    # remove Boundary Prometheus target and Grafana dashboard
hal boundary obs status    # show obs artifact registration state for Boundary
```

## Key Behaviours

- Boundary obs onboarding is explicit and opt-in — `hal boundary create` does not auto-register monitoring artifacts.
- Official Boundary dashboard is auto-downloaded and imported into Grafana folder `HAL`.
- `hal boundary obs create` fails with a clear message if the obs stack is not running.

## Lab Assumptions

- Grafana: `http://grafana.localhost:3000`
- Prometheus: `http://prometheus.localhost:9090`
- Boundary controller endpoint: `http://boundary.localhost:9200`
