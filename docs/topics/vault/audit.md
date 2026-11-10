summary: Vault audit logging with a file device on a Docker volume that Promtail reads for Loki.
read: hal vault audit | hal vault status | hal obs status
actions: hal vault audit enable | hal vault audit update | hal vault audit disable
docs:
- Vault audit devices: https://developer.hashicorp.com/vault/docs/audit#availability-of-audit-devices
- Hashing sensitive values: https://developer.hashicorp.com/vault/docs/audit#hashing-sensitive-values
- Vault file audit device: https://developer.hashicorp.com/vault/docs/audit/file#file_path

# Vault audit logging

## The mechanism

An audit device is a place where Vault writes a record of every request it
receives and every response it sends. Vault hashes most string values in them
with a keyed hash (HMAC-SHA256), so the log shows that a secret was asked for
without holding the secret itself.

Vault treats the audit log as a duty, not a courtesy. It sends each entry to every
enabled device and insists on saving it to at least one. If devices are enabled
and none of them can take the entry, Vault refuses the request. A full disk under
the only device is enough to make Vault stop answering.

## How this lab stages it

- **The device is a Vault file device**, mounted at `file/` by default, writing to
  `/vault/logs/audit.log` inside the `hal-vault` container.
- **That path is a Docker volume**, `hal-vault-logs`, which `hal vault create`
  mounts into the Vault container. The observability stack mounts the same volume
  read-only in its Promtail container. Promtail follows the file and pushes the
  lines to Loki under the job label `vault-audit`. So Vault only ever writes to local
  disk. If Promtail or Loki is down, Vault does not notice and does not block.
- **The observability stack is separate.** `hal obs create` starts Promtail, Loki
  and Grafana. The audit command does not start them, and it does not check that
  they exist. Without them the file is simply written and nothing reads it.
- **`--loki` configures the same device as the plain command.** Both create a file
  device at `/vault/logs/audit.log`. The flag changes the messages HAL prints and
  makes `disable` and `update` purge the file even for a custom mount path.
- **The device type can be changed** with `--type`: `file` (default), `socket` or
  `syslog`. HAL only knows how to mount them. For `socket` it points at
  `127.0.0.1:9080` over TCP, which is an address inside the Vault container, and
  for `syslog` it sets the facility `AUTH` and the tag `vault`. HAL starts nothing
  that receives either of them, and only the file type is wired to Promtail. This
  lesson covers the file device. The other two are not a working scenario of this
  lab and were not run.

## What to expect

- **A start-over deletes the history.** `disable` and `update` remove the audit
  file from the shared volume (when the mount path is the default `file/`, or with
  `--loki`). HAL cannot remove the volume from the Vault container, so it empties
  the file instead. `update` then enables the device again on a fresh file.
- **Dev mode forgets the device.** Dev mode keeps its configuration in memory, so
  when the Vault container is replaced (for example by `hal vault update`, which also
  removes the logs volume) the audit device is gone with it. Run the audit command
  again afterwards.
- **Prod mode does not share the volume.** HAL starts the prod-mode container
  without the `hal-vault-logs` mount, so the file the device writes stays inside
  the container, where Promtail does not look. The shared-volume path to Loki is
  a dev-mode arrangement. This follows from how HAL starts the container. It was
  not run end to end.
- **The log has no rotation.** HAL sets none up, so the file grows until the next
  `disable` or `update` empties it.
- **Enabling twice is not fatal.** If Vault does not accept the device, HAL prints
  a note that it may already be enabled and suggests `update`, then ends.
- **The status line always names the file.** `hal vault audit` prints the log file
  path whenever a device is active at the mount path, whatever its type.
- **Audit log analysis is not covered here.** Reading the log to investigate who
  did what is a separate skill. This topic stops at getting the log written and
  shipped.

## Commands

- `hal vault audit` with no action is read-only. It asks Vault whether a device is
  active at the mount path, names the log file when it is, and gives the next step.
  `hal vault status` also lists the audit devices next to the auth methods and
  secrets engines. `hal obs status` tells whether the observability containers
  (Prometheus, Loki, Promtail and Grafana) are running.
- Every action needs a running, initialized and unsealed Vault. It stops with an
  error otherwise, so start Vault first (see the Vault topic).
- `enable` creates the device, by default a file device at `file/`. `--path`
  changes the mount path and `--type` the device type.
- `update` disables the device, purges the file where the rules above say so, and
  enables it again.
- `disable` removes the device and, under the same rules, empties the file.

The audit device needs no login of its own. For the credentials of Vault itself,
which HAL keeps out of topics, `hal creds status` shows the ones of whatever is
running.
