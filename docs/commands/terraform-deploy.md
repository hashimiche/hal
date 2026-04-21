# HAL Terraform Deploy Command Spec

## Command
- `hal terraform create`
- `hal terraform create --target twin`
- `hal terraform create --target both`

## Purpose
Deploy the local Terraform Enterprise (TFE) stack for HAL labs.

## Behavior
- Provisions and starts the TFE stack components used by HAL.
- Prepares local endpoint access for TFE workflows.
- Uses `--target` to select deployment scope (`primary`, `twin`, or `both`).

## Related
- Parent namespace: [terraform.md](terraform.md)
- Status: [terraform-status.md](terraform-status.md)
- Destroy: [terraform-destroy.md](terraform-destroy.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Deprecated: older HAL docs may reference `hal terraform create --force`. That flag has been removed from the CLI. Use `hal terraform update` or `hal terraform create --update`.
- Command flags from `hal terraform create --help`:
```text
-u, --update                       Reconcile an existing Terraform Enterprise deployment in place
-h, --help                         help for deploy
--minio-api-port int           Host port mapped to MinIO S3 API container port 9000 (default 19000)
--minio-console-port int       Host port mapped to MinIO console container port 9001 (default 19001)
--minio-version string         MinIO image tag for TFE object storage (default "latest")
-p, --password string              TFE Encryption Password (default "hal-secret-encryption-password")
--pg-version string            PostgreSQL version for TFE backend (default "16")
--proxy-nginx-version string   Nginx image tag for the TFE ingress proxy (default "alpine")
--redis-version string         Redis version for TFE background jobs (default "7")
-t, --target string            Terraform scope to act on: primary, twin, or both (default "primary")
--tfe-admin-email string       Initial TFE admin email used when bootstrapping via IACT (default "haladmin@localhost")
--tfe-admin-password string    Initial TFE admin password used when bootstrapping via IACT (default "hal9000FTW")
--tfe-admin-username string    Initial TFE admin username used when bootstrapping via IACT (default "haladmin")
--tfe-org string               Terraform Enterprise organization name to auto-bootstrap during deploy (default "hal")
--tfe-project string           Terraform Enterprise project name to auto-bootstrap during deploy (default "Dave")
--twin-container-name string        Container name used for the twin TFE core application (default "hal-tfe-bis")
--twin-db-name string               Database name for the twin TFE schema in shared PostgreSQL (default "tfe_bis")
--twin-db-password string           PostgreSQL password used by the twin TFE backend (default "tfe_password")
--twin-hostname string              TLS hostname used by the twin TFE instance (default "tfe-bis.localhost")
--twin-https-port int               Host HTTPS port exposed by the twin TFE ingress proxy (default 9443)
--twin-minio-root-password string   MinIO root password for shared object storage (default "minioadmin")
--twin-minio-root-user string       MinIO root user for shared object storage (default "minioadmin")
--twin-password string              Twin TFE encryption password (default "hal-secret-encryption-password")
--twin-proxy-ip string              Static internal proxy IP on hal-net for twin hostname routing (default "10.89.3.55")
--twin-proxy-nginx-version string   Nginx image tag for the twin ingress proxy (default "alpine")
--twin-s3-bucket string             S3 bucket name for twin TFE objects in shared MinIO (default "tfe-bis-data")
--twin-tfe-admin-email string       Initial twin TFE admin email used when bootstrapping via IACT (default "haladmin@localhost")
--twin-tfe-admin-password string    Initial twin TFE admin password used when bootstrapping via IACT (default "hal9000FTW")
--twin-tfe-admin-username string    Initial twin TFE admin username used when bootstrapping via IACT (default "haladmin")
--twin-tfe-org string               Terraform Enterprise organization name to auto-bootstrap for the twin instance (default "hal-bis")
--twin-tfe-project string           Terraform Enterprise project name to auto-bootstrap for the twin instance (default "Dave-bis")
--twin-version string               Terraform Enterprise Docker image tag for the twin instance (default "1.2.0")
-v, --version string               Terraform Enterprise Docker image tag (default "1.2.0")
```
- Global flags: `--debug`, `--dry-run`

Observability artifacts are managed explicitly with `hal terraform obs <create|update|delete|status>`.

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal terraform create
hal terraform create --target twin
hal terraform create --target both
```
