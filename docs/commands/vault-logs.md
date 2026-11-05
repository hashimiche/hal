# HAL Vault Logs Command Spec

## Command
- `hal vault logs [--tail N] [--component NAME]`

## Purpose
Print the most recent log lines of one of the containers `hal vault delete` owns.

Read-only: the command never follows the stream, never starts, stops or changes anything, and is annotated `hal.mcp/readonly: "true"` (ADR 0003, decisions 3 and 8) so the MCP `hal_read` tool may run it. It replaces running `podman logs` by hand.

## Behavior
- Detects the container engine with the same probe as every other command (`docker info`, then `podman info`), so Docker and Podman both work.
- Reads the component's state with `<engine> inspect`, then runs `<engine> logs --tail N <container>`.
- Missing container: prints `<container> is not running: no container named <container> on <engine>. Check 'hal vault status', or start it with 'hal vault create'.` on stderr and exits `1`.
- Stopped container (exited, created, paused): its last logs are still printed, which is what you want after a crash. A note on stderr says it is not running; exit `0`.
- An engine failure that is not "no such container" (daemon unreachable, permission denied) is reported as that failure, never as "not running".
- Without `--component` the default container (`hal-vault`) is read. `--component` takes the container name without its `hal-` prefix; the full container name is accepted too.
- Bounded output: `--tail` defaults to 100 lines and is capped at 1000 (a larger value is clamped, with a note on stderr; `0` or a negative value is an error). The printed text is also capped at 256 KiB, keeping the most recent part and starting on a line boundary.
- Log lines go to stdout and nothing else does. Every note and error goes to stderr, so a pipe sees only log lines. Container applications usually log to stderr; both streams are merged into stdout.
- Exit code: `0` when logs were printed (or the component has no output yet), `1` for every failure.

## Related
- Parent namespace: [vault.md](vault.md)
- Status: `hal vault status`

## Prerequisites
- HAL CLI is available in your local environment.
- Docker or Podman is running, and the component has been created at least once.

## Components
| `--component` | Container | What it is |
|---|---|---|
| `vault` | `hal-vault` | Vault server (default) |
| `openldap` | `hal-openldap` | `hal vault ldap` |
| `phpldapadmin` | `hal-phpldapadmin` | `hal vault ldap` UI |
| `vault-mariadb` | `hal-vault-mariadb` | `hal vault database`, shared with `agentic-iam` |
| `vault-oracle-db` | `hal-vault-oracle-db` | `hal vault database` (Oracle) |
| `agentic-iam-chat` | `hal-agentic-iam-chat` | `hal vault agentic-iam` |
| `agentic-iam-agent` | `hal-agentic-iam-agent` | `hal vault agentic-iam` |

`hal-gitlab` and the Authentik stack are not listed: they are shared singletons that Terraform also uses, so neither product owns them.

## Flags
- Command flags from `hal vault logs --help`:
```text
    --component string   Which container to read (default "vault")
-h, --help               help for logs
    --tail int           Number of most recent log lines to print (max 1000) (default 100)
```
- Global flags: `--debug`, `--dry-run`, `--verbose`

## Side Effects
- None. The command is read-only. It runs `inspect` and `logs` on the engine and writes nothing.

## Example
```bash
hal vault logs
hal vault logs --tail 20
hal vault logs --component openldap --tail 50
```
