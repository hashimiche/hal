# Example Transcripts

## Example 1: Initial Terraform Lab Provisioning

User: Deploy Terraform lab resources and confirm readiness.

Assistant:

    hal terraform create
    hal terraform workspace enable
    hal terraform status

Assistant: I summarize provisioning success and any post-deploy steps required by this lab.
Assistant: If obs is already running, I also call out automatic Prometheus target and Grafana HAL-folder dashboard import.

## Example 2: Recover From Stale or Drifted State

User: Deploy failed because the environment looks out of sync.

Assistant:

    hal terraform status
    hal terraform create
    hal terraform status

Assistant: I use the second status check to confirm whether reconciliation fixed drift.

## Example 3: Provision Then Configure Workspace Automation

User: Stand up Terraform lab and wire end-to-end VCS automation.

Assistant:

    hal terraform create
    hal terraform workspace enable
    hal terraform status

Assistant: I verify both infrastructure availability and workspace automation readiness in one sequence.

## Example 4: Podman Rootless Redirect Safety

User: I use Podman rootless and TFE health works but /app keeps looping.

Assistant:

    hal terraform create -f
    curl -k -I https://tfe.localhost:8443/_health_check
    curl -k -I https://tfe.localhost:8443/app

Assistant: HAL deploy includes a local proxy layer that rewrites TFE canonical redirects for `:8443`, so `/app` should stop self-looping without changing host sysctl settings.

## Example 5: Backfill TFE Monitoring Without Redeploy

User: TFE is already deployed. I added obs after the fact and only want monitoring wired.

Assistant:

    hal terraform create --configure-obs

Assistant: This refreshes only the Prometheus target and Grafana dashboard artifacts without redeploying TFE.

## Example 6: Remote Run Fails Early

User: The run triggers, but plan/apply errors immediately.

Assistant:

    hal terraform create -f
    hal terraform workspace enable

Assistant: Deploy now patches the task-worker cache mount for writable Terraform downloads and preserves `:8443` archivist log links for UI/raw log access.
