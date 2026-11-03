# Example Transcripts

## Example 1: Enable Dynamic DB Credentials

User: Configure database secrets with Vault.

Assistant:

    hal vault database enable --backend mariadb

Assistant: I verify database connection and role:

    vault read database/config/hal-vault-mariadb
    vault read database/roles/dba-role

## Example 2: Credential Flow Validation

User: Show me JIT credentials in action.

Assistant:

    vault read database/creds/dba-role

Assistant: I explain lease TTL and revocation behavior.

## Example 3: Rebuild DB Secrets Lab

User: Reset the Vault database secrets lab.

Assistant:

    hal vault database update

## Example 4: KinD + VSO Live Rotation

User: Show database credentials rotating inside Kubernetes.

Assistant:

    hal vault database enable --k8s

Assistant: The demo is at `http://db.localhost:8091`. Auth lives on `kubernetes-db/`, not `kubernetes/`. The KinD node is attached to `hal-net` so VSO can reach Vault.

## Example 5: Database Lab Next To The Agentic IAM Lab

User: I disabled the database lab, but `hal-vault-mariadb` is still running.

Assistant: `hal-vault-mariadb` is shared and counted per consumer. `disable` removed `database/` and the `vaultadmin` broker, and kept the container because `hal vault agentic-iam` still uses it. It goes when its last consumer disables:

    hal vault agentic-iam
    hal vault agentic-iam disable
