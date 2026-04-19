# Example Transcripts

## Example 1: Build Then Enter

User: I need a Terraform shell that already trusts the local TFE cert.

Assistant:

    hal tf api-workflow enable

Assistant: This builds the helper image, bootstraps auth in the container, ensures the default scenario workspaces in TFE (`Dave`/`Frank` mapping), and opens the shell without requiring host trust-store changes.

## Example 2: Check Current State

User: Is the Terraform helper already running?

Assistant:

    hal terraform api-workflow

Assistant: Use the status view first. It shows whether the helper image exists, whether the target helper container is running, and whether the target TFE core is up.

## Example 2b: Fresh Reset Expectations

User: After `hal tf create`, `hal tf api-workflow enable`, what should I see in TFE?

Assistant:

    Projects: Dave, Frank
    Workspaces: hal-lucinated, hal-lelujah, hal-ibut (Dave)
                hal-ogen, hal-oween (Frank)

Assistant: By default, `HAL-CLI` is no longer auto-created unless you pass `--tfe-project`.

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

    hal tf api-workflow disable --auto-approve
    hal tf api-workflow update

## Example 5: Redirect From Host Terraform Login

User: `terraform login` on my Mac fails because of the self-signed cert. What should I do?

Assistant:

    hal tf api-workflow enable

Assistant: Use the HAL helper shell instead of changing host trust. The helper already trusts local TFE and has Terraform and TFX auth preconfigured.