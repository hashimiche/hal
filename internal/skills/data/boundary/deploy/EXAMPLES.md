# Example Transcripts

## Example 1: Start Boundary Control Plane

User: Deploy Boundary in this lab.

Assistant:

hal boundary create --version 0.15.2

Assistant: I verify core services and provide next target commands.

## Example 2: Recover From Drift

User: Boundary is half-working.

Assistant:

	hal boundary status
	hal boundary update

Assistant: I confirm clean control-plane recovery.

## Example 3: Deploy Then Add Target

User: Bring Boundary up and add SSH target.

Assistant:

	hal boundary create
	hal boundary ssh enable
