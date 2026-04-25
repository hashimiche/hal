---
name: obs
description: Deploy, verify, update, and destroy the standalone HAL observability stack (Grafana, Prometheus, Loki, Promtail). Use when the user asks to set up monitoring, wire dashboards, or check the health of the observability stack.
---

# HAL Observability Stack

The `hal obs` subcommand manages a standalone Grafana + Prometheus + Loki + Promtail stack that product-specific observability wiring (`hal vault obs`, `hal terraform obs`, etc.) can attach to.

## Lab Assumptions

- The obs stack is a shared dependency. Deploy it before using `hal vault obs`, `hal terraform obs`, or any product-specific `obs create` command.
- All dashboards are pre-provisioned; no manual import is required.
- Grafana default credentials: `admin` / `admin`.

## Primary Commands

```
hal obs create
hal obs status
hal obs update
hal obs delete
```

## Version Pinning (Optional)

```
hal obs create \
  --loki-version 3.7 \
  --grafana-version main \
  --prom-version main \
  --promtail-version 3.6
```

## Workflow

### Deploy

```
hal obs create
```

Verify it is healthy before wiring products into it:

```
hal obs status
hal status
```

### Wire a product into obs

After the stack is running, enable product-level observability. Examples:

```
hal vault obs create
hal terraform obs create --target both
hal consul obs create
```

### Update

```
hal obs update
```

Pull new image versions or re-apply configuration changes.

### Teardown

```
hal obs delete
```

Tears down Grafana, Prometheus, Loki, and Promtail containers. Product `obs` artifacts are removed by their own lifecycle commands.

## Lab Endpoints

| Service | URL |
|---|---|
| Grafana | http://grafana.localhost:3000 |
| Prometheus | http://prometheus.localhost:9090 |
| Loki | http://loki.localhost:3100/ready |

## Edge Cases

- If a product `obs create` fails with a connection error, the obs stack is probably not running yet. Run `hal obs create` first.
- If Grafana shows no data, confirm the relevant Promtail scrape config is active by checking `hal vault obs status` or the equivalent for the product.
- `hal obs delete` does NOT remove product-specific dashboard artifacts — each product's `obs delete` must be run separately.
