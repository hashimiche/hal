---
name: terraform-obs
description: Manage Terraform Enterprise observability artifacts in hal — Prometheus scrape targets and Grafana dashboards. Use when the user asks to wire TFE metrics into the obs stack or manage Terraform monitoring artifacts.
---

# Terraform Observability Skill

`hal terraform obs` manages TFE-specific monitoring artifacts attached to the shared HAL obs stack.

## Prerequisites

The shared obs stack must already be running:

```
hal obs status
hal obs create    # if not running
```

## Primary Commands

```
hal terraform obs create              # register Prometheus target + import TFE Grafana dashboard
hal terraform obs create --target both  # register for both primary and twin TFE instances
hal terraform obs update              # re-register targets and re-import dashboard (idempotent)
hal terraform obs delete              # remove TFE Prometheus target and Grafana dashboard
hal terraform obs status              # show obs artifact registration state for Terraform
```

## Key Behaviours

- Terraform obs onboarding is explicit and opt-in — `hal terraform create` does not auto-register monitoring artifacts.
- `hal terraform obs create` is a refresh/manage action — it requires the obs stack to already be running.
- Official TFE dashboard is auto-downloaded and imported into Grafana folder `HAL`.
- Dashboard JSON is normalized so panel datasources resolve to `hal-prometheus`.

## Lab Assumptions

- Grafana: `http://grafana.localhost:3000`
- Prometheus: `http://prometheus.localhost:9090`
- Primary TFE: `https://tfe.localhost:8443`
- Twin TFE (if running): `https://tfe-bis.localhost:8444`
