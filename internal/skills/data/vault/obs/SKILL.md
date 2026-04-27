---
name: vault-obs
description: Manage Vault observability artifacts in hal — Prometheus scrape targets and Grafana dashboards. Use when the user asks to wire Vault metrics into the obs stack, import the Vault dashboard, or remove Vault monitoring artifacts.
---

# Vault Observability Skill

`hal vault obs` manages Vault-specific monitoring artifacts (Prometheus targets and Grafana dashboards) attached to the shared HAL obs stack.

## Prerequisites

The shared obs stack must be running before using these commands:

```
hal obs status
hal obs create    # if not running
```

## Primary Commands

```
hal vault obs create    # register Prometheus target + import Vault Grafana dashboard
hal vault obs update    # re-register targets and re-import dashboard (idempotent)
hal vault obs delete    # remove Vault Prometheus target and Grafana dashboard
hal vault obs status    # show obs artifact registration state for Vault
```

## Key Behaviours

- Vault obs onboarding is explicit and opt-in — `hal vault create` no longer auto-registers monitoring artifacts.
- Official Vault dashboard is auto-downloaded and imported into Grafana folder `HAL`.
- Dashboard JSON is normalized so panel datasources resolve to `hal-prometheus`.
- `hal vault obs create` will fail with a clear message if the obs stack is not running.

## Lab Assumptions

- Grafana: `http://grafana.localhost:3000` (default credentials: `admin` / `admin`)
- Prometheus: `http://prometheus.localhost:9090`
- Vault metrics endpoint: `http://vault.localhost:8200/v1/sys/metrics`
