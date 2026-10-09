# Agentic IAM lab: the chat and the demo agent

The Python half of `hal vault agentic-iam` ([ADR 0004](../../../docs/adr/0004-vault-agentic-iam-lab.md)).
One image runs as two containers on `hal-net`:

| Command | Container role | Default port | Published on the host |
|---|---|---|---|
| `chat` | the web page and its BFF | `8080` | yes, the persona's browser opens it |
| `agent` | the demo agent's internal API | `8081` | **no**, only the chat calls it |

HAL embeds `Dockerfile`, `requirements.txt` and `agentic_iam/` in its binary
(`internal/agenticiam`) and builds `localhost/hal-agentic-iam:<hash of those files>`
at the first `enable`, and removes the images of earlier sources once the
containers run the new one. This README, `requirements.in` and `tests/` are not
embedded: editing them does not change the image.

```sh
podman run -d --name <chat>  --network hal-net -p <host port>:8080 -e ... localhost/hal-agentic-iam:<hash> chat
podman run -d --name <agent> --network hal-net                     -e ... localhost/hal-agentic-iam:<hash> agent
```

`hal vault agentic-iam enable` runs them as `hal-agentic-iam-chat` (published
on host port 8092, `PUBLIC_URL=http://agentic.localhost:8092`) and
`hal-agentic-iam-agent`. It passes the variables in `--env-file`s with mode
`0600` under `~/.hal/agentic-iam/`, one per container, never on the command
line (`cmd/vault/agentic_iam_containers.go`).

## Configuration: environment variables only

A missing required variable stops the container at start, with all missing
names in one message (exit code 2).

### `chat`

| Variable | Required | Default | Meaning |
|---|---|---|---|
| `LISTEN_PORT` | | `8080` | Port inside the container. |
| `PUBLIC_URL` | yes | | The URL the browser uses, e.g. `http://agentic.localhost:<host port>`. `PUBLIC_URL/callback` must be a redirect URI of `hal-chat`, and `PUBLIC_URL/` its post-logout redirect URI. |
| `OIDC_ISSUER` | | `http://authentik.localhost:9100/application/o/hal-chat/` | Endpoints are discovered from `<issuer>/.well-known/openid-configuration`. |
| `OIDC_CLIENT_ID` | yes | | `hal-chat` client ID. |
| `OIDC_CLIENT_SECRET` | yes | | `hal-chat` client secret (confidential client). |
| `AGENT_URL` | yes | | The demo agent on `hal-net`, e.g. `http://<agent>:8081`. |

### `agent`

| Variable | Required | Default | Meaning |
|---|---|---|---|
| `LISTEN_PORT` | | `8081` | Port inside the container. |
| `TOKEN_ENDPOINT` | | `http://authentik.localhost:9100/application/o/token/` | Authentik's token endpoint. |
| `EXCHANGE_CLIENT_ID` | yes | | `vault-agentic` client ID: the token exchange. |
| `EXCHANGE_CLIENT_SECRET` | yes | | `vault-agentic` client secret. |
| `ACTOR_CLIENT_ID` | yes | | `hal-demo-agent` client ID: the demo agent's own login. |
| `ACTOR_CLIENT_SECRET` | yes | | `hal-demo-agent` client secret. |
| `ACTOR_USERNAME` | | `finance-agent` | The Authentik Actor of the demo agent. |
| `ACTOR_APP_PASSWORD` | yes | | That Actor's app password. |
| `VAULT_ADDR` | | `http://hal-vault:8200` | `https://hal-vault:8200` for a prod Vault (`hal vault create --mode prod`). |
| `SSL_CERT_FILE` | | the system CA bundle | Python's standard variable. HAL sets it, for a prod Vault only, to HAL's self-signed Vault CA mounted read-only in the container. |
| `DB_MOUNT` | | `agentic-db` | The lab's database secrets engine mount. |
| `DB_ROLE_QUARTERLY_RESULTS` | | `quarterly-results` | Role for `SELECT` on `quarterly_results`. |
| `DB_ROLE_FORECASTS` | | `forecasts` | Role for `SELECT` on `forecasts`. |
| `DB_ROLE_PAYROLL` | | `payroll` | Role for `SELECT` on `payroll`. |
| `DB_HOST` | | `hal-vault-mariadb` | |
| `DB_PORT` | | `3306` | |
| `DB_NAME` | | `acme` | |

Every Authentik URL must use `authentik.localhost:9100`, inside `hal-net` too:
Authentik derives its issuer from the `Host` header.

## What the demo agent does

| Tool | Scope (task scope entry) | Vault path | Query |
|---|---|---|---|
| `get_quarterly_results(quarter)` | `vault:finance-reports` | `<DB_MOUNT>/creds/<DB_ROLE_QUARTERLY_RESULTS>` | `quarter, revenue_musd, operating_margin_pct, commentary` of one quarter |
| `get_forecasts()` | `vault:forecasts` | `<DB_MOUNT>/creds/<DB_ROLE_FORECASTS>` | `quarter, revenue_musd, confidence` |
| `get_payroll()` | `vault:payroll` | `<DB_MOUNT>/creds/<DB_ROLE_PAYROLL>` | `employee, department, annual_salary_kusd` |

- `POST /plan {prompt}` returns `{scopes, answer}`: the scopes of the tools the
  model calls first. Those calls wait for approval (PydanticAI's deferred
  tools), so no data is read. With no scope, `answer` is the model's reply and
  there is no task.
- `POST /run {prompt, subject_token, scopes}` performs **one** token exchange
  (`subject_token` = the persona's `hal-chat` access token, `actor_token` = the
  demo agent's `hal-demo-agent` access token, `scope=openid <scopes>`), then runs
  the tool loop. Each tool call reads its Vault path with the OBO JWT as
  `X-Vault-Token`, then queries MariaDB with the ephemeral user. It returns
  `{answer, trace}`:

```json
{
  "task_scope": ["vault:finance-reports"],
  "exchange": {"ok": true, "error": null, "decided_by": null},
  "obo": {"sub": "alice", "act": {"sub": "finance-agent"}, "aud": "<vault-agentic client ID>",
          "authorization_details": [{"type": "vault:path_access", "path": "agentic-db/creds/quarterly-results", "capabilities": ["read"]}]},
  "tool_calls": [{"tool": "get_quarterly_results", "args": {"quarter": "Q2"},
                  "vault_path": "agentic-db/creds/quarterly-results", "in_task_scope": true,
                  "decision": "allowed", "vault_error": null, "decided_by": null,
                  "db_user": "v-...", "lease_ttl": 300, "rows": 1, "db_error": null}]
}
```

`decision` is `allowed`, `refused` (HTTP 403) or `error`. `decided_by` is one of
`idp`, `persona`, `ceiling`, `task_scope`, or `null` when nobody says.

The trace and the answer never contain a token or a password, and neither does
anything the model sees: the OBO token stays in the run's dependency, which
the model never sees. The demo agent is a PydanticAI agent (`pydantic-ai-slim`)
and its model is a deterministic fake, a PydanticAI function model
(`agentic_iam/fake_model.py`). It falls for the injection planted in the Q2
commentary every time. `agentic_iam/model.py` is the one-line seam for a real
model.

## Open: `TODO(spike)`

`agentic_iam/vault.py`, `DECISION_MARKERS`: Vault's 403 text has not been seen
yet (ADR 0004, open question 5, blocked by the license). Only `RAR_NO_MATCH` →
`task_scope` is mapped, from the 2.1.x changelog. Until a marker matches,
`decided_by` stays `null` and the panel shows Vault's own words.

## Development

### Tests

Standard library `unittest`, with the IdP, Vault and MariaDB faked:

```sh
podman run --rm -e PYTHONDONTWRITEBYTECODE=1 -v "$PWD/tests:/app/tests:ro" \
  --entrypoint python localhost/hal-agentic-iam:<hash> -m unittest discover -s tests -v
```

### Regenerating the lock file

After changing `requirements.in`, in a throwaway container of the pinned base
image. The lock is universal: its hashes cover every platform.

```sh
podman run --rm -v "$PWD:/work" -w /work <base image of the Dockerfile> sh -c \
  "pip install --root-user-action=ignore uv==0.12.23 && uv pip compile --universal \
   --python-version 3.14 --generate-hashes \
   --custom-compile-command 'see README.md, Regenerating the lock file' \
   requirements.in -o requirements.txt"
```
