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
-v, --tfe-tag string           TFE container image tag (default "2.0.5")
--tfe-image string             TFE container image name (default "images.releases.hashicorp.com/hashicorp/terraform-enterprise")
--tfe-pg-tag string            PostgreSQL image tag for TFE backend (default "17-alpine")
--tfe-pg-image string          PostgreSQL image name for TFE backend (default "postgres")
--tfe-redis-tag string         Redis image tag for TFE background jobs (default "8-alpine")
--tfe-redis-image string       Redis image name for TFE background jobs (default "redis")
--s3-api-port int              Host port mapped to the S3 API container port 9000 (default 19000)
--tfe-s3-tag string            Object storage gateway image tag for TFE object storage (VersityGW, default "v1.8.0")
--tfe-s3-image string          Object storage gateway image name for TFE object storage (default "ghcr.io/versity/versitygw")
--tfe-proxy-tag string         Nginx image tag for the shared TFE ingress proxy — serves every target (default "alpine")
--tfe-proxy-image string       Nginx image name for the shared TFE ingress proxy — serves every target (default "nginx")
-p, --password string              TFE Encryption Password (default "hal-secret-encryption-password")
-t, --target string            Terraform scope to act on: primary, twin, or both (default "primary")
--tfe-admin-email string       Initial TFE admin email used when bootstrapping via IACT (default "haladmin@localhost")
--tfe-admin-password string    Initial TFE admin password used when bootstrapping via IACT (default "hal9000FTW")
--tfe-admin-username string    Initial TFE admin username used when bootstrapping via IACT (default "haladmin")
--tfe-org string               Terraform Enterprise organization name to auto-bootstrap during deploy (default "hal")
--tfe-project string           Terraform Enterprise project name to auto-bootstrap during deploy (default "Dave")
--twin-tag string                   TFE container image tag for the twin instance (default "2.0.5")
--twin-image string                 TFE container image name for the twin instance (default "images.releases.hashicorp.com/hashicorp/terraform-enterprise")
--twin-container-name string        Container name used for the twin TFE core application (default "hal-tfe-bis")
--twin-db-name string               Database name for the twin TFE schema in shared PostgreSQL (default "tfe_bis")
--twin-db-password string           PostgreSQL password used by the twin TFE backend (default "tfe_password")
--twin-hostname string              TLS hostname used by the twin TFE instance (default "tfe-bis.localhost")
--twin-https-port int               Host HTTPS port the shared ingress proxy publishes for the twin vhost (default 9443)
--twin-s3-access-key string         S3 access key for shared object storage (default "haladmin")
--twin-s3-secret-key string         S3 secret key for shared object storage (default "hal9000FTW")
--twin-password string              Twin TFE encryption password (default "hal-secret-encryption-password")
--twin-s3-bucket string             S3 bucket name for twin TFE objects in shared object storage (default "tfe-bis-data")
--twin-tfe-admin-email string       Initial twin TFE admin email used when bootstrapping via IACT (default "haladmin@localhost")
--twin-tfe-admin-password string    Initial twin TFE admin password used when bootstrapping via IACT (default "hal9000FTW")
--twin-tfe-admin-username string    Initial twin TFE admin username used when bootstrapping via IACT (default "haladmin")
--twin-tfe-org string               Terraform Enterprise organization name to auto-bootstrap for the twin instance (default "hal-bis")
--twin-tfe-project string           Terraform Enterprise project name to auto-bootstrap for the twin instance (default "Dave-bis")
```
- Global flags: `--debug`, `--dry-run`, `--verbose`

Observability artifacts are managed explicitly with `hal terraform obs <create|update|delete|status>`.

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal terraform create
hal terraform create --target twin
hal terraform create --target both
```
