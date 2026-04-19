# Example Transcripts

## Example 1: Enable Standalone MariaDB Target

User: Enable Boundary access to a MariaDB target.

Assistant:

    hal boundary mariadb enable

Assistant: I verify the target is created and then guide login and connection.

    BOUNDARY_AUTHENTICATE_PASSWORD_PASSWORD=password boundary authenticate password -addr http://127.0.0.1:9200 -auth-method-id <lab-auth-id> -login-name dba-user
    boundary connect -addr http://127.0.0.1:9200 -target-id <target-id> -listen-port 13306

Assistant: In another terminal, connect with static lab creds.

    mariadb -h 127.0.0.1 -P 13306 -u admin -ppassword targetdb

## Example 2: Enable With Vault Dynamic Credentials

User: Rebuild MariaDB and wire Boundary to Vault dynamic creds.

Assistant:

    hal boundary mariadb enable --with-vault --update

Assistant: I confirm the target has brokered credential sources attached, then use the DB-aware connect flow.

    BOUNDARY_AUTHENTICATE_PASSWORD_PASSWORD=password boundary authenticate password -addr http://127.0.0.1:9200 -auth-method-id <lab-auth-id> -login-name dba-user
    boundary connect mysql -addr http://127.0.0.1:9200 -target-id <target-id>

Assistant: If you want to inspect raw Vault credentials independently:

    vault read database/creds/dba-role

## Example 3: Recover Broken Target Wiring

User: MariaDB runs but target auth fails.

Assistant:

    hal boundary mariadb --update

Assistant: I re-check current IDs and command shape before retrying connect.

    BOUNDARY_AUTHENTICATE_PASSWORD_PASSWORD=password boundary authenticate password -addr http://127.0.0.1:9200 -auth-method-id <lab-auth-id> -login-name dba-user
    boundary targets list -recursive
    boundary connect -addr http://127.0.0.1:9200 -target-id <fresh-target-id>

## Example 4: Disable DB Target

User: Remove this DB target lab.

Assistant:

    hal boundary mariadb disable

Assistant: I verify container and Boundary resources are removed cleanly.
