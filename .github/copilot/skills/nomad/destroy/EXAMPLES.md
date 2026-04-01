# Example Transcripts

## Example 1: Planned Nomad Teardown

User: Destroy Nomad resources before switching labs.

Assistant:

    hal nomad destroy
    hal nomad status

Assistant: I verify that scheduler components are no longer active.

## Example 2: Cleanup From Corrupted Runtime State

User: Nomad is wedged and jobs cannot recover.

Assistant:

    hal nomad destroy
    hal nomad status

Assistant: If status still indicates residual state, I suggest a second destroy pass followed by status to confirm a clean slate.

## Example 3: Safe Rebuild After Destroy

User: Tear down and restore Nomad cleanly.

Assistant:

    hal nomad destroy
    hal nomad deploy
    hal nomad status

Assistant: I confirm the rebuilt control plane is healthy before any workload actions.
