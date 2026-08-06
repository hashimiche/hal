---
name: deploy
description: Deploy the local Terraform demo environment in hal. Use to provision and initialize Terraform lab resources.
---

# Terraform Deploy Workflow

## Intent

Handle hal terraform create requests with a stable lifecycle pattern.

## Primary Command

- hal terraform create

## Validation

- Confirm command output and summarize the resulting lab state.
- If applicable, suggest the next expected command in the lifecycle.
- Terraform deployment no longer auto-registers observability artifacts.

## Vault Dynamic Provider Credentials

- Use `hal vault create` before `hal terraform create --vault-enabled`.
- The flag wires the primary TFE issuer to `hal-vault` through the `jwt-tfe`
  auth mount and a global `TFC_VAULT_*` variable set.
- `--target both` wires primary and continues the twin deployment.
- Do not propose `--target twin --vault-enabled`; twin issuer wiring is not
  implemented and HAL returns an actionable error.
- If TFE is already running, rerunning `hal terraform create --vault-enabled`
  applies only the missing Vault wiring and does not require a redeploy.

## Observability Notes

- Use explicit lifecycle commands for Terraform observability artifacts:
	- `hal terraform obs create`
	- `hal terraform obs update`
	- `hal terraform obs delete`
	- `hal terraform obs status`
- `hal terraform obs create` expects the obs stack to already be running; otherwise it should ask the user to run `hal obs create` first.

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest update or cleanup path when supported.

## Networking Notes

- HAL deploys a small local ingress proxy (`hal-tfe-proxy`) in front of TFE.
- This avoids requiring privileged host port 443 on rootless Podman.
- User-facing URL remains `https://tfe.localhost:8443` in this mode.
- Deploy validation should include both health endpoint and `/app` reachability (to catch redirect loops).
- Deploy should preserve UI/log usability for archivist object links by keeping links on `:8443` in proxied responses.

## Runtime Notes

- TFE task-worker `agent-run` must keep `/tmp/terraform` writable; the read-only mount is TFE's required disk cache (`TFE_DISK_CACHE_VOLUME_NAME`, cannot be removed) and causes remote plan/apply failures ("read-only file system") when downloading Terraform binaries. TFE renders the run config from `/etc/task-worker/config.hcl.tmpl` ~40ms into boot, so HAL **bind-mounts** a patched template (extracted from the exact image, `readonly` flipped to `false`) over that path at container creation — a post-start patch cannot win the race and this image has no supervisord to restart the task-worker.
