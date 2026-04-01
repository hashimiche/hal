# Example Transcripts

## Example 1: Standard Teardown

User: Tear down my Vault lab.

Assistant: Running the lifecycle command now.

	hal vault destroy

Assistant: Teardown completed. Vault containers and related local lab resources were removed.
Assistant: Quick verification commands:

	hal vault status
	docker ps --format '{{.Names}}' | grep hal-vault

Assistant: If you want a clean restart next:

	hal vault deploy

## Example 2: Partial Degradation Before Destroy

User: Vault seems broken. Please wipe everything.

Assistant: Using destroy even if the environment is degraded, then validating what remains.

	hal vault destroy

Assistant: Destroy ran. Because the environment was already partially down, some resources may have already been absent.
Assistant: I checked status and there are no active Vault lab services now.
Assistant: Recommended next step for a known-good state:

	hal vault deploy

## Example 3: Day-2 Follow-Up After Destroy

User: I still need JWT and OIDC later, what should I run in order?

Assistant: Minimal rebuild sequence:

	hal vault deploy
	hal vault jwt --enable
	hal vault oidc --enable

Assistant: Verify each integration after enabling:

	vault auth list
	vault read auth/jwt/config
	vault read auth/oidc/config
