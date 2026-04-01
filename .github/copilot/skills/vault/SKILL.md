---
name: vault
description: Route user intent to the right Vault lab skill in hal. Use when the user asks for Vault work but the auth or engine type is not yet clear, or when they ask for a general Vault setup, status, reset, or troubleshooting flow.
---

# Vault Skill Router

Use this router when the user says Vault but does not clearly specify the exact lab mode.

## Routing Rules

- Use audit when the user asks about audit devices, logging, Loki, Grafana, or log shipping.
- Use audit-analysis when the user asks who did what, request tracing, incident review, or audit investigations.
- Use jwt when the user asks about CI or machine auth with JWT, GitLab pipelines, or bound claims.
- Use oidc when the user asks about human SSO, browser login, callback URL issues, or Keycloak.
- Use k8s when the user asks about Kubernetes auth, KinD, service account auth, or VSO.
- Use mariadb when the user asks about database secrets, dynamic DB credentials, or root rotation.

## Shared Lab Assumptions

- Vault address: http://127.0.0.1:8200
- Root token: root
- Prefer hal commands for deploy or teardown.
- For post-deploy day-2 operations, provide exact vault read/write commands.

## Quick Triage Sequence

1. Run hal vault status for initial state.
2. If Vault is down, run hal vault deploy.
3. Route to the correct specialized skill based on the user intent.
4. Verify state with MCP or vault CLI before concluding.
