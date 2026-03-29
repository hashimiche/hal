# Hal CLI - Available Skills

The `hal` CLI is a local lab builder for HashiCorp tools. Here are the valid commands:

* `hal vault deploy`: Spins up a local Vault container. Flags: `-e ent` (Enterprise), `-f` (Force clean).
* `hal vault ldap`: Deploys OpenLDAP and configures Vault LDAP auth.
* `hal vault jwt`: Enables the JWT auth method in Vault.
* `hal obs`: Deploys the PLG observability stack (Prometheus, Loki, Grafana, Promtail).

**Example Usage:**
If a user wants to test GitHub Actions, tell them to run `hal vault deploy` followed by `hal vault jwt`.