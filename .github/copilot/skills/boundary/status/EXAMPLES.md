# Example Transcripts

## Example 1: Ecosystem Health Check

User: Is Boundary and its target ecosystem healthy?

Assistant:

    hal boundary status

Assistant: I summarize control plane status plus MariaDB and SSH target readiness.

## Example 2: Partial Failure Diagnosis

User: SSH is up but MariaDB target seems down.

Assistant:

    hal boundary status

Assistant: I route to the exact target reset command.

## Example 3: Next Step Routing

User: What should I run now?

Assistant: I recommend `deploy`, `ssh --enable`, `mariadb --enable`, or `destroy` based on output.
