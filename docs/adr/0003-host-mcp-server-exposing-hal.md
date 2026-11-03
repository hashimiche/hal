# 3. The MCP server is the host `hal` binary, and it exposes `hal` itself

- **Status:** Accepted, 2026-11-03. Revised after the HAL Plus discussion, which is
  recorded in ADR 0006 (HAL Plus lessons). Where HAL Plus runs, the fate of the
  image and the client obligations are now settled; see
  [Open questions](#open-questions) for what remains.
- **Date:** 2026-10-02, revised 2026-11-03
- **Branch for implementation:** one branch per phase — see
  [Implementation plan](#implementation-plan)
- **Supersedes:** the containerised `hal-mcp` server and its 39-tool surface, as
  described in the MCP section of `LLM_CONTEXT.md`
- **Working notes:** none — this ADR is the output of a decision interview held on
  2026-10-02

---

## Context

`hal mcp` was introduced on 2026-04-05 (`ae62eba`, "first test with hal MCP") so
that any LLM could understand `hal` and suggest to the user, in a chat interface,
which commands to type. It grew into a hand-rolled JSON-RPC server with two
transports, 39 tools, a response contract (`HAL_MCP_CONTRACT.json`), a container
image (`ghcr.io/hashimiche/hal-mcp`) and an integration with HAL Plus.

What we actually want from it today is broader than the original goal:

| Need | Needs MCP? |
|---|---|
| **Catalog** — the LLM knows the `hal` commands, flags and semantics | No. A static context file would do. |
| **Observation** — the LLM sees the lab: what runs, health, endpoints, logs | Yes. It needs tool calls, and a chat UI has no shell. |
| **Action** — the LLM runs `hal` commands on the user's behalf | Yes, for the same reason. |

It is used by the maintainer and by learners, each on their own machine, single-user,
local only. Users must be able to switch **both** the model (local Ollama ↔ a cloud
model) **and** the client (the HAL Plus chat ↔ Claude Code, VS Code, Claude Desktop).
So the server must not depend on any particular client.

Measured against that, the current server fails on every axis:

1. **It is blind inside HAL Plus.** The `hal-mcp` container has no engine socket,
   by design (`LLM_CONTEXT.md:101`). So every tool that shells out to `hal status`,
   `podman logs`, etc. fails there. Only `get_capabilities`, `hal_policy_profile`
   and `validate_command` work. HAL Plus actually gets lab state from the
   `hal-health` sidecar, which serves a snapshot frozen at the last lifecycle
   command.
2. **It does not speak standard MCP over stdio.** It frames messages with
   LSP-style `Content-Length` headers (`cmd/mcp/mcp.go:1039`). The MCP spec
   requires newline-delimited JSON. So Claude Code, Claude Desktop and VS Code
   cannot use `hal mcp serve`. The only working client is the one in `hal-plus`,
   which mirrors the non-standard framing (acknowledged at `LLM_CONTEXT.md:78`).
3. **Its catalog drifts.** The command surface comes from hand-written Go maps
   (`cmd/mcp/advanced.go`) and from `hal …` lines regex-scraped out of `SKILL.md`
   files (`cmd/mcp/skills_index.go`). Only `hal_help` reflects the real cobra tree.
   - Concrete drift: `hal vault aap` exists, but `validate_command` rejects it,
     because `aap` is missing from the hand-written vault list.
   - No CI check exists. The help snapshots in `cmd/mcp/testdata/` are orphaned.
   - The docs are stale. The README lists removed tools. `docs/commands/mcp-serve.md`
     says stdio is the only transport. `docs/cli-lifecycle-model.md` lists
     `hal mcp update` and `hal mcp policy`, which do not exist.
4. **It is too heavy for the model it serves.** HAL Plus ran `gemma4:e4b` with a
   32k context. 39 tool definitions take a large part of that window, and small
   models choose poorly among that many tools. Every new `hal` feature also needs a
   new hand-written handler. ADR 0006 has since moved HAL Plus to a hosted model,
   which removes the context pressure there. The argument for two tools still holds
   for the other clients, local models included, and for the per-tool approval
   described in decision 4.
5. **The protocol is hand-rolled.** There is no SDK, no sessions, no SSE, and no
   tests for the stdio path.
6. **The stdio client runs a stale copy of `hal`.** `hal mcp create` copies `hal`
   to `~/.hal/bin/hal-mcp`. After the next `brew upgrade`, the MCP server no longer
   matches the CLI.

---

## Decision

Keep an MCP server, and redefine its job: it gives the LLM **eyes and hands on the
lab** — catalog, observation and action — from **any** MCP client. To do that, the
server **is the host `hal` binary**, and its tools **are `hal` itself**.

| # | Decision |
|---|---|
| 1 | **The server is always the host `hal` binary** (`hal mcp serve`). There is no MCP container. |
| 2 | **Built on the official Go SDK**, `github.com/modelcontextprotocol/go-sdk`. |
| 3 | **The catalog is the cobra command tree**, read in-process. MCP metadata lives in cobra annotations. The knowledge is `docs/topics`, served as MCP resources. |
| 4 | **Two tools, `hal_read` and `hal_run`, both taking argv**, plus server `instructions`. |
| 5 | **Only `hal` can be executed** — guaranteed by construction, not by a filter list. |
| 6 | **Every `hal` command is reachable, including full teardown.** Safety comes from the *lab invariant*, which this ADR makes true. |
| 7 | **Host-path flags are refused** through MCP. |
| 8 | **Observation gaps become `hal` commands**: `hal <product> logs` and `hal status --json`. |
| 9 | **One confirmation gate, owned by the server**: MCP elicitation, asked for every `hal_run` over HTTP and for guarded commands over stdio, with a two-step fallback for HTTP clients that cannot elicit. |
| 10 | **Execution semantics**: synchronous with progress, SIGINT on cancel, one `hal_run` at a time per server, shaped output. |
| 11 | **Two transports, two lifecycles**: stdio is owned by the client; the HTTP daemon is owned by `hal mcp create/status/delete`, and `hal plus` drives it. |
| 12 | **The HTTP daemon is protected**: loopback bind, `Origin` check, a stable bearer token. |
| 13 | **The guarantees are enforced in CI** by a test that walks the cobra tree. |

### 1. The server is the host `hal` binary

To observe and act, the server needs three things:
- access to the container engine;
- the HAL state in `~/.hal` (TFE token cache, Vault init cache, shared services… —
  read by 18 files);
- the **same version** of `hal` as the user's CLI.

Only the host has all three without workarounds. So:

- Desktop clients spawn `hal mcp serve` over **stdio**.
- HAL Plus reaches a host-side `hal mcp serve --transport streamable-http` at
  `host.containers.internal` — the same route it already uses for Ollama.
- The `hal-mcp` container is removed. The "no socket mounts, Podman stays rootless"
  rule (`LLM_CONTEXT.md:101`) is preserved trivially: there is no container left
  to mount a socket into.

### 2. Official Go SDK

`github.com/modelcontextprotocol/go-sdk`. Pin the then-current release at
implementation time, and confirm its elicitation and progress support then. It
gives us:
- spec-conformant stdio (newline-delimited JSON) and streamable HTTP;
- protocol version negotiation;
- tool input schemas derived from Go structs;
- tool annotations, elicitation and progress notifications.

This deletes the hand-rolled protocol layer: `readFramedMessage`/`writeResponse`,
the version table, and the HTTP handler. The `Origin` check is kept as middleware.
The cost is one new dependency in an otherwise minimal `go.mod`.

### 3. The catalog is the cobra tree

The server *is* `hal`, so it walks its own root command: `Use`, `Short`, `Long`,
flags, subcommands, `Deprecated` and `Hidden`. Drift is impossible by construction.

- `--help` is served through `hal_read`, because cobra generates it.
- The server's `instructions` are generated at startup from the same tree. They give
  the model a short map of the top-level commands and tell it to use `--help` to go
  deeper.
- `validate_command` disappears as a tool. "Is this command valid?" now means "does
  cobra accept it?", and it is checked inside `hal_read`/`hal_run` before anything
  is spawned.
- The **knowledge** (procedures, the *why*) is `docs/topics`, defined by ADR 0006 and
  embedded in the binary. The server declares the `resources` capability and serves
  one resource per topic, at `hal://topic/<id>`. Topics never define **which commands
  exist**: that stays the cobra tree. `SKILL.md` files no longer play this role.

MCP-specific metadata lives in cobra `Annotations`, which nothing uses today. On
flags, it lives in pflag annotations (`Flags().SetAnnotation`).

| Annotation | On | Meaning | When absent |
|---|---|---|---|
| `hal.mcp/readonly: "true"` | command | allowed through `hal_read` | treated as an action (`hal_run` only) — **fails safe**: one confirmation too many |
| `hal.mcp/ends-http-session: "true"` | command | running it over HTTP ends the current session (decision 9); HAL Plus never offers it as an action | no session warning |

Annotated today as read-only: `hal status`, `hal status --json` (decision 8),
`hal <product> logs` (decision 8), and `hal creds status`. Annotated as ending the
HTTP session: `hal plus delete`, `hal mcp delete`, `hal delete`, `hal daisy`.
| `hal.mcp/hostpath: "true" \| "false"` | flag | `"true"`: the value is a host path, so `hal_run` refuses the flag (decision 7) | **must** be set when the flag name looks like a path — enforced by CI (decision 13) |

### 4. Two tools: `hal_read` and `hal_run`

```
tools/list
  hal_read  (readOnlyHint)     args: ["vault","status"]
                               → only commands annotated read-only, plus any --help
  hal_run   (destructiveHint)  args: ["vault","create","--version","1.21"]
                               → every other command
instructions: map of top-level commands, generated from cobra
```

```
user: "my vault stopped answering"
llm:  hal_read ["vault","status"]            → auto-approved by the client
llm:  hal_read ["vault","create","--help"]   → auto-approved by the client
llm:  hal_run  ["vault","create"]            → client asks: "hal vault create — allow?"
```

- **`args` is argv** without the leading `hal`, never a command string. See
  decision 5.
- **Two tools, not one**, because MCP clients approve *per tool*. A user can set
  "always allow `hal_read`" once and still be asked before every `hal_run`. With a
  single tool, either every `status` prompts or nothing does.
- **Two tools, not 39 or 96** (there are 96 cobra commands). Each tool definition
  sits in the context window of a 4B model, and adding a `hal` feature should not
  need a new MCP handler.
- **`hal mcp serve --read-only`** registers `hal_read` only. This is the mode the
  issue-resolver agent designed in `docs/agent-architecture.md` expects.

### 5. Only `hal` can be executed

The server always runs **its own binary** (`os.Executable()`) with the argv it
received, and never through a shell. Cobra validates the argv before anything is
spawned. So `["status;","rm","-rf","~"]` is an unknown command, and `rm`, `cp` or
`curl` cannot be expressed at all.

This holds because no `hal` command is a gateway to other commands. Verified:
- no command uses `DisableFlagParsing` or `ArbitraryArgs`, so there is no
  passthrough;
- no command spawns a shell on the host;
- every `sh -c` in the codebase runs *inside* a lab container.

Each spawned `hal` runs with `TERM=dumb` and stdin closed.

### 6. Everything is reachable; the lab invariant protects it

Every `hal` command can be run through MCP, `hal delete` and `hal daisy` included.
If the model misbehaves, it destroys a lab that is **disposable by design**. The
alternative, a blocklist of "dangerous" commands, was rejected.

That only holds if this **lab invariant** is true:

> **A `hal` command only acts on resources HAL created.**

Today it is *almost* true. Two leaks are fixed before `hal_run` ships (phase 1):

| Leak | Where | Effect |
|---|---|---|
| Any kind cluster named `kind` is treated as HAL's | `cmd/teardown.go:298` | `hal delete` deletes the user's own default kind cluster |
| `multipass purge` is global | `cmd/teardown.go:140` | permanently removes *every* deleted multipass VM, including non-HAL ones |

The first one is not just a bad match. HAL's own cluster *is* called `kind`,
because `writeHALKindConfig` (`cmd/vault/helper.go:94`) sets no `name:`. The fix
therefore needs a real ownership test — for example "its node container is attached
to `hal-net`", which `ensureHALKindCluster` always does — or a rename, which touches
about 37 references to `kind`, `kind-kind` and `kind-control-plane`. The bugfix
branch makes that call. The second fix is `multipass delete --purge <vm>`, one VM at
a time.

The remaining exits from the lab are host-path flags, closed by decision 7.

**Secrets.** Tool output goes to whichever model is selected, and that can be a
cloud provider (see Context). `hal` output contains **lab credentials** (the Vault
root token, demo passwords), which are disposable by design. It never prints the
TFE license, which it only reads from `TFE_LICENSE` or `TFE_LICENSE_PATH`. This is
accepted. If `hal` ever handles a non-disposable secret, the invariant must be
extended to its output.

### 7. Host-path flags are refused

Some flags take a **host** path, and so reach outside the lab even though only `hal`
runs:

| Flag | Command | Effect |
|---|---|---|
| `--local-directory` | `hal terraform api-workflow` | **mounts any host directory** into a helper container |
| `--prom-config-path`, `--scrape-config-path` | `hal obs create` | reads a host file into the lab |
| `--oracle-plugin-path` | `hal vault database` | reads a host file into the lab |

`hal plus create --model-config` is not in this table: ADR 0006 replaces it with
`--provider`, which takes no path.

These flags are annotated `hal.mcp/hostpath: "true"`, and `hal_run` refuses any argv
that uses one. A human can still use them from a terminal.

Some flags have path-like names but refer to paths **inside** containers or Vault,
and are annotated `"false"`: `--metrics-path`, `--aap-ca-cert-file`, `--path`
(Vault audit), `--root-mount`, `--int-mount`, `--project-path`, `--http-path`.

### 8. Observation gaps become `hal` commands

**Logs.** `hal_diagnostics` runs `podman logs` today. Decision 5 forbids that. No
`hal … logs` command exists, so add `hal <product> logs [--tail N]`, annotated
read-only. The rule then has no exception, observation keeps its most useful signal,
and humans get the command too.

**Status as data.** `hal status --json` prints the lab snapshot as JSON, annotated
read-only. It does not exist today: the only `json` in `cmd/status.go` is a
`-format=json` passed to Terraform. It is built on `BuildStatusSnapshot`
(`internal/global/status_snapshot.go`), which already produces the shape HAL Plus
reads for its status panel. This replaces the `hal-health` sidecar and its frozen
snapshot (see Consequences): the panel calls `hal_read ["status","--json"]` and
always sees the live lab.

### 9. One confirmation gate, owned by the server

The server is the only guard. A client bug, or a model that misbehaves, must not be
able to start a command that no human answered for. The gate depends on the
transport, because the two transports have different clients.

**What triggers it.**

| Transport | `hal_run` goes through the gate when… |
|---|---|
| **HTTP** | **always**. Every `hal_run` asks, whatever the command. |
| **stdio** | the command defines `--auto-approve` (detected from cobra, so there is no annotation to forget; today `hal delete`, `hal terraform agent`, `hal terraform vcs-workflow`, `hal terraform api-workflow`). |

The `ends-http-session` annotation adds the session warning to the question, but does
not trigger the gate by itself: over stdio the client owns the server process, which
none of these commands affect, and over HTTP every `hal_run` is asked anyway.

Over stdio, the client already approves `hal_run` per tool (decision 4), so asking
again for every command would only add noise. Over HTTP the client may be a chat that
has no per-tool approval, so the server asks itself.

**What the gate does.**

1. **Elicitation**, if the client declares the capability. The server asks the user
   directly, without going through the model, with an empty `requestedSchema`: the
   answer is accept, decline or cancel, and nothing else.
   - The message is written by the server and contains the **exact argv** that will
     run.
   - For a command that defines `--auto-approve`, it adds the command's original
     `[y/N]` text (for example "This will destroy ALL HAL containers, clusters, and
     VMs.").
   - For a command annotated `ends-http-session`, over HTTP, it adds a warning that
     the command ends this session.
   - If the user accepts, the server runs the command. For a command that defines
     `--auto-approve`, **the server appends it**, since elicitation is what tells the
     server that a human said yes.
   - If the user declines or cancels, the tool returns "Cancelled by the user." and
     nothing is spawned.
   - **If nobody answers within 5 minutes**, the server cancels the question and
     nothing is spawned.
2. **Two-step fallback**, only when the gate applies and **the client does not declare
   elicitation**. Over HTTP that is any client without the capability, such as the MCP
   Inspector; HAL Plus always declares it, so it never uses the fallback. Over stdio
   it is the guarded commands only, as before. The first call returns the warning
   instead of running the command, with an instruction to relay it to the user and
   call again with `confirmed: true`. This reaches the user in most cases, but a
   careless model can skip the user and call again straight away. That risk is
   accepted, because of decision 6.
3. **`hal_run` rejects `--auto-approve` in the model's argv**, otherwise the gate
   would be trivial to bypass. For the same reason, `confirmed` is honoured only when
   the client has not declared elicitation: a client that declared it cannot skip the
   question by sending `confirmed: true`.

**Execution has no timeout from the server.** Once approved, a command runs as long as
it needs (decision 10). Only the question is bounded.

**Accepted limitation.** The three `hal terraform` commands accept `--auto-approve`
but only prompt on `disable`. Over stdio, their `enable` path is therefore gated too,
which costs one unnecessary confirmation. This can be refined with an annotation at
implementation time if it proves annoying. Over HTTP it makes no difference, since
every `hal_run` asks.

### 10. Execution semantics

- **Synchronous, with progress.** When the client sends a `progressToken`, the
  server sends `notifications/progress` with the last lines of output, which also
  keeps the connection open. Long commands, such as `hal terraform create`, depend
  on the client's tool timeout: HAL Plus imposes none on execution, and
  `hal mcp create` prints the `MCP_TOOL_TIMEOUT` hint for Claude Code. Only the
  confirmation question has a limit (decision 9).
- **A daemon restart interrupts the run.** `hal mcp create` restarts the HTTP daemon
  when its version differs from the binary's (decision 11). A command running at that
  moment is cut. The client sees its session answer 404, initializes again (the
  token is stable, decision 12), and tells the user the run was interrupted and to
  check the status.
- **Cancellation sends SIGINT** to the child `hal`. It has exactly the meaning of a
  Ctrl-C in a terminal.
- **One `hal_run` at a time per server process.** `hal_read` calls run freely. A
  global lock across all `hal` processes, terminal included, is out of scope: this
  is a local lab tool, not Terraform.
- **Output.** stdout and stderr are merged, and residual ANSI sequences are removed.
  A non-zero exit sets `isError: true`. Past a size cap, the server keeps the
  **tail**, where errors are, and adds a truncation marker. The existing non-TTY
  degradation (`LLM_CONTEXT.md:37`) already produces plain output.

### 11. Two transports, two lifecycles

```
stdio — owned by the client
  once:          claude mcp add hal -- hal mcp serve      (config lives in the client)
  every session: the client spawns `hal mcp serve` and kills it on exit
  → nothing to create or delete on the HAL side

HTTP — owned by hal mcp, driven by hal plus
  hal mcp create   → starts `hal mcp serve --transport streamable-http` detached on 127.0.0.1:<port>
                     writes PID under ~/.hal/mcp/; creates the token once (decision 12)
                     restarts the daemon if its version differs from this binary's
                     prints the path of the token file, and the client setup lines:
                     stdio for Claude Code / VS Code / Claude Desktop, and the HTTP URL
                     with `Authorization: Bearer $(cat ~/.hal/mcp/token)`
  hal mcp status   → daemon running? version? /healthz
  hal mcp delete   → stops the daemon; removes PID, token and legacy artifacts
                     (~/.hal/bin/hal-mcp, ~/.hal/mcp/hal-mcp.json)
  hal plus create  → hal mcp create (no-op if already running), then starts hal-plus,
                     injecting the token as HAL_MCP_TOKEN
  hal plus delete  → removes hal-plus and the leftovers of the old layout (hal-mcp,
                     hal-qdrant, hal-health), then calls hal mcp delete
  hal delete       → stops the daemon too, via the existing MCP cleanup in teardown
```

**What the commands ask, and what they print.**

- `hal plus delete` and `hal mcp delete` **do not prompt** in the terminal. They
  stop the daemon and print afterwards what they stopped, with a warning that any open
  chat or HTTP MCP client is now disconnected.
- `hal delete` keeps its existing `[y/N]` prompt, whose text now also says that
  HAL Plus and the MCP server will be stopped.
- `hal daisy`, the hidden command, has no prompt, as before.

**Commands that end the HTTP session.** `hal plus delete`, `hal mcp delete`, `hal delete`
and `hal daisy` are annotated `hal.mcp/ends-http-session`. HAL Plus never runs them,
and its client refuses them as a second line of defence. Its chat shows the command
to the user, with the warning that the session will be lost, and no button. The test
in decision 13 checks that no topic lists them in its `actions`.

- `hal mcp` follows the usual `create/status/delete` lifecycle
  (`docs/cli-lifecycle-model.md`). The MCP server can also be tested without HAL
  Plus, for example with `hal mcp create` and the MCP Inspector.
- **No binary copy.** Client configs point at the user's own `hal`, so the server
  always matches the CLI.
- `hal mcp create` **prints** the client configuration; it never writes into
  third-party config files.

### 12. Protecting the HTTP daemon

Over stdio, only the client that spawned the server can talk to it, through pipes.
The HTTP daemon listens on a port instead:

| Who can reach it | Path | Protected by |
|---|---|---|
| `hal-plus` | `host.containers.internal` | intended |
| **every other container**: Vault, GitLab, TFE, Authentik, AAP, kind pods, any third-party image | the same path | **bearer token** |
| a web page in the user's browser | `fetch` to `127.0.0.1` | `Origin` check (already exists) |
| another OS user on the machine | loopback | bearer token |
| the user's own processes | — | nothing needed: they can already run `hal` |

`hal mcp create` generates the token **once**, writes it to `~/.hal/mcp/token` with
mode `0600`, and reuses it when the daemon restarts after a version change. Only
`hal mcp delete` and `hal delete` remove it. `hal plus create` injects it into
`hal-plus` as `HAL_MCP_TOKEN`. Containers cannot read host files, so they cannot get
the token on their own. OAuth 2.1, as the MCP spec describes it, is disproportionate
for a single-user local tool.

The token is **never printed**, not even in a dry run. `hal mcp create` shows the
path of the file, and the client configuration refers to it, as in
`Authorization: Bearer $(cat ~/.hal/mcp/token)`.

A stable token means a restart does not invalidate a client's configuration. The
cost is that a leaked token stays valid until `hal mcp delete` or `hal delete` removes
it. That is accepted for a single-user local lab, where the attacker would already
need a process on the user's machine.

### 13. Enforced in CI

A Go test walks the cobra tree and fails when:

- a flag whose name contains `path`, `dir`, `file` or `config` has no
  `hal.mcp/hostpath` classification. This is needed because a missing `hostpath`
  annotation fails **open**, unlike a missing `readonly`;
- a command known to end the HTTP session lacks `hal.mcp/ends-http-session`.

Unit tests cover:
- argv validation: unknown commands, the shell-metacharacter case, host-path
  refusal, `--auto-approve` rejection;
- the gate, in both its elicitation and fallback modes;
- `hal_run` serialization.

The "is the MCP catalog in sync with the CLI?" test that is missing today becomes
unnecessary: the catalog *is* the CLI.

---

## Consequences

**The MCP becomes portable.** Claude Code, VS Code and Claude Desktop can use it over
stdio from phase 3 on. It is the first time any client other than `hal-plus` can.

**HAL Plus gains live observation and action** once phase 4 lands. Today it gets
neither from MCP.

**A lot of code goes away.**
- The hand-rolled protocol.
- 37 of the 39 tools.
- `validate_command`, `hal_policy_profile`, `get_capabilities` and the skill-index
  scraping (`cmd/mcp/skills_index.go`).
- The orphaned `cmd/mcp/testdata/*_help_snapshot.json`.
- The binary copy in `~/.hal/bin/hal-mcp`.
- The `hal-mcp` container.

The image, `HAL_MCP_CONTRACT.json` and the response envelope are removed with the
legacy server, in phase 4 (see Consequences).

**HAL Plus breaks unless both repos migrate together.** It prefetches
`hal_status_baseline`, `get_capabilities`, `hal_policy_profile` and
`validate_command`, and it parses the contract envelope. Phase 3 keeps the legacy
server behind the streamable-HTTP path, so HAL Plus is untouched until phase 4.
Phase 4 then follows the cross-repo rule (`LLM_CONTEXT.md:277`).

**The HAL Plus client gets new obligations**, all listed in ADR 0006:
- implement elicitation, and do not use the two-step fallback: HAL Plus always
  declares the capability, so the server asks the user for every `hal_run`;
- send the bearer token from `HAL_MCP_TOKEN`;
- use a long tool timeout, and relay progress notifications;
- show "interrupted, check the status" when the daemon restarts during a run;
- refuse the four `ends-http-session` commands, even if a topic lists them, and show
  them as text only.

**The LLM can destroy the whole lab.** This is accepted by design and bounded by the
lab invariant. The four commands that end the HTTP session are not reachable from
HAL Plus at all: the chat displays them and the user runs them from a terminal. The
server itself refuses none of them. Over stdio, a Claude Code user who approves
`hal_run ["delete"]` gets the `[y/N]` text of `hal delete` and nothing about a session,
since none exists to lose. Over HTTP, any other client gets the session-ending warning
in the question.

**The `hal-health` sidecar and the `hal-mcp` image go away.** `hal-health` serves a
snapshot frozen at the last lifecycle command. Its replacement is
`hal status --json` (decision 8 of this ADR, phase 2). The removal covers the
container, `hal health`, `RefreshHalHealth` and `RemoveHalHealth` (39 call sites in
`cmd/`), the `ghcr.io/hashimiche/hal-mcp` image and its publishing workflow, and
`HAL_MCP_CONTRACT.json` with the response envelope.

**Lab credentials reach the selected model provider**, cloud included. This is
accepted, because they are disposable.

**Contributors have three annotations to think about.** CI enforces the one that
fails open (`hostpath`). Forgetting `readonly` only costs a confirmation.

**The CLI gains `hal <product> logs`**, for humans too.

**The MCP docs are rewritten**, not patched: `README.md` (MCP section),
`LLM_CONTEXT.md` (MCP and HAL Plus sections), `docs/commands/mcp*.md`,
`docs/cli-lifecycle-model.md`, `internal/skills/data/mcp/SKILL.md` and
`CONTRIBUTING.md` (doc-sync row).

**Unrelated bugs found along the way**, out of scope here:
- `release.yml:81` injects `-X main.version`, but the variable is
  `hal/cmd.Version`;
- `hal plus create` checks for the MCP image *before* honouring `--pull`
  (`cmd/plus/create.go:109-125`);
- nothing writes the PID file that `hal mcp delete` cleans up. Decision 11 gives it
  a writer.

---

## Implementation plan

Each phase is its own branch and PR, in this order.

### Phase 1 — `bugfix/teardown-scope`

Make the lab invariant true (decision 6). It is a prerequisite for `hal_run`.

- Ownership test for kind clusters, or a rename of HAL's cluster, which the branch
  decides. Either way, a kind cluster HAL did not create is never deleted.
- Replace the global `multipass purge` with `multipass delete --purge <vm>` for each
  HAL VM.

### Phase 2 — `feature/hal-logs`

- `hal <product> logs [--tail N]` for each product, annotated `hal.mcp/readonly`.
- `hal status --json`, annotated `hal.mcp/readonly`, built on `BuildStatusSnapshot`
  (decision 8).

### Phase 3 — `feature/mcp-host-server`

The new server over **stdio**. The legacy server stays on the streamable-HTTP path,
so HAL Plus is unchanged until phase 4. The new server's HTTP transport, and the HTTP
gate of decision 9, come in phase 4.

Starts with a **spike** on the Go SDK, before any other work: elicitation during a
`tools/call`, an empty `requestedSchema`, progress notifications, and the 5-minute
question timeout. The spike tests both transports. Over **streamable HTTP**, it must
show that the elicitation request travels on the SSE stream of the `tools/call` in
progress, and that the answer comes back and unblocks the call. This is the highest
risk of the whole redesign, because the HTTP gate of decision 9 depends on it. The
spike is throwaway: it may exercise HTTP without shipping any of it, since the HTTP
transport ships in phase 4. Its findings go into this phase's PR description.

- Official Go SDK; stdio transport.
- `hal_read`, `hal_run` and generated `instructions` (decisions 3–5).
- Topics as resources: the `resources` capability, one resource per topic at
  `hal://topic/<id>`, served from the embedded `docs/topics` (decision 3).
- The three annotations on existing commands and flags (decisions 3 and 7).
- The confirmation gate (decision 9) and the execution semantics (decision 10).
- `--read-only`.
- The CI tree-walk test (decision 13).
- `hal mcp create` prints client setup lines; the binary copy is removed.
- Docs for the stdio path.

This phase is L5. It runs in a single session on a frontier model, with no other L5
task in parallel.

### Phase 4 — with `hal plus create --provider` (ADR 0006, T05)

Merged with T05, because both touch `cmd/plus/create.go` and both are the same work
on the HTTP daemon and the token.

- The new server over streamable HTTP, with the HTTP gate of decision 9 (elicitation
  for every `hal_run`) and the `Origin` middleware.
- The HTTP daemon on the host, with its stable token and the `hal mcp create/status/delete`
  lifecycle (decisions 11 and 12).
- The `hal plus create/delete` wiring, and `hal plus create --provider` (ADR 0006).
- The HAL Plus client migration, coordinated in `hal-plus` (ADR 0006, T07 and T08).
- Removal of the legacy server, of the `hal-mcp` container, of `hal-health` and
  `hal health`, and of the image and its publishing (ADR 0006, T11).
- `HAL_MCP_CONTRACT.json` and the response envelope go with the legacy server.

---

## Verification

To be completed per phase. Minimum bar for phase 3:

### Static

```bash
go build ./... && go vet ./... && go test ./...
gofmt -l ./cmd ./internal
```

### Runtime

```bash
claude mcp add hal -- hal mcp serve
# tools/list → exactly hal_read, hal_run (hal_read only with --read-only)
# hal_read ["status"]                          → lab status, isError=false
# hal_read ["vault","create"]                  → refused: not a read-only command
# hal_run  ["status;","rm","-rf","~"]          → refused by cobra, nothing spawned
# hal_run  ["terraform","api-workflow","enable","--local-directory","/"] → refused: host path
# hal_run  ["delete","--auto-approve"]         → refused: --auto-approve is gate-only
# hal_run  ["delete"]                          → elicitation shown, the [y/N] text quoted
# cancel during `hal_run ["terraform","create"]` → child receives SIGINT
# two concurrent hal_run calls                 → second waits for the first
# MCP Inspector over stdio                     → handshake and both tools work
```

HTTP, from phase 4. The gate itself is also covered by Go tests, which need no Docker:

```bash
# hal_run over HTTP, any command                → elicitation always sent, even for `status`
# hal_run over HTTP, user declines              → nothing spawned, "Cancelled by the user."
# hal_run over HTTP, no answer for 5 minutes    → question cancelled, nothing spawned
# hal_run over HTTP, client declares no elicitation → confirmed: true is honoured only then
# hal_run ["plus","delete"] over HTTP           → elicitation carries the session warning
# the stable token appears in no output         → grep the stdout and stderr of
#                                                 hal mcp create, hal mcp status and hal plus create
```

---

## Alternatives considered and rejected

- **No MCP, only a static context** (`LLM_CONTEXT.md`, `llms.txt`, a good
  `--help`). Fine for the catalog, but a chat UI has no shell, so no observation and
  no action.
- **MCP for observation and action, with the catalog as a static context.**
  Rejected: it gives the client two entry points, and an on-demand catalog is cheaper
  for a small model than injecting all of it up front.
- **A container with the engine socket and `~/.hal` mounted.** It breaks the
  "no socket mounts" rule. With Docker, it is root-equivalent. And it leaves two
  `hal` binaries to keep in sync.
- **A container that reads the `hal-health` snapshot.** Observation stays frozen,
  and action is impossible.
- **Keep the hand-rolled protocol and fix the framing.** It still lacks sessions,
  SSE, elicitation and progress, and keeps a protocol layer to maintain.
- **`mark3labs/mcp-go`.** A capable community SDK; the official one is preferred.
- **Catalog from hand-maintained maps or `SKILL.md` scraping**, which is the status
  quo. It already drifts: `hal vault aap` is rejected today.
- **One tool per cobra command (96), or the 39 current tools plus `hal_run`.** It
  costs too much context and confuses small models.
- **A single `hal(args)` tool.** Approval is per tool, so a single tool cannot
  separate reads from actions.
- **A dedicated `hal_help` tool.** `--help` already goes through `hal_read`.
- **The command as a string.** Someone has to parse it, and that creates an
  injection surface.
- **A blocklist** of teardown commands, or of the `hal mcp` / `hal plus` subtrees.
  Rejected in favour of full reachability, the lab invariant and the
  session-ending warning.
- **Actions disabled by default (`--allow-actions`).** Rejected: actions are on, and
  `--read-only` is the opt-out.
- **Letting the model pass `--auto-approve` itself.** It bypasses the human.
- **A warning in the tool description only.** It relies on the model, which is
  fragile with a 4B model.
- **Asynchronous jobs.** They need a third tool.
- **Letting a cancelled command keep running.** The user would have no way to stop
  it.
- **A global `~/.hal` lock.** It is a change to the whole CLI, beyond what a local
  lab tool needs.
- **No token on the HTTP daemon.** Any lab container could drive `hal`. **OAuth
  2.1** is disproportionate.
- **A daemon owned only by `hal plus`.** `hal mcp delete` would mean nothing, and
  the MCP could not be tested without HAL Plus.
- **An OS service** (launchd, systemd). Too heavy for something only HAL Plus needs.
- **`hal mcp create` writing third-party client configs.** It is intrusive, and those
  formats change.
- **A runtime value heuristic for host paths** (refusing values that start with `/`
  or `~`). It gives false positives, such as `--metrics-path /metrics`. The CI
  classification is deterministic.
- **Redacting secrets from output.** It is a fragile heuristic, and the model loses
  information it sometimes needs.

---

## Open questions

1. **The environment of a GUI-launched stdio server.** Claude Desktop, launched from
   the Dock, starts `hal mcp serve` with macOS's minimal `PATH`. So `podman`, `kind`,
   `multipass` and `ollama` are not found, and `TFE_LICENSE_PATH` is missing.
   Claude Code and the HTTP daemon inherit a terminal's environment and are
   unaffected. The proposal: `hal mcp create` captures the terminal's `PATH` and the
   non-secret HAL variables (`TFE_LICENSE_PATH` yes, `TFE_LICENSE` never) into the
   `env` block of the printed client config. Rejected so far: reloading a login shell
   at startup (slow, depends on rc files), and a CLI-wide `~/.hal/env` file (out of
   scope).
2. **The HTTP daemon port.** Fixed by default and configurable. Not `8080`, which is
   too common.
