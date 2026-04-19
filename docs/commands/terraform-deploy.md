# HAL Terraform Deploy Command Spec

## Command
- `hal terraform create`

## Purpose
Deploy the local Terraform Enterprise (TFE) stack for HAL labs.

## Behavior
- Provisions and starts the TFE stack components used by HAL.
- Prepares local endpoint access for TFE workflows.

## Related
- Parent namespace: [terraform.md](terraform.md)
- Status: [terraform-status.md](terraform-status.md)
- Destroy: [terraform-destroy.md](terraform-destroy.md)

## Prerequisites
- HAL CLI is available in your local environment.
- The relevant product base deployment should be running when this command targets an existing stack.
## Flags
- Command flags from `hal terraform create --help`:
```text
--configure-obs                Refresh Prometheus target and Grafana dashboard artifacts without redeploying Terraform Enterprise
-f, --force                        Force redeploy
-h, --help                         help for deploy
--minio-api-port int           Host port mapped to MinIO S3 API container port 9000 (default 19000)
--minio-console-port int       Host port mapped to MinIO console container port 9001 (default 19001)
--minio-version string         MinIO image tag for TFE object storage (default "latest")
-p, --password string              TFE Encryption Password (default "hal-secret-encryption-password")
--pg-version string            PostgreSQL version for TFE backend (default "16")
--proxy-nginx-version string   Nginx image tag for the TFE ingress proxy (default "alpine")
--redis-version string         Redis version for TFE background jobs (default "7")
--tfe-admin-email string       Initial TFE admin email used when bootstrapping via IACT (default "haladmin@localhost")
--tfe-admin-password string    Initial TFE admin password used when bootstrapping via IACT (default "hal9000FTW")
--tfe-admin-username string    Initial TFE admin username used when bootstrapping via IACT (default "haladmin")
--tfe-org string               Terraform Enterprise organization name to auto-bootstrap during deploy (default "hal")
--tfe-project string           Terraform Enterprise project name to auto-bootstrap during deploy (default "Dave")
-v, --version string               Terraform Enterprise Docker image tag (default "1.2.0")
```
- Global flags: `--debug`, `--dry-run`

## Side Effects
- This command may create, mutate, or remove local lab resources depending on its operation.

## Example
```bash
hal terraform create
```
