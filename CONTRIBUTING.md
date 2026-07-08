# Contributing to HAL

Thanks for contributing to HAL. This guide covers the essentials for landing a
change. The deeper design context lives elsewhere (see
[Sources of truth](#sources-of-truth)) — this file is the short version you need
before opening a pull request.

## Filing issues

Before writing code, open an issue so the change can be discussed. Use the
templates in the issue chooser — blank issues are disabled to keep reports
actionable:

- **Bug report** — for defects in command behavior, output, or lifecycle
  handling. Include the exact commands you ran, expected vs. actual behavior,
  your `hal version`, and your OS / container engine.
- **Feature request** — for new capabilities or enhancements. Describe the
  problem first, then a proposed command surface that fits the two-tier CLI
  conventions below.

Search existing issues before opening a new one, and keep each issue scoped to a
single bug or request.

## Quick start

```bash
# From the repo root
go build -o hal main.go   # build the binary the way CI does
go build ./...            # verify all packages compile
go test ./...             # run the full test suite
go vet ./...              # static checks
```

All four must pass before you open a PR.

## Branch naming

Every change lands on a named branch before merging to `main` — never commit to
`main` directly.

- New capabilities: `feature/<short-description>`
- Bug fixes or corrections: `bugfix/<short-description>`

The repository squash-merges PRs, so keep your branch focused on one logical
change.

## The doc-sync rule (most-missed step)

HAL's command surface is consumed by AI tooling (Copilot, the HAL Plus UI, and
the MCP server), so **documentation is part of the code, not an afterthought.**
When you change command behavior, naming, flags, or lifecycle semantics, update
the affected surfaces **in the same PR**:

| If you change… | Also update… |
| --- | --- |
| Lifecycle verbs / naming semantics | `docs/cli-lifecycle-model.md` (source of truth) |
| Policy or architecture conventions | `.github/copilot-instructions.md` |
| Command behavior, flags, or workflows | `LLM_CONTEXT.md` |
| Contributor-facing command behavior | `README.md` |
| MCP command syntax or contracts | `HAL_MCP_CONTRACT.json`, `cmd/mcp/ops_api.go`, `cmd/mcp/testdata/*_help_snapshot.json` |
| AI skill guidance | `.github/copilot/skills/**/*.md` |

A PR that changes command behavior without updating the matching docs will be
sent back. The PR template has a checklist to help you catch this.

## Key conventions

- **Two-tier CLI.** Core products use verb subcommands (`hal vault create`);
  feature/integration flows use noun + lifecycle-action subcommands
  (`hal vault oidc enable`). See `LLM_CONTEXT.md` for the full pattern.
- **Smart status default.** Running a command with no lifecycle action returns a
  read-only status view (up / down / degraded) ending in a copy-pasteable
  `Next Step`, not Cobra help.
- **Naming:** the observability namespace is `obs`, not `observability`.
- **No hardcoded images/versions.** Any path that launches a container, KinD
  cluster, Helm release, or VM must expose a version/image override flag with a
  sensible default.
- **Reuse shared runtime helpers** in `internal/global` (engine detection,
  `hal-net` management, `HalNetStaticIP`) instead of open-coding them.

## Commit and PR discipline

- Keep commits scoped and messages descriptive (imperative mood, e.g.
  `feat(mcp): add ...`, `fix(vault): ...`).
- Do not disable safety checks (no `--no-verify`) or force-push shared branches.
- Confirm `go build ./...`, `go test ./...`, and `go vet ./...` are green
  locally before requesting review.

## Sources of truth

Read these in order before changing command behavior or UX patterns:

1. [`docs/cli-lifecycle-model.md`](docs/cli-lifecycle-model.md) — authoritative lifecycle verb model
2. [`.github/copilot-instructions.md`](.github/copilot-instructions.md) — concise policy and architecture notes
3. [`LLM_CONTEXT.md`](LLM_CONTEXT.md) — repo-specific architecture patterns and implementation lessons

Keep all three in sync when adding or renaming commands.
