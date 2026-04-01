# Example Transcripts

## Example 1: Controlled Consul Teardown

User: Tear down Consul before switching lab scenarios.

Assistant:

    hal consul destroy
    hal consul status

Assistant: I confirm that Consul processes and related runtime state are gone before declaring teardown complete.

## Example 2: Forced Cleanup After Broken Cluster

User: Consul is stuck in a bad state, clean it up fully.

Assistant:

    hal consul destroy
    hal consul status

Assistant: If status still shows leftover artifacts, I recommend running destroy once more and rechecking status to ensure full cleanup.

## Example 3: Destroy Then Rebuild Safely

User: Remove Consul and bring it back cleanly.

Assistant:

    hal consul destroy
    hal consul deploy
    hal consul status

Assistant: I verify the rebuilt environment is healthy before moving on to downstream integrations.
