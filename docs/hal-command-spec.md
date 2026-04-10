# HAL Command Specs

The HAL command specification has been split into one file per command area so you can iterate on a single command without touching unrelated sections.

Start here:
- [docs/commands/index.md](commands/index.md)

Related detailed spec:
- [docs/terraform-cli-container-spec.md](terraform-cli-container-spec.md)

Source of truth for command registration remains Cobra `AddCommand(...)` wiring in `cmd/`.
