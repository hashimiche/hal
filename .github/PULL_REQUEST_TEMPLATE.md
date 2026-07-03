<!--
Thanks for contributing to HAL! Please fill out the sections below.
See CONTRIBUTING.md for branch naming, the doc-sync rule, and conventions.
-->

## Summary

<!-- What does this PR do, and why? Link any related issue. -->

## Type of change

- [ ] New capability (branch `feature/...`)
- [ ] Bug fix (branch `bugfix/...`)
- [ ] Docs / internal only

## Checklist

- [ ] Branch follows `feature/<desc>` or `bugfix/<desc>` naming
- [ ] `go build ./...` passes
- [ ] `go test ./...` passes
- [ ] `go vet ./...` is clean
- [ ] Change is scoped to one logical concern (repo squash-merges PRs)

## Doc-sync (required when command behavior/flags/lifecycle change)

<!-- Tick every surface your change touches. Delete rows that don't apply,
     but do not skip a surface that your change affects. -->

- [ ] `docs/cli-lifecycle-model.md` — lifecycle verbs / naming semantics
- [ ] `.github/copilot-instructions.md` — policy or convention deltas
- [ ] `LLM_CONTEXT.md` — command behavior, flags, or workflows
- [ ] `README.md` — contributor-facing command behavior
- [ ] `.github/copilot/skills/**/*.md` — AI skill guidance
- [ ] MCP contracts: `HAL_MCP_CONTRACT.json`, `cmd/mcp/ops_api.go`, `cmd/mcp/testdata/*_help_snapshot.json`
- [ ] N/A — this change does not alter command behavior

## Notes for reviewers

<!-- Anything reviewers should focus on, manual test steps, screenshots, etc. -->
