# Example Transcripts

## Example 1: User Is Unsure Which Boundary Flow

User: Help me with Boundary setup.

Assistant: I start with platform state and route to the right sub-workflow.

    hal boundary status

Assistant: Then I route to deploy, ssh, mariadb, or teardown based on your goal.

## Example 2: User Wants MariaDB With Vault Cred Brokering

User: I need Boundary access to MariaDB with temporary Vault creds.

Assistant:

    hal boundary mariadb --enable --with-vault --force

Assistant: After deploy, I route the user to the DB-aware connect flow.

    BOUNDARY_AUTHENTICATE_PASSWORD_PASSWORD=password boundary authenticate password -addr http://127.0.0.1:9200 -auth-method-id <lab-auth-id> -login-name dba-user
    boundary connect mysql -addr http://127.0.0.1:9200 -target-id <target-id>

## Example 3: Control Plane Down

User: I want SSH target access but Boundary UI is down.

Assistant:

    hal boundary deploy
    hal boundary ssh --enable

## Example 4: Clean Rebuild

User: Reset all Boundary resources.

Assistant:

    hal boundary destroy
    hal boundary deploy
