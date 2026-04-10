# TFE CLI Scenario Bootstrap

This scenario creates five Terraform Enterprise workspaces and five matching repos inside the helper container at:

/workspaces/<repo>

It then seeds a mixed run history:

- hal-lucinated -> hal-lucinated: 5 apply runs, mushroom theme, project Dave
- hal-ogen -> hal-ogen: 3 plan-only runs, chemistry theme, project Frank
- hal-lelujah -> hal-lelujah: 2 apply + 1 plan, choir theme, project Dave
- hal-oween -> hal-oween: 1 plan + 1 apply, spooky theme, project Frank
- hal-ibut -> hal-ibut: no runs, fish theme, project Dave

The spread is intentionally split across the Terraform Enterprise projects `Dave` and `Frank`.

## Run From Host

1. Ensure the helper is ready:
   go run main.go tf cli -e --force
   go run main.go tf cli -c --banner

2. Copy and execute bootstrap script inside helper:
   docker cp examples/tfe-cli-scenario/bootstrap.sh hal-tfe-cli:/tmp/bootstrap.sh
   docker exec hal-tfe-cli sh -lc 'chmod +x /tmp/bootstrap.sh && /tmp/bootstrap.sh'

## Explore Inside Helper

Open a shell:

go run main.go tf cli -c

Then:

cd /workspaces/hal-lucinated
terraform plan
terraform apply -auto-approve

Repeat for other repos to generate additional runs.

## Teardown

To remove the helper container and delete HAL-managed scenario workspaces tracked by HAL:

```bash
go run main.go tf cli --disable
```

Use `--force` to skip the interactive confirmation prompt:

```bash
go run main.go tf cli --disable --force
```

Leaving the shell with `exit` or `CTRL+D` does not destroy the helper container. Re-enter with:

```bash
go run main.go tf cli -c
```
