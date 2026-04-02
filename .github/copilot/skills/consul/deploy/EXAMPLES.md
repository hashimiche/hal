# Example Transcripts

## Example 1: Fresh Consul Bring-Up

User: Deploy Consul in this lab and confirm it is healthy.

Assistant:

    hal consul deploy
    hal consul status

Assistant: I summarize whether the server and UI endpoint are reachable, then provide the next command to continue with dependent services.
Assistant: If obs is already deployed, I also mention that Consul target registration and dashboard import are automatic.

## Example 4: Backfill Consul Monitoring Without Redeploy

User: Consul is already up. I only want monitoring artifacts now.

Assistant:

    hal consul deploy --configure-obs

## Example 2: Drift Recovery After Host Restart

User: Consul was working yesterday, now service discovery looks inconsistent.

Assistant:

    hal consul status
    hal consul deploy
    hal consul status

Assistant: I compare before/after status and call out whether the second deploy fully reconciled the environment.

## Example 3: Hard Reset Recommendation

User: Deploy keeps reporting unhealthy state.

Assistant:

    hal consul status

Assistant: Because status remains degraded, I recommend a clean reset path:

    hal consul destroy
    hal consul deploy
    hal consul status

Assistant: I explain that this removes stale runtime artifacts before recreating the cluster.
