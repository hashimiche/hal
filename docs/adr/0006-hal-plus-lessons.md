# 6. HAL Plus answers with a lesson grounded on a topic

- **Status:** Accepted on 2026-10-26.
- **Branch for this record:** `docs/adr-hal-plus`
- **Numbering:** 0003 is the host MCP server ADR, `docs/adr/0003-host-mcp-server-exposing-hal.md`.
- **Working notes:** decided in a design interview on 2026-10-26. Terms are
  defined in `CONTEXT.md` under "HAL Plus".

## Context

HAL Plus is the chat for a lab. It was built around a small local model: Ollama
on the host, a Qdrant index, intent routes, and long behavior packs in the
`hal-plus` repo. The same knowledge was also written as Copilot skills under
`internal/skills`, embedded in the `hal` binary and symlinked from
`.github/copilot/skills`.

On a 48 GB machine the local model is too slow to be the way someone learns a
mechanism, and it blocks anyone who might actually run the chat. The packs and
the skills say the same thing twice, and the skill path only exists so Copilot
can find it. Copilot is no longer the editor in use.

What the chat is for is a lesson: what a mechanism is, how this lab stages it,
which `hal` commands show it, and the documents to read. A hosted frontier model
can write that prose. It cannot know the lab's commands, names, or the document
URLs we are willing to cite. Those stay in a topic we maintain.

## Decision

HAL Plus stays a container and talks to the host `hal mcp serve` over HTTP, as
ADR 0003 sets out. The model is a hosted one. `hal plus create --provider
anthropic|openai|xai|google` reads `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`,
`XAI_API_KEY`, or `GEMINI_API_KEY`. A missing key stops `create` before any
container. The key is passed into the container environment and is never written
under `~/.hal` or printed. There is no default provider. One model is pinned per
provider, chosen from that provider's docs at implementation time. Changing
provider means running `create` again.

Ollama and Qdrant are not part of the boot. The behavior packs and the Copilot
skill tree go away once their text has moved.

A topic is `docs/topics/<product>/<feature>.md`. The product's own topic is
`docs/topics/<product>/index.md`, because a file cannot sit beside a directory
of the same name. Topics are English. The lesson is in the language of the
question. The header is the only part the code parses:

```markdown
summary: GitLab CI logs into Vault with a JWT bound to a tag.
read: hal vault jwt | hal vault status | hal creds status
actions: hal vault jwt enable | hal vault jwt update | hal vault jwt disable
docs:
- Vault JWT auth: https://developer.hashicorp.com/vault/docs/auth/jwt#bound-claims
- GitLab CI JWT: https://docs.gitlab.com/ci/secrets/id_token_authentication/#token-payload
```

`summary` is the catalog line. `read` is one or more commands HAL Plus runs
before the lesson is written. `actions` are the commands that change the lab.
`docs` is two or three titles, URLs, and anchors. The prose under the header
says what the mechanism is and how the lab stages it. Operator logins are not
written in the file. A topic that needs one lists `hal creds status` under `read`.
`hal creds` alone only prints its help. When no service is running, `hal creds
status` answers "No active lab services detected", and the lesson then says to
start the service first.

The same files are embedded in the `hal` binary. The host MCP server exposes
each topic as a resource. A coding agent reads `docs/topics` in a checkout.
Homebrew and the container get the embedded copy.

Two model calls, and the model never chooses a tool. The first receives the
catalog (id and `summary`) and names one primary topic, and at most one
secondary. The code loads them. It runs every `read` of the primary and
downloads each primary document section at its anchor. The secondary contributes
its prose and its URLs only. The second call returns four fields: mechanism,
configuration, commands, documents. HAL Plus drops any command that is not in
the file and any URL that is not in the header, then the UI renders the lesson.
If an anchor cannot be fetched, the lesson still runs, the URL stays, and one
line says the excerpt could not be read.

A command from `actions` runs only after one dialog that shows the argv. There
is no "always allow". HAL Plus implements MCP elicitation, and that dialog is
the server's own question: over HTTP, ADR 0003 has the server ask for every
`hal_run`, with the exact argv. The question waits 5 minutes, then the server
cancels it and nothing runs. The server is the only guard, so a bug in HAL Plus
cannot run a command nobody answered for. The model never sees a
`confirmed: true`. The two-step fallback is not used, because HAL Plus always
declares elicitation. `hal delete`, `hal plus delete`, `hal mcp delete` and `hal
daisy` are never run, even if a topic lists them: they are the commands ADR 0003
annotates `ends-http-session`. The lesson prints the command, with no button, and
warns that the current chat will be cut and the session lost. That refusal lives
in HAL Plus, as a hard-coded list on top of the annotation. Other MCP clients keep
ADR 0003's rule.

Execution has no time limit set by HAL Plus. The server sends progress to keep the
connection open. If the daemon restarts during a run, the UI says it was
interrupted and to check the status, and the client initializes again with the
same token.

When no topic matches, the lesson still explains, and the command field says
that HAL has no command for it.

LangChain, Pydantic, GEPA, Waza, and a tool-calling loop are not part of this
runtime. The topic, the header lists, and the dialog are the control.

## Consequences

Lab secrets printed by a `read` can reach the model provider. They are
disposable. A product license is never shown, and never stored.

The HAL Plus container needs a network path to the document hosts. A dead anchor
is fixed in the topic file.

The knowledge lives in `docs/topics`, served as MCP resources from the embedded
files (ADR 0003, decision 3). The cobra tree is the command catalog. `SKILL.md`
files no longer play either role. The `hal-plus` behavior packs are removed in the
same work as the topic move, or the two copies diverge again.

HAL Plus reads the lab state through `hal_read ["status","--json"]`. The
`hal-health` sidecar, `hal health` and the `hal-mcp` image are removed (ADR 0003,
phase 4).

`internal/skills` and `.github/copilot/skills` are deleted after the move, and
the doc-sync rules that name them point at `docs/topics` instead.

## Alternatives considered

- **Keep Ollama.** It was tried on a 48 GB machine. The chat is too slow, and a
  local model is a requirement most people will not meet.
- **Keep Qdrant.** The vectors were built so a small model would hit the right
  page. A hosted model can work from a short catalog. A provider embedding would
  not match the existing index without rebuilding it for one vendor.
- **Keep Copilot skills as the source.** The discovery path is vendor-specific,
  and the current files are runbooks (root token, raw `vault read`) rather than
  the lesson frame. Sending them unchanged would teach the model to print
  `VAULT_TOKEN=root`.
- **A second corpus beside the topics.** That is the duplication this ADR
  closes.
- **Let the model call `hal_read` and fetch documents itself.** The topic
  already names the command and the anchor. A loop is a place for a command to
  leave the list.
- **Paste document excerpts into the topic.** The URL and the anchor are the
  maintained part. The page is fetched at question time.
- **Markdown straight from the model, scraped for stray commands.** A command
  hidden in a sentence gets through. Four fields can be checked.
- **LangChain, Pydantic, GEPA, or Waza.** One HTTP call and a checked object do
  not need an orchestration framework, a Python type layer, a prompt evolver, or
  an evaluator tied to the Copilot SDK.
