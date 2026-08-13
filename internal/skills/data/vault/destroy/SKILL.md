---
name: destroy
description: Destroy local Vault lab resources in hal. Use when the user asks for cleanup, reset, or complete teardown of the Vault environment.
---

# Vault Destroy Workflow

## Intent

Handle hal vault delete requests with a stable lifecycle pattern.

## Primary Command

- `hal vault delete` — product teardown, including the shared KinD cluster (no co-tenant guard)
- `hal delete` / `hal daisy` — global teardown of all HAL containers, KinD nodes, volumes, and `hal-net`

## Validation

- Confirm containers/resources are removed.
- Summarize what remains and what to run next.
- KinD nodes are named `kind-control-plane` (not `hal-*`). Global teardown always sweeps containers labeled `io.x-k8s.kind.cluster` even if `kind get clusters` fails.

## Edge Cases

- If Vault is partially down, still guide user through cleanup.
- If dependent labs remain, call out the effect on those integrations.
- On Podman 6, older kind CLIs fail `kind get clusters` (`cannot index slice/array with type string`). That is not a teardown failure: HAL lists clusters via `{{.Label "io.x-k8s.kind.cluster"}}` and force-removes leftover nodes. Do not tell the user to re-run daisy solely because of that warning on an older binary; current HAL swallows it.
