# Example Transcripts

## Example 1: Deploy The Lab

User: Show me an AI agent getting Vault secrets on behalf of a user.

Assistant: That is the Agentic IAM lab. It needs Vault Enterprise 2.1.0+ with a license that includes the Agentic IAM terms. I preview it first: the dry run checks the prerequisites for real and changes nothing.

    hal vault agentic-iam enable --dry-run
    hal vault agentic-iam enable

Assistant: Open `http://agentic.localhost:8092` and log in as `alice` / `password`. The six cases of the scenario are offered as suggestions in the chat.

## Example 2: Refused On A CE Vault

User: `hal vault agentic-iam enable` says my Vault is not Enterprise.

Assistant: The lab needs Vault Enterprise 2.1.0 or later. The refusal changed nothing, so I redeploy Vault Enterprise and enable again:

    export VAULT_LICENSE_PATH=/path/to/vault.hclic
    hal vault update --edition ent --vault-tag 2.1.2-ent
    hal vault agentic-iam enable

## Example 3: Licensed Enterprise, Still Refused

User: I run Vault 2.1.2+ent with a valid license, and enable says "Feature Not Enabled".

Assistant: The Agentic IAM terms were added to the license model in Vault 2.1.0, so a valid 2.1.x license can lack them. Vault then answers HTTP 401 `Feature Not Enabled` on `sys/config/oauth-resource-server`. Only a license with those terms fixes it. Point `VAULT_LICENSE_PATH` at it and unset `VAULT_LICENSE`, which takes precedence:

    unset VAULT_LICENSE
    export VAULT_LICENSE_PATH=/path/to/agentic-iam.hclic
    hal vault update --edition ent --vault-tag 2.1.2-ent
    hal vault agentic-iam enable

## Example 4: Authentik Is Too Old

User: Enable refuses because Authentik is older than 2026.8.0.

Assistant: Authentik is shared, and HAL never restarts it under another lab. The refusal lists the labs that use it. I disable them, enable the lab, which starts Authentik at its default tag, then re-enable them:

    hal vault oidc disable
    hal vault agentic-iam enable
    hal vault oidc enable

## Example 5: Why Did The Agent Not Get The Forecasts?

User: alice asked for the Q2 results. She got them, but the forecasts were refused.

Assistant: That is case 5, decided by the task scope. The Q2 row carries a planted prompt injection that asks for the forecasts. alice consented only to `vault:finance-reports`, so the OBO token's `authorization_details` does not name the forecasts path, and Vault answers `RAR_NO_MATCH`. The transparency panel shows `decided_by: task_scope`. Asking for "Q3 results and forecasts" (case 6) consents to both, and both are allowed.

## Example 6: Tell The Persona's Rights From The Ceiling

User: bob and "alice asks for payroll" both just say permission denied. Which decision point refused?

Assistant: Vault names only the task scope in its response. The audit log tells the others apart, so I enable it and replay the cases:

    hal vault audit enable
    docker exec -it hal-vault tail -n 20 /vault/logs/audit.log

Assistant: Every lab request has the persona as `auth.entity_id` and `finance-agent` as `auth.metadata.actor_entity_name`. For bob, `identity_policies` is empty: his own rights refused. For alice and payroll, her policy grants the path and the RAR names it, yet `allowed` is false: the ceiling `agentic-iam-finance-agent-ceiling` refused.

## Example 7: The Lab Next To The Database Lab

User: I also run `hal vault database`. Will disabling the agentic lab break it?

Assistant: No. `hal-vault-mariadb` is shared and counted per consumer. The lab's `disable` drops its own `agentic-db/` mount, `acme` schema and `agentic-iam-broker` user, and keeps the container while `vault-database` uses it:

    hal vault agentic-iam disable
    vault read database/creds/dba-role

## Example 8: After Upgrading hal

User: The status says the containers run an older image.

Assistant: The image tag is a hash of the Python sources embedded in `hal`, so a new `hal` can bring a new image. `update` builds it, recreates both containers, and removes the images of earlier sources:

    hal vault agentic-iam update
