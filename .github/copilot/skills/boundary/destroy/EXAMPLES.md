# Example Transcripts

## Example 1: Full Boundary Teardown

User: Destroy all Boundary resources.

Assistant:

    hal boundary destroy

Assistant: I verify control plane and targets are gone.

## Example 2: Cleanup After Failed Deploy

User: Boundary is broken; wipe everything.

Assistant:

    hal boundary destroy
    hal boundary status

Assistant: I confirm cleanup completeness.

## Example 3: Rebuild Sequence

User: Bring it back after destroy.

Assistant:

    hal boundary deploy
    hal boundary ssh --enable
