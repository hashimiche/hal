---
name: twin
description: Manage a second local Terraform Enterprise instance (twin) alongside the primary in hal. Use for `hal terraform twin`, `hal tf twin`, `hal tf bis`, or `--target twin` flags.
---

# Terraform Twin Skill

This skill handles the lifecycle of a second local TFE instance — the "twin" — through `hal terraform twin` and `--target twin` flags on other terraform subcommands.

## Intent

Use this skill when the user asks to:

- provision a second TFE instance reusing the primary ecosystem (MinIO, Redis, Postgres)
- check the status of the twin TFE instance
- tear down the twin while preserving the primary
- run api-workflow, vcs-workflow, or agent on the twin instance
- troubleshoot twin-specific certificate or networking issues

## Core Commands

- `hal terraform twin` — smart status view (default when no subcommand given)
- `hal terraform twin enable` — provision the twin TFE instance
- `hal terraform twin disable` — tear down the twin TFE instance only
- `hal terraform twin update` — recreate the twin container in-place
- `hal terraform create --target twin` — alternative deploy entry point

Short aliases: `hal tf twin enable`, `hal tf bis enable`, `hal tf dup enable`

## Twin-Aware Sibling Commands

When the user scopes other workflows to the twin, use `--target twin`:

- `hal terraform vcs-workflow enable --target twin`
- `hal terraform api-workflow enable --target twin`
- `hal terraform agent enable --target twin`
- `hal terraform status --target twin`
- `hal terraform delete --target twin`

## Lab Assumptions

- Primary TFE endpoint: `https://tfe.localhost:8443`
- Twin TFE endpoint: configurable port (default `https://tfe-bis.localhost:8444`)
- Twin core container: `hal-tfe-bis` (default, override with `--twin-container-name`)
- Twin proxy container: `hal-tfe-bis-proxy`
- Twin API helper container: `hal-tfe-bis-api`
- Twin agent container: `hal-tfe-bis-agent`
- Prerequisite: primary TFE must be running before enabling the twin
- The twin reuses `hal-tfe-db`, `hal-tfe-redis`, `hal-tfe-minio` from the primary deployment

## Key Flags

- `--twin-container-name` — core container name (default `hal-tfe-bis`)
- `--twin-hostname` — hostname for TLS cert and ingress (default `tfe-bis.localhost`)
- `--twin-port` — HTTPS port for the twin (default `8444`)
- `--twin-password` — initial admin password for the twin (default `hal9000FTW`)
- `--twin-org` — TFE org name for the twin (default `hal`)
- `--auto-approve` — skip confirmation prompts

## Validation

- Confirm primary TFE is running before enable.
- Confirm twin container state with `hal terraform twin` or `hal terraform status --target twin`.
- After enable, access twin UI at the configured twin endpoint.
- If TLS errors occur, keep HAL-generated cert wiring; do not bypass TLS verification.

## Troubleshooting Notes

- If twin fails to start, confirm there is no port conflict on the twin HTTPS port.
- Use `hal terraform twin update` to recreate the container without tearing down the primary.
- Use `hal terraform delete --target twin` to clean up twin resources only.
