# 5. One catalog for every lab secret, and a credential tree of what is running

- **Status:** Accepted on 2026-10-21.
- **Branch for implementation:** `feature/lab-secrets-catalog`
- **Working notes:** decided in a design interview on 2026-10-21. Terms are
  defined in `CONTEXT.md` under "Lab secrets".

## Context

Every lab invented its own credentials where it happened to need them: the TFE
admin password in `cmd/terraform/defaults.go`, the GitLab root password as a flag
default in `cmd/terraform/vcs-workflow.go`, the OIDC demo users inline in the
`CreateUser` calls of `cmd/vault/oidc.go`, the UserPass demo user inline in
`cmd/vault/userpass.go`, the LDAP seeds inside an LDIF string literal. A second,
hand-maintained copy of some of them lived in `cmd/creds/creds.go` so that
`hal creds status` had something to print, and a third copy lived inside
`CollectActiveCredentials` in the same file for the MCP tool.

Two things followed from that. The copies drifted: GitLab was running and had a
login, and no surface showed it. And an operator could not remember the
passwords, because there is no single password — TFE and GitLab each refuse a
weak one, while the Vault demo users are meant to be typed from memory.

The obvious fix, one password everywhere, does not survive contact with the
labs: the products that enforce strength would reject it, and the scenarios that
teach a difference between two identities need two.

## Decision

1. **One catalog.** `internal/labsecrets` declares every fixed lab secret once.
   Products boot from those names and the credential surfaces read the same
   names, so a lab cannot show a credential it did not boot with. A new user,
   password or token is added there, never written as a literal in a command
   file.
2. **Each value stands alone.** The catalog is a list of named values, not one
   shared password. `GitLabRootPassword` and `TFEAdminPassword` read alike today
   because both products enforce strength; either may change without touching
   the other. The comment in `cmd/terraform/defaults.go` that told the next
   reader to reuse one lab password is removed.
3. **Generated secrets keep their boot file and are mirrored.** A lab still
   boots from the file it always booted from — `~/.hal/authentik/env`,
   `~/.hal/vault-prod/init.json`, `~/.hal/tfe-app-api-token`. Create and enable
   additionally copy the operator-facing value, and any catalog password a flag
   overrode, into `~/.hal/lab.creds` (mode `0600`). Delete and disable drop
   those keys. A reader prefers `lab.creds` and falls back to the boot file, so
   a lab that was already up before this existed still shows its secret.
4. **The surfaces show a tree of what is running.** A section per running
   product, a subsection per scenario enabled on it. `hal vault create` shows one
   Vault block; each `hal vault <scenario> enable` adds a block underneath it.
   A shared service sits under the products that registered as its consumers, so
   GitLab appears under Vault when `vault jwt` started it, under Terraform when
   `vcs-workflow` did, and under both when both did. `hal creds status`,
   `hal vault status`, `hal terraform status` and the MCP tool
   `get_active_credentials` all render that same tree; the product commands
   filter it to their own product.
5. **A product license is not a lab secret.** The HashiCorp license a product
   boots with is never written to the catalog or to `lab.creds`, and no surface
   ever prints it.

## Consequences

- Deciding where a credential is shown is no longer a judgement call per
  command: a value that an operator types is an entry in the tree, a value only
  a container types stays in the catalog with no section.
- A password a scenario destroys is shown as the command that reads the live one
  — the LDAP directory admin after `ldap/rotate-root`, `demouser` and `appadmin`
  after the Vault OS enable — rather than as a seed that no longer works.
- `get_active_credentials` now returns the generated TFE API token and the prod
  Vault root token. They were withheld before on the grounds that the MCP
  surface should carry less than the terminal; everything HAL generates for a
  lab is disposable, so the two surfaces were made to agree.
- Boundary, Consul and Nomad keep the credential output they had. They are not
  in the catalog, and the rule above covers Vault, Terraform and the services
  those two share.
