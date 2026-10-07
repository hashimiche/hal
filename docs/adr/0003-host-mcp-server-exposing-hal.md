# 3. The MCP server is the host `hal` binary, and it exposes `hal` itself

- **Status:** Proposed — **draft**, to be revised after the HAL Plus discussion
  (several decisions below depend on where HAL Plus runs; see
  [Open questions](#open-questions))
- **Date:** 2026-10-02
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
4. **It is too heavy for the model it serves.** HAL Plus runs `gemma4:e4b` with a
   32k context. 39 tool definitions take a large part of that window, and small
   models choose poorly among that many tools. Every new `hal` feature also needs a
   new hand-written handler.
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
| 3 | **The catalog is the cobra command tree**, read in-process. MCP metadata lives in cobra annotations. |
| 4 | **Two tools, `hal_read` and `hal_run`, both taking argv**, plus server `instructions`. |
| 5 | **Only `hal` can be executed** — guaranteed by construction, not by a filter list. |
| 6 | **Every `hal` command is reachable, including full teardown.** Safety comes from the *lab invariant*, which this ADR makes true. |
| 7 | **Host-path flags are refused** through MCP. |
| 8 | **Logs become a `hal` command**: `hal <product> logs`. |
| 9 | **One confirmation gate**: MCP elicitation, with a two-step fallback. |
| 10 | **Execution semantics**: synchronous with progress, SIGINT on cancel, one `hal_run` at a time per server, shaped output. |
| 11 | **Two transports, two lifecycles**: stdio is owned by the client; the HTTP daemon is owned by `hal mcp create/status/delete`, and `hal plus` drives it. |
| 12 | **The HTTP daemon is protected**: loopback bind, `Origin` check, bearer token. |
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
- `SKILL.md` files remain the **knowledge** (procedures, the *why*). They no longer
  define **which commands exist**.

MCP-specific metadata lives in cobra `Annotations`, which nothing uses today. On
flags, it lives in pflag annotations (`Flags().SetAnnotation`).

| Annotation | On | Meaning | When absent |
|---|---|---|---|
| `hal.mcp/readonly: "true"` | command | allowed through `hal_read` | treated as an action (`hal_run` only) — **fails safe**: one confirmation too many |
| `hal.mcp/ends-http-session: "true"` | command | running it over HTTP ends the current session (decision 9) | no session warning |
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
| `--model-config` | `hal plus create` | reads a host file |

These flags are annotated `hal.mcp/hostpath: "true"`, and `hal_run` refuses any argv
that uses one. A human can still use them from a terminal.

Some flags have path-like names but refer to paths **inside** containers or Vault,
and are annotated `"false"`: `--metrics-path`, `--aap-ca-cert-file`, `--path`
(Vault audit), `--root-mount`, `--int-mount`, `--project-path`, `--http-path`.

### 8. Logs become a `hal` command

`hal_diagnostics` runs `podman logs` today. Decision 5 forbids that. No `hal … logs`
command exists, so add `hal <product> logs [--tail N]`, annotated read-only. The
rule then has no exception, observation keeps its most useful signal, and humans get
the command too.

### 9. One confirmation gate

`hal_run` passes a command through a single gate when either of these is true:

- **the command defines `--auto-approve`.** This is detected from cobra, so there is
  no annotation to forget. Today: `hal delete`, `hal terraform agent`,
  `hal terraform vcs-workflow`, `hal terraform api-workflow`.
- **the command is annotated `hal.mcp/ends-http-session` and the transport is
  HTTP.** Today: `hal plus delete`, `hal mcp delete`, `hal delete`, `hal daisy`. Over
  stdio the client owns the server process, which none of these commands affect, so
  no warning is given.

The gate works like this:

1. **Elicitation**, if the client declares the capability. The server asks the user
   directly, without going through the model. The message reuses the command's
   original `[y/N]` text (for example "This will destroy ALL HAL containers,
   clusters, and VMs."), plus "…and it will also end this session." when relevant.
   - If the user accepts, **the server appends `--auto-approve`** and runs the
     command. Elicitation is what lets the server know a human said yes.
   - If the user declines, the tool returns "Cancelled by the user."
2. **Two-step fallback**, if the client cannot elicit. The first call returns the
   warning instead of running the command, with an instruction to relay it to the
   user and call again with `confirmed: true`. This reaches the user in most cases,
   but a careless model can skip the user and call again straight away. That risk is
   accepted, because of decision 6.
3. **`hal_run` rejects `--auto-approve` in the model's argv**, otherwise the gate
   would be trivial to bypass.

**Accepted limitation.** The three `hal terraform` commands accept `--auto-approve`
but only prompt on `disable`. Their `enable` path is therefore gated too, which costs
one unnecessary confirmation. This can be refined with an annotation at
implementation time if it proves annoying.

### 10. Execution semantics

- **Synchronous, with progress.** When the client sends a `progressToken`, the
  server sends `notifications/progress` with the last lines of output. Long
  commands, such as `hal terraform create`, depend on the client's tool timeout: it
  is long in the HAL Plus client, and `hal mcp create` prints the
  `MCP_TOOL_TIMEOUT` hint for Claude Code.
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
                     writes PID and token under ~/.hal/mcp/
                     restarts the daemon if its version differs from this binary's
                     prints client setup lines: stdio for Claude Code / VS Code / Claude Desktop,
                     and the HTTP URL with its token
  hal mcp status   → daemon running? version? /healthz
  hal mcp delete   → stops the daemon; removes PID, token and legacy artifacts
                     (~/.hal/bin/hal-mcp, ~/.hal/mcp/hal-mcp.json)
  hal plus create  → hal mcp create (no-op if already running), then starts hal-plus with URL and token
  hal plus delete  → removes hal-plus and hal-qdrant, then hal mcp delete
  hal delete       → stops the daemon too, via the existing MCP cleanup in teardown
```

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

`hal mcp create` generates the token, writes it to `~/.hal/mcp/token` with mode
`0600`, and `hal plus create` injects it into `hal-plus`. Containers cannot read
host files, so they cannot get the token. OAuth 2.1, as the MCP spec describes it,
is disproportionate for a single-user local tool.

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

The fate of the image, `HAL_MCP_CONTRACT.json` and the response envelope is still
open — see below.

**HAL Plus breaks unless both repos migrate together.** It prefetches
`hal_status_baseline`, `get_capabilities`, `hal_policy_profile` and
`validate_command`, and it parses the contract envelope. Phase 3 keeps the legacy
server behind the streamable-HTTP path, so HAL Plus is untouched until phase 4.
Phase 4 then follows the cross-repo rule (`LLM_CONTEXT.md:277`).

**The HAL Plus client gets new obligations**:
- implement elicitation, or rely on the weaker two-step fallback;
- send the bearer token;
- use a long tool timeout;
- relay progress notifications.

**The LLM can destroy the whole lab.** This is accepted by design and bounded by the
lab invariant. Running `hal delete` from the HAL Plus chat ends the chat session,
after a warning.

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

### Phase 3 — `feature/mcp-host-server`

The new server over **stdio**. The legacy server stays on the streamable-HTTP path,
so HAL Plus is unchanged.

- Official Go SDK; stdio transport.
- `hal_read`, `hal_run` and generated `instructions` (decisions 3–5).
- The three annotations on existing commands and flags (decisions 3 and 7).
- The confirmation gate (decision 9) and the execution semantics (decision 10).
- `--read-only`.
- The CI tree-walk test (decision 13).
- `hal mcp create` prints client setup lines; the binary copy is removed.
- Docs for the stdio path.

### Phase 4 — after the HAL Plus ADR

- The HTTP daemon on the host, with its token and the `hal mcp create/status/delete`
  lifecycle (decisions 11 and 12).
- The `hal plus create/delete` wiring.
- The HAL Plus client migration, coordinated in `hal-plus`.
- Removal of the legacy server and of the `hal-mcp` container.
- The decision on the image and the contract.

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
2. **Deferred to the HAL Plus discussion:**
   - **Where HAL Plus runs.** If it moves to the host, it can talk to the server over
     stdio, and decisions 11 and 12 shrink to almost nothing.
   - **The fate of `ghcr.io/hashimiche/hal-mcp`.** `hal-health` reuses that image.
   - **Structured outputs.** Whether HAL Plus still needs the contract envelope, or
     JSON output from `hal` itself.
   - **The HAL Plus client obligations** listed under Consequences.
3. **The HTTP daemon port.** Fixed by default and configurable. Not `8080`, which is
   too common.
