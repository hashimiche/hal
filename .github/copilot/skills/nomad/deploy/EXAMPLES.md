# Example Transcripts

## Example 1: Nomad Control Plane Bring-Up

User: Deploy Nomad and confirm scheduler health.

Assistant:

    hal nomad deploy
    hal nomad status

Assistant: I report whether servers and clients are healthy before any workload submission.
Assistant: If `hal obs deploy` is already up, I also mention Nomad target and dashboard registration are automatic.

## Example 2: Partial Recovery After Unstable Allocations

User: Nomad deploy succeeded, but jobs are flapping.

Assistant:

    hal nomad status
    hal nomad deploy
    hal nomad status

Assistant: I use the before/after status to decide if reconciliation worked or if a full reset is safer.

## Example 3: Deploy and Immediately Validate With a Job

User: Bring Nomad online and prove it can schedule workloads.

Assistant:

    hal nomad deploy
    hal nomad job
    hal nomad status

Assistant: I confirm the control plane is healthy and that at least one sample workload reaches a stable allocation state.
