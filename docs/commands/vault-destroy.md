# HAL Vault Destroy Command Spec

## Command
- `hal vault delete`

## Purpose
Destroy local Vault instance and associated integration resources.

## Related
- Parent namespace: [vault.md](vault.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal vault delete --help`:
```text
-h, --help   help for destroy
```
- Global flags: `--debug`, `--dry-run`, `--verbose`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.
- Removes the Vault container, its volumes, and the ecosystem containers.
- Removes the shared `hal-vault-mariadb` too (every consumer of it is a Vault lab) and clears its `vault-mariadb` consumers in `~/.hal/shared-services.json`.
- Removes the Agentic IAM lab's containers (`hal-agentic-iam-chat`, `hal-agentic-iam-agent`) and `~/.hal/agentic-iam/`. Its image stays (a build cache).
- Deregisters both Vault consumers of the shared Authentik (`vault-oidc`, `vault-agentic-iam`) and stops Authentik only if no other lab (`hal tf saml`) uses it. When Authentik stays up, the Agentic IAM lab's Authentik objects are removed from it.
- For a production instance, also removes `~/.hal/vault-prod/` (config, TLS certs,
  and `init.json`) so the saved unseal key / root token are never stranded.

## Example
```bash
hal vault delete
```
