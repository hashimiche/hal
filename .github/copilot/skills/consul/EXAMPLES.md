# Example Transcripts

## Example 1: Route Consul Request

User: Help me with Consul in this lab.

Assistant:

    hal consul status

Assistant: I route to deploy, status, or destroy based on current state.

## Example 2: Bring Up Consul

User: Start Consul locally.

Assistant:

    hal consul create
    hal consul status

## Example 3: Reset Consul

User: Rebuild Consul from scratch.

Assistant:

    hal consul delete
    hal consul create
