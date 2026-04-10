# Example Transcripts

## Example 1: Build Then Enter

User: I need a Terraform shell that already trusts the local TFE cert.

Assistant:

    hal tf cli -e
    hal tf cli -c

Assistant: This builds the helper image, bootstraps auth in the container, and opens the shell without requiring host trust-store changes.

## Example 2: Check Current State

User: Is the Terraform helper already running?

Assistant:

    hal terraform cli

Assistant: Use the status view first. It shows whether the helper image exists, whether `hal-tfe-cli` is running, and whether `hal-tfe` is up.

## Example 3: Verify Helper Tooling

User: I am inside the helper. What should I run to verify it is ready?

Assistant:

    terraform version
    tfx --help
    cat /root/.tfx.hcl
    cat /root/.terraform.d/credentials.tfrc.json
    ls /workspaces

## Example 4: Reset The Helper Cleanly

User: The helper container feels stale. Recreate it cleanly.

Assistant:

    hal tf cli --disable --force
    hal tf cli -e -f
    hal tf cli -c

## Example 5: Redirect From Host Terraform Login

User: `terraform login` on my Mac fails because of the self-signed cert. What should I do?

Assistant:

    hal tf cli -c

Assistant: Use the HAL helper shell instead of changing host trust. The helper already trusts local TFE and has Terraform and TFX auth preconfigured.