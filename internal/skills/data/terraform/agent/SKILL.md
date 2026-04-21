---
name: agent
description: Manage Terraform Enterprise custom agent-pool lifecycle in hal. Use for `hal terraform agent` status, enable, disable, and troubleshooting local TFE agent registration.
---

# Terraform Agent Pool Workflow

This skill handles local TFE custom agent pool lifecycle through `hal terraform agent`.

## Intent

Use this skill when the user asks to:

- create or reuse a TFE agent pool
- start a local TFC agent container
- stop and revoke HAL-managed agent runtime
- verify whether a pool has registered agents
- troubleshoot `no registered agents` execution issues

## Core Commands

- `hal terraform agent` for smart status
- `hal terraform agent enable` to create/reuse pool and run `hal-tfe-agent`
- `hal terraform agent disable` to stop agent and revoke HAL-managed token

## Lab Assumptions

- TFE local endpoint: `https://tfe.localhost:8443`
- TFE runtime container: `hal-tfe`
- Agent container: `hal-tfe-agent`
- Default pool name: `hal-agent-pool`
- Default agent image: `hashicorp/tfc-agent:1.28`

## Validation

- Confirm TFE runtime is running before enable.
- Confirm agent container state with `hal terraform agent`.
- If needed, inspect logs with `docker logs --tail 50 hal-tfe-agent`.
- In TFE UI, switch workspace execution mode to Agent and select the desired pool.

## Troubleshooting Notes

- If agent registration fails with TLS trust errors, keep HAL-generated cert wiring in place and avoid bypassing TLS verification.
- If runtime is stale, use `hal terraform agent enable --update` to rotate token and recreate the container.
- If pool assignment fails in UI, verify the pool has at least one registered running agent.
