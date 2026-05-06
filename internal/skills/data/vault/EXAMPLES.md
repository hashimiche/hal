# Example Transcripts

## Example 1: Route Ambiguous Vault Request

User: Help me set up Vault auth in this lab.

Assistant: I start with global state and then route to the right Vault sub-workflow.

    hal vault status

Assistant: If you need machine auth, I route to `hal vault jwt enable`; if you need human SSO, I route to `hal vault oidc enable`.

## Example 2: Vault Base Layer Is Down

User: Configure Vault Kubernetes auth.

Assistant: Vault is down, so I recover the base first.

    hal vault create
    hal vault k8s enable

## Example 3: Clean Rebuild Sequence

User: I want to rebuild Vault and then re-enable LDAP.

Assistant:

    hal vault delete
    hal vault create
    hal vault ldap enable

## Example 4: Enterprise Feature Request

User: I want CSI projection for VSO and Sentinel policy examples.

Assistant: Those are Enterprise-only in this lab, so I switch Vault to Enterprise first.

    export VAULT_LICENSE='your_license_string'
    hal vault create --edition ent --update
    hal vault k8s enable --csi

## Example 5: Observability Backfill

User: Vault was already deployed. I now want metrics and dashboards in obs.

Assistant:

    hal obs create
    hal vault obs create

Assistant: Then verify surfaces:

    hal obs status
    hal vault status

## Example 6: PKI — Two-Tier CA with cert-manager K8s Demo

User: Set up a Vault PKI CA chain and show cert-manager issuing a cert in Kubernetes.

A:

    hal vault pki enable --k8s

A: Access the nginx web page (cert info rendered by the pod itself):

    https://pki.localhost:8089

## Example 7: PKI — Vault ACME + Caddy Live Auto-Renewal Demo

User: I want to see Caddy auto-renewing a cert every few minutes via Vault ACME.

A:

    hal vault pki enable --acme --acme-cert-ttl 5m

A: Access the live countdown page:

    https://acme.localhost:8090

A: Change TTL on a running demo (restarts Caddy, forces fresh ACME exchange):

    hal vault pki update --acme --acme-cert-ttl 2m
