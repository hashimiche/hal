# HAL AAP Deploy Command Spec

## Command
- `hal aap create`

## Purpose
Deploy a local AAP runtime container with systemd-friendly flags and publish HTTP/HTTPS locally.

## Related
- Parent namespace: [aap.md](aap.md)

## Prerequisites
- HAL CLI is available in your local environment.
- Docker or Podman is running.
- Local image `ubi9-aap:latest` exists (or override image/tag flags).
- The image expects a systemd-style container runtime, so HAL runs it privileged with `/sys/fs/cgroup` and tmpfs mounts.

## Flags
- Command flags from `hal aap create --help`:
```text
-u, --update             Reconcile an existing AAP deployment in place
    --aap-image string   AAP container image name (default "ubi9-aap")
    --aap-tag string     AAP container image tag (default "latest")
    --host-port int      Host HTTPS port to publish AAP container port 443 (default 443)
-h, --help               help for create
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- Creates `hal-aap` container on `hal-net`.
- Binds `80:80` and `host-port:443` (default `443:443`).
- Runs the container privileged with cgroup namespace host and systemd-friendly tmpfs mounts.

## Example
```bash
hal aap create
hal aap create --host-port 8443
hal aap create --aap-image ubi9-aap --aap-tag latest
```
