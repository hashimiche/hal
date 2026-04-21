---
name: deploy
description: Deploy local Nomad in hal. Use for starting Nomad server and client resources.
---

# Nomad Deploy Workflow

## Intent

Handle hal nomad create requests with a stable lifecycle pattern.

## Primary Command

- hal nomad create

## Validation

- Confirm command output and summarize the resulting lab state.
- If applicable, suggest the next expected command in the lifecycle.
- Nomad deployment no longer auto-registers observability artifacts.

## Observability Notes

- Use explicit lifecycle commands for Nomad observability artifacts:
	- `hal nomad obs create`
	- `hal nomad obs update`
	- `hal nomad obs delete`
	- `hal nomad obs status`
- `hal nomad obs create` expects the obs stack to already be running; otherwise it should ask the user to run `hal obs create` first.

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest update or cleanup path when supported.
