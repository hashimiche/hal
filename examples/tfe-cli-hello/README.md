# TFE CLI Hello

This demo shows local Terraform CLI commands with remote execution in Terraform Enterprise.

## What this proves

- You run Terraform from your local shell or HAL helper container.
- The run is executed remotely in TFE workspace `testmiche-cli`.
- You can see run history and outputs in TFE UI.

## Important hostname note

For this HAL setup, use `tfe.localhost:8443` in the Terraform `cloud` block.

Do not use `tfe.localhost` without port in this environment.

## Run it

```bash
terraform init
terraform plan
terraform apply -auto-approve
terraform output
```

## Verify remote execution

Open TFE and check workspace runs:

- https://tfe.localhost:8443
- organization: hal
- workspace: testmiche-cli

You should see run entries created by your local CLI actions.
