---
name: deploy
description: Deploy local Consul in hal. Use for starting the Consul control plane.
---

# Consul Deploy Workflow

## Intent

Handle hal consul create requests with a stable lifecycle pattern.

## Primary Command

- hal consul create

## Validation

- Confirm command output and summarize the resulting lab state.
- If applicable, suggest the next expected command in the lifecycle.
- Consul deployment no longer auto-registers observability artifacts.

## Observability Notes

- Use explicit lifecycle commands for Consul observability artifacts:
	- `hal consul obs create`
	- `hal consul obs update`
	- `hal consul obs delete`
	- `hal consul obs status`
- `hal consul obs create` expects the obs stack to already be running; otherwise it should ask the user to run `hal obs create` first.

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest update or cleanup path when supported.
