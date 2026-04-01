# Example Transcripts

## Example 1: Run Setup Helper

User: Execute Boundary setup for data-plane resources.

Assistant:

    hal boundary setup

Assistant: I summarize what setup configured and what still needs manual action.

## Example 2: Setup After Deploy

User: Boundary is deployed. What should I run now?

Assistant:

    hal boundary setup
    hal boundary status

Assistant: I verify readiness and suggest target enable commands.

## Example 3: Setup + Target Sequence

User: Run setup and then enable MariaDB target.

Assistant:

    hal boundary setup
    hal boundary mariadb --enable
