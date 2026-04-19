# Example Transcripts

## Example 1: Enable SSH Target Lab

User: Create a Linux VM target for Boundary SSH.

Assistant:

    hal boundary ssh enable

Assistant: I verify VM and Boundary target wiring, then provide login test steps.

## Example 2: Repair Broken SSH Target

User: The VM exists but session auth fails.

Assistant:

    hal boundary ssh --force

Assistant: I confirm both VM and Boundary resources were reset and recreated.

## Example 3: Remove SSH Target Lab

User: Disable this SSH lab.

Assistant:

    hal boundary ssh disable
