---
name: health
description: Manage the hal-health sidecar container that serves live ecosystem state snapshots to HAL Plus and other consumers. Use when the user asks to refresh the status snapshot, recreate the health container, or troubleshoot HAL Plus product state.
---

# HAL Health Skill

The `hal health` namespace manages the `hal-health` sidecar container. It builds a frozen snapshot of the running HAL ecosystem and serves it at `http://hal-health:9001/api/status` on `hal-net` so HAL Plus and AI clients can query product state without touching the container engine directly.

## Primary Commands

```
hal health create    # create (or recreate) the hal-health container with a fresh snapshot
hal health update    # refresh the snapshot — use after ecosystem changes outside normal lifecycle
hal health delete    # remove the hal-health container
```

## When To Use

- **`hal health create`**: first-time setup or after `hal health delete`.
- **`hal health update`**: manual escape hatch after a product was added/removed outside the normal `hal <product> create/delete` flow (e.g. manual container changes).
- **`hal health delete`**: clean up the sidecar without affecting any product containers.

## Architecture Notes

- The `hal-health` container reuses `ghcr.io/hashimiche/hal-mcp:latest` with `--entrypoint /usr/local/bin/hal` and `health _serve` as args.
- The snapshot shape: `{ timestamp, engine, products: [{ product, state, health, reason, endpoint, containers, features: [...] }] }`.
- The container is a no-op host — it reads the `HAL_STATUS_DATA` env var injected at start time and never touches the container engine socket.
- `global.RefreshHalStatus` is called automatically after all product `create`/`delete` commands — manual `hal health update` is only needed for out-of-band changes.
- `hal health` is a no-op if `hal-net` does not exist or `ghcr.io/hashimiche/hal-mcp:latest` is not present locally.

## Lab Assumptions

- HAL Plus must be running on `hal-net` for `hal-health` to be reachable by HAL Plus.
- HAL Plus falls back to direct product endpoint probing if `hal-health` is unavailable.
