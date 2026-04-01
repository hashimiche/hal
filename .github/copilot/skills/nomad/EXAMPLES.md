# Example Transcripts

## Example 1: Route Nomad Request

User: Help me set up Nomad in this lab.

Assistant:

    hal nomad status

Assistant: I route to `deploy`, `job`, `status`, or `destroy` based on state.

## Example 2: Bring Up Scheduler

User: Start Nomad now.

Assistant:

    hal nomad deploy
    hal nomad status

## Example 3: Clean Rebuild

User: Reset Nomad from scratch.

Assistant:

    hal nomad destroy
    hal nomad deploy
