---
name: consul-obs
description: Manage Consul observability artifacts in hal — Prometheus scrape targets and Grafana dashboards. Use when the user asks to wire Consul metrics into the obs stack or manage Consul monitoring artifacts.
---

# Consul Observability Skill

`hal consul obs` manages Consul-specific monitoring artifacts attached to the shared HAL obs stack.

## Prerequisites

```
hal obs status
hal obs create    # if not running
```

## Primary Commands

```
hal consul obs create    # register Prometheus target + import Consul Grafana dashboard
hal consul obs update    # re-register targets and re-import dashboard (idempotent)
hal consul obs delete    # remove Consul Prometheus target and Grafana dashboard
hal consul obs status    # show obs artifact registration state for Consul
```

## Key Behaviours

- Consul obs onboarding is explicit and opt-in — `hal consul create` does not auto-register monitoring artifacts.
- Official Consul dashboard is auto-downloaded and imported into Grafana folder `HAL`.
- `hal consul obs create` fails with a clear message if the obs stack is not running.

## Lab Assumptions

- Grafana: `http://grafana.localhost:3000`
- Prometheus: `http://prometheus.localhost:9090`
- Consul UI: `http://consul.localhost:8500`
