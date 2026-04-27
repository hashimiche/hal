---
name: vault
description: Route user intent to the right Vault lab skill in hal, including CE versus Enterprise constraints, observability wiring, and full-stack scenario guidance.
---

# Vault Skill Router

Use this router when the user says Vault but does not clearly specify the exact scenario.

## Product Baseline

- Base create command: `hal vault create`
- Default startup mode: Vault dev mode with root token `root`
- Enterprise path: `hal vault create --edition ent` with `VAULT_LICENSE` exported
- Observability surfaces (when obs is deployed):
	- Grafana: `http://grafana.localhost:3000`
	- Prometheus: `http://prometheus.localhost:9090`
	- Loki: `http://loki.localhost:3100/ready`

If obs comes after Vault, prefer `hal vault obs create` to backfill metrics/dashboard artifacts.

## Routing Rules

- Use audit when the user asks about audit devices, logging, Loki, Grafana, or log shipping.
- Use audit-analysis when the user asks who did what, request tracing, incident review, or audit investigations.
- Use jwt when the user asks about CI or machine auth with JWT, GitLab pipelines, or bound claims.
- Use oidc when the user asks about human SSO, browser login, callback URL issues, or Keycloak.
- Use k8s when the user asks about Kubernetes auth, KinD, VSO, secret refresh demos, or CSI projection.
- Use mariadb when the user asks about database secrets, dynamic DB credentials, or root rotation.
- Use ldap when the user asks about LDAP auth, LDAP secrets engine, dynamic users, static creds, or credential libraries.
- Use create when the user asks for initial Vault bring-up, CE vs Enterprise selection, or obs backfill.
- Use delete when the user asks for full Vault ecosystem teardown.
- Use obs when the user asks to wire Vault metrics into Grafana or Prometheus, or manage Vault observability artifacts.
- Use status when the user asks whether Vault and add-on scenarios are currently healthy.

## Shared Lab Assumptions

- Vault address: `http://vault.localhost:8200`
- Root token: root
- Prefer hal commands for deploy or teardown.
- For post-deploy day-2 operations, provide exact vault read/write commands.

## Enterprise Feature Guardrails

- Sentinel policy packs (RGP/EGP) should be framed as Enterprise-only.
- K8s CSI projection mode (`hal vault k8s enable --csi`) requires Enterprise in this lab.
- If Enterprise-only asks arrive on CE, state the limitation and provide the explicit upgrade command path.

## Quick Triage Sequence

1. Run hal vault status for initial state.
2. If Vault is down, run hal vault create.
3. Determine if CE is sufficient or Enterprise is required for requested features.
4. Route to the correct specialized skill based on the user intent.
5. Verify state with MCP or vault CLI before concluding.
