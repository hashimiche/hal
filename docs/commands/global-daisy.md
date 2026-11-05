# HAL Daisy Command Spec

## Command
- `hal daisy` (hidden)

## Purpose
Run cinematic global teardown easter egg.

## Behavior
- Hidden command
- Uses the same global teardown backend as `hal delete` (`runGlobalTeardown`)
- Removes the KinD cluster HAL created (named `kind`, a node attached to `hal-net`), not only `hal-*` containers. A KinD cluster you created yourself is left alone and reported.
- Removes each HAL Multipass VM with `multipass delete --purge <vm>`; there is no global `multipass purge`.
- Removes a `tfe-agent` / `tfc-agent` container that is not named `hal-*` only when it is attached to `hal-net`; a TFC/TFE agent you run yourself is left alone.
- Discovers KinD clusters via the container engine, not `kind get clusters`, which fails on Podman 6 with older kind CLIs (Labels stored as a slice). That template error is not reported as a teardown warning.
- Has no confirmation prompt and no `--dry-run` handling: use `hal delete --dry-run` to preview the same teardown.

## Related
- Parent: [global.md](global.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.

## Flags
- Command flags from `hal daisy --help`:
```text
-h, --help   help for daisy
```
- Global flags: `--debug`, `--dry-run`, `--verbose`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal daisy
```
