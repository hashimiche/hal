# HAL Vault Command Spec

## Base Command
- Command: `hal vault`
- Purpose: manage local Vault deployments and integrations
- Default behavior: runs `hal vault status`

## Subcommands
- `hal vault create`
  - Deploy a local Vault instance and baseline setup
  - Spec: [vault-deploy.md](vault-deploy.md)

- `hal vault status`
  - Deep status for Vault container, API, and related integrations
  - Spec: [vault-status.md](vault-status.md)

- `hal vault delete`
  - Destroy Vault and associated extension resources
  - Spec: [vault-destroy.md](vault-destroy.md)

- `hal vault audit`
  - Manage/check Vault audit logging state
  - Spec: [vault-audit.md](vault-audit.md)

- `hal vault oidc`
  - Deploy and configure OIDC flow (Keycloak integration)
  - Spec: [vault-oidc.md](vault-oidc.md)

- `hal vault jwt`
  - Simulate GitLab-backed JWT CI/CD auth flow
  - Spec: [vault-jwt.md](vault-jwt.md)

- `hal vault k8s`
  - Deploy KinD and Vault Secrets Operator scenario
  - Spec: [vault-k8s.md](vault-k8s.md)

- `hal vault ldap`
  - Deploy and configure OpenLDAP auth and related flows
  - Spec: [vault-ldap.md](vault-ldap.md)

- `hal vault database`
  - Configure dynamic DB credentials scenario (defaults to `mariadb`; `postgres` planned)
  - Spec: [vault-database.md](vault-database.md)

## Local Lab Assumptions
- Vault local endpoint defaults to `http://127.0.0.1:8200`
- Typical local root token assumption: `root`
- Day-2 policy/auth tuning should be performed with direct `vault` CLI/API operations after deployment

## Sources
- Namespace: `cmd/vault/vault.go`
- Subcommands: `cmd/vault/*.go`
