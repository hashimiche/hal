---
name: destroy
description: Destroy local Vault lab resources in hal. Use when the user asks for cleanup, reset, or complete teardown of the Vault environment.
---

# Vault Destroy Workflow

## Intent

Handle hal vault delete requests with a stable lifecycle pattern.

## Primary Command

- `hal vault delete` — product teardown, including the shared KinD cluster (no co-tenant guard) when HAL created it
- `hal delete` / `hal daisy` — global teardown of all HAL containers, HAL's KinD cluster, volumes, the images HAL builds for `hal-tfe-cli` and the Agentic IAM lab, and `hal-net`. They only act on what HAL created: a KinD cluster the user made is left alone

## Validation

- Confirm containers/resources are removed.
- Summarize what remains and what to run next.
- HAL's KinD cluster is named `kind` with a node `kind-control-plane` (not `hal-*`) attached to `hal-net`. Global teardown finds it through the container engine even if `kind get clusters` fails. A KinD cluster with no node on `hal-net` is the user's own and is reported as left alone, not deleted. If the user expected it gone, tell them to run `kind delete cluster` themselves.

## Edge Cases

- If Vault is partially down, still guide user through cleanup.
- If dependent labs remain, call out the effect on those integrations.
- On Podman 6, older kind CLIs fail `kind get clusters` (`cannot index slice/array with type string`). That is not a teardown failure: HAL lists clusters through the container engine and removes only the HAL cluster's own nodes. Do not tell the user to re-run daisy solely because of that warning on an older binary; current HAL swallows it.
