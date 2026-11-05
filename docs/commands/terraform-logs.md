# HAL Terraform Logs Command Spec

## Command
- `hal terraform logs [--tail N] [--component NAME]` (alias `hal tf`, `hal tfe`)

## Purpose
Print the most recent log lines of one of the Terraform Enterprise containers, primary or twin.

Read-only: the command never follows the stream, never starts, stops or changes anything, and is annotated `hal.mcp/readonly: "true"` (ADR 0003, decisions 3 and 8) so the MCP `hal_read` tool may run it. It replaces running `podman logs` by hand.

## Behavior
- Detects the container engine with the same probe as every other command (`docker info`, then `podman info`), so Docker and Podman both work.
- Reads the component's state with `<engine> inspect`, then runs `<engine> logs --tail N <container>`.
- Missing container: prints `<container> is not running: no container named <container> on <engine>. Check 'hal terraform status', or start it with 'hal terraform create'.` on stderr and exits `1`.
- Stopped container (exited, created, paused): its last logs are still printed, which is what you want after a crash. A note on stderr says it is not running; exit `0`.
- An engine failure that is not "no such container" (daemon unreachable, permission denied) is reported as that failure, never as "not running".
- Without `--component` the default container (`hal-tfe`) is read. `--component` takes the container name without its `hal-` prefix; the full container name is accepted too.
- Bounded output: `--tail` defaults to 100 lines and is capped at 1000 (a larger value is clamped, with a note on stderr; `0` or a negative value is an error). The printed text is also capped at 256 KiB, keeping the most recent part and starting on a line boundary.
- Log lines go to stdout and nothing else does. Every note and error goes to stderr, so a pipe sees only log lines. Container applications usually log to stderr; both streams are merged into stdout.
- Exit code: `0` when logs were printed (or the component has no output yet), `1` for every failure.

## Related
- Parent namespace: [terraform.md](terraform.md)
- Status: `hal terraform status`

## Prerequisites
- HAL CLI is available in your local environment.
- Docker or Podman is running, and the component has been created at least once.

## Components
| `--component` | Container | What it is |
|---|---|---|
| `tfe` | `hal-tfe` | TFE primary (default) |
| `tfe-db` | `hal-tfe-db` | shared PostgreSQL |
| `tfe-redis` | `hal-tfe-redis` | shared Redis |
| `tfe-s3` | `hal-tfe-s3` | shared object storage |
| `tfe-proxy` | `hal-tfe-proxy` | shared ingress proxy |
| `tfe-agent` | `hal-tfe-agent` | agent, primary |
| `tfe-api` | `hal-tfe-api` | api-workflow helper, primary |
| `tfe-bis` | `hal-tfe-bis` | TFE twin |
| `tfe-bis-agent` | `hal-tfe-bis-agent` | agent, twin |
| `tfe-bis-api` | `hal-tfe-bis-api` | api-workflow helper, twin |

The twin is selected with `--component tfe-bis` (and its `-agent` / `-api` helpers), not with `--target`: the database, Redis, S3 and proxy containers serve both instances, so a per-target view of them would be the same container. `hal-gitlab` and Authentik are shared with Vault and are not listed.

## Flags
- Command flags from `hal terraform logs --help`:
```text
    --component string   Which container to read (default "tfe")
-h, --help               help for logs
    --tail int           Number of most recent log lines to print (max 1000) (default 100)
```
- Global flags: `--debug`, `--dry-run`, `--verbose`

## Side Effects
- None. The command is read-only. It runs `inspect` and `logs` on the engine and writes nothing.

## Example
```bash
hal terraform logs
hal terraform logs --tail 20
hal terraform logs --component tfe-db --tail 50
```
