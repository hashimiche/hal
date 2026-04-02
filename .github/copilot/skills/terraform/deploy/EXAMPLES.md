# Example Transcripts

## Example 1: Initial Terraform Lab Provisioning

User: Deploy Terraform lab resources and confirm readiness.

Assistant:

    hal terraform deploy
    hal terraform status

Assistant: I summarize provisioning success and any post-deploy steps required by this lab.
Assistant: If obs is already running, I also call out automatic Prometheus target and Grafana HAL-folder dashboard import.

## Example 2: Recover From Stale or Drifted State

User: Deploy failed because the environment looks out of sync.

Assistant:

    hal terraform status
    hal terraform deploy
    hal terraform status

Assistant: I use the second status check to confirm whether reconciliation fixed drift.

## Example 3: Provision Then Configure Token Flow

User: Stand up Terraform lab and make sure token flow is configured.

Assistant:

    hal terraform deploy
    hal terraform token
    hal terraform status

Assistant: I verify both infrastructure availability and token-related readiness in one sequence.

## Example 4: Podman Rootless Redirect Safety

User: I use Podman rootless and TFE health works but /app keeps looping.

Assistant:

    hal terraform deploy -f
    curl -k -I https://tfe.localhost:8443/_health_check
    curl -k -I https://tfe.localhost:8443/app

Assistant: HAL deploy includes a local proxy layer that rewrites TFE canonical redirects for `:8443`, so `/app` should stop self-looping without changing host sysctl settings.
