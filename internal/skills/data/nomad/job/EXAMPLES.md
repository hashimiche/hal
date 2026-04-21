# Example Transcripts

## Example 1: Baseline Job Submission

User: Launch the sample Nomad job and confirm it stays healthy.

Assistant:

    hal nomad job
    hal nomad status

Assistant: I report allocation and scheduler health together so the user can distinguish app failure from control-plane failure.

## Example 2: Job Fails Right After Submission

User: Job command returned errors.

Assistant:

    hal nomad status
    hal nomad job
    hal nomad status

Assistant: I compare status before and after retry to identify whether recovery happened or a deeper redeploy is needed.

## Example 3: Full Recovery Path For Persistent Job Failures

User: This workload keeps failing, reset everything.

Assistant:

    hal nomad delete
    hal nomad create
    hal nomad job
    hal nomad status

Assistant: I confirm the environment is rebuilt and the workload can schedule successfully.
