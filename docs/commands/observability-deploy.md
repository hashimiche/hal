# HAL Observability Deploy Command Spec

## Command
- `hal obs create`

## Purpose
Deploy Prometheus, Loki, Grafana, and Promtail stack components.

## Related
- Parent namespace: [observability.md](observability.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal obs create --help`:
```text
-f, --force                     Force a clean redeployment
--grafana-version string    Tag for the grafana/grafana image (default "main")
-h, --help                      help for deploy
--loki-version string       Tag for the grafana/loki image (default "3.7")
--prom-version string       Tag for the prom/prometheus image (default "main")
--promtail-version string   Tag for the grafana/promtail image (default "3.6")
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal obs create
```
